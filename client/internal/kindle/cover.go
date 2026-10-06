// Package kindle holds the stock-Kindle specifics: cover thumbnails, the
// Amazon cover-bug cache and sidecar files. See docs/PROTOCOL.md §7, §10.
package kindle

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	ThumbDir = "system/thumbnails"
	// CacheDir is calibre's own backup folder for the Amazon cover bug
	// (MobileRead t=329945); shared with calibre's USB driver.
	CacheDir = "amazon-cover-bug"
)

var ErrNoKey = errors.New("book has no cdeKey/cdeType")

// BookKey reads the cdeKey (EXTH 113) and cdeType (EXTH 501) from a MOBI/AZW3
// file, the same fields calibre uses to name the Kindle thumbnail.
func BookKey(path string) (key, cdeType string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	return bookKey(f)
}

func bookKey(r io.ReaderAt) (string, string, error) {
	hdr := make([]byte, 78+16)
	if _, err := r.ReadAt(hdr, 0); err != nil {
		return "", "", fmt.Errorf("pdb header: %w", err)
	}
	if string(hdr[60:68]) != "BOOKMOBI" {
		return "", "", fmt.Errorf("not a MOBI/AZW3 file (%q)", hdr[60:68])
	}
	if binary.BigEndian.Uint16(hdr[76:78]) < 2 {
		return "", "", errors.New("too few records")
	}
	rec0 := int64(binary.BigEndian.Uint32(hdr[78:82]))
	rec1 := int64(binary.BigEndian.Uint32(hdr[86:90]))
	if rec1 <= rec0 || rec1-rec0 > 1<<20 {
		return "", "", errors.New("bad record 0 size")
	}
	rec := make([]byte, rec1-rec0)
	if _, err := r.ReadAt(rec, rec0); err != nil {
		return "", "", fmt.Errorf("record 0: %w", err)
	}
	if len(rec) < 0x84 || string(rec[16:20]) != "MOBI" {
		return "", "", errors.New("no MOBI header")
	}
	mobiLen := int(binary.BigEndian.Uint32(rec[20:24]))
	if binary.BigEndian.Uint32(rec[0x80:0x84])&0x40 == 0 {
		return "", "", ErrNoKey // no EXTH block
	}
	p := 16 + mobiLen
	if p+12 > len(rec) || string(rec[p:p+4]) != "EXTH" {
		return "", "", errors.New("EXTH missing")
	}
	count := int(binary.BigEndian.Uint32(rec[p+8 : p+12]))
	p += 12
	var key, typ string
	for i := 0; i < count && p+8 <= len(rec); i++ {
		id := binary.BigEndian.Uint32(rec[p : p+4])
		n := int(binary.BigEndian.Uint32(rec[p+4 : p+8]))
		if n < 8 || p+n > len(rec) {
			break
		}
		data := rec[p+8 : p+n]
		switch id {
		case 113:
			key = strings.TrimSpace(string(data))
		case 501:
			typ = strings.TrimSpace(string(data))
		}
		p += n
	}
	if key == "" || typ == "" {
		return "", "", ErrNoKey
	}
	return key, typ, nil
}

// ThumbName is the Kindle's thumbnail file name for a book.
func ThumbName(key, cdeType string) string {
	return fmt.Sprintf("thumbnail_%s_%s_portrait.jpg", key, cdeType)
}

// Covers writes and restores thumbnails under the storage root (/mnt/us).
type Covers struct{ Root string }

// JPEG decodes calibre's metadata thumbnail: [width, height, base64 JPEG].
func JPEG(thumb any) ([]byte, error) {
	t, ok := thumb.([]any)
	if !ok || len(t) < 3 {
		return nil, errors.New("no thumbnail in metadata")
	}
	s, _ := t[2].(string)
	if s == "" {
		return nil, errors.New("empty thumbnail")
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return nil, errors.New("thumbnail is not a JPEG")
	}
	return b, nil
}

// Write saves the cover for bookFile into system/thumbnails and the
// cover-bug cache. bookFile may be the not-yet-renamed .part file.
func (c Covers) Write(bookFile string, thumb any) (string, error) {
	key, typ, err := BookKey(bookFile)
	if err != nil {
		return "", err
	}
	img, err := JPEG(thumb)
	if err != nil {
		return "", err
	}
	name := ThumbName(key, typ)
	for _, dir := range []string{ThumbDir, CacheDir} {
		d := filepath.Join(c.Root, dir)
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
		if err := writeFile(filepath.Join(d, name), img); err != nil {
			return "", err
		}
	}
	return name, nil
}

// NotCached reports whether bookFile's cover is missing from the cover-bug
// cache. The cache only ever holds covers from calibre, so a cover the Kindle
// made itself (often Amazon's "No image available") doesn't count.
func (c Covers) NotCached(bookFile string) (bool, error) {
	key, typ, err := BookKey(bookFile)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(filepath.Join(c.Root, CacheDir, ThumbName(key, typ)))
	return os.IsNotExist(err), nil
}

// Remove deletes bookFile's thumbnail from both places. Call before the book
// file itself is deleted (the key is read from it).
func (c Covers) Remove(bookFile string) error {
	key, typ, err := BookKey(bookFile)
	if err != nil {
		return err
	}
	name := ThumbName(key, typ)
	for _, dir := range []string{ThumbDir, CacheDir} {
		if err := os.Remove(filepath.Join(c.Root, dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// Restore copies cached covers back into system/thumbnails when the Kindle
// has replaced (different size) or removed them, exactly as calibre's USB
// driver does on connect.
func (c Covers) Restore() (restored []string, err error) {
	src := filepath.Join(c.Root, CacheDir)
	dst := filepath.Join(c.Root, ThumbDir)
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if _, err := os.Stat(dst); err != nil {
		return nil, nil // no thumbnails folder: not a Kindle layout
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasPrefix(e.Name(), "thumbnail_") {
			continue
		}
		si, err := e.Info()
		if err != nil {
			continue
		}
		if di, err := os.Stat(filepath.Join(dst, e.Name())); err == nil && di.Size() == si.Size() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			continue
		}
		if err := writeFile(filepath.Join(dst, e.Name()), b); err == nil {
			restored = append(restored, e.Name())
		}
	}
	return restored, nil
}

func writeFile(p string, b []byte) error {
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, bytes.NewReader(b))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, p)
}

// sidecarExts are deleted with a book, as calibre's KINDLE2 USB driver does.
var sidecarExts = []string{".mbp", ".tan", ".pdr", ".ea", ".apnx", ".phl", ".mbp1", ".mbs", ".sdr", ".han"}

// RemoveSidecars deletes a deleted book's sidecar files and folders
// (reading position, notes, page numbers).
func RemoveSidecars(bookFile string) {
	base := strings.TrimSuffix(bookFile, filepath.Ext(bookFile))
	for _, ext := range sidecarExts {
		for _, p := range []string{base + ext, bookFile + ext} {
			if st, err := os.Lstat(p); err == nil {
				if st.IsDir() {
					os.RemoveAll(p)
				} else {
					os.Remove(p)
				}
			}
		}
	}
}
