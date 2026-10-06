// Package library keeps the on-device book list in calibre's own USB format
// (metadata.calibre + driveinfo.calibre at the storage root), so books sent over
// USB and over Wi-Fi are one list. See docs/PROTOCOL.md §4.
package library

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	MetadataFile  = "metadata.calibre"
	DriveinfoFile = "driveinfo.calibre"
	// BooksDir is the only folder the Kindle indexes; every lpath lives under it.
	BooksDir = "documents"
)

// Book is calibre's metadata for one book, kept exactly as calibre sent it
// (numbers kept as json.Number so nothing is rounded on the way back).
type Book map[string]any

func (b Book) Str(k string) string {
	s, _ := b[k].(string)
	return s
}

func (b Book) LPath() string { return b.Str("lpath") }

// Library is the book list rooted at Root (/mnt/us on the Kindle).
type Library struct {
	Root  string
	Books []Book
	Drive map[string]any
}

func Open(root string) (*Library, error) {
	l := &Library{Root: root}
	if err := readJSON(filepath.Join(root, MetadataFile), &l.Books); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%s: %w", MetadataFile, err)
	}
	if err := readJSON(filepath.Join(root, DriveinfoFile), &l.Drive); err != nil && !errors.Is(err, os.ErrNotExist) {
		l.Drive = nil // a broken driveinfo is replaced, not fatal
	}
	if l.Drive == nil {
		l.Drive = map[string]any{}
	}
	if l.Drive["device_store_uuid"] == nil {
		l.Drive["device_store_uuid"] = newUUID()
	}
	return l, nil
}

func readJSON(p string, v any) error {
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	return d.Decode(v)
}

func writeJSON(p string, v any) error {
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	err = enc.Encode(v)
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

func (l *Library) Save() error {
	if l.Books == nil {
		l.Books = []Book{}
	}
	return writeJSON(filepath.Join(l.Root, MetadataFile), l.Books)
}

func (l *Library) SaveDrive() error {
	return writeJSON(filepath.Join(l.Root, DriveinfoFile), l.Drive)
}

func (l *Library) StoreUUID() string {
	s, _ := l.Drive["device_store_uuid"].(string)
	return s
}

// Index returns the position of the book with this lpath, or -1.
func (l *Library) Index(lpath string) int {
	for i, b := range l.Books {
		if b.LPath() == lpath {
			return i
		}
	}
	return -1
}

func (l *Library) Get(lpath string) Book {
	if i := l.Index(lpath); i >= 0 {
		return l.Books[i]
	}
	return nil
}

// Put adds or replaces a book, keyed by lpath.
func (l *Library) Put(b Book) {
	if i := l.Index(b.LPath()); i >= 0 {
		l.Books[i] = b
		return
	}
	l.Books = append(l.Books, b)
}

func (l *Library) Remove(lpath string) Book {
	i := l.Index(lpath)
	if i < 0 {
		return nil
	}
	b := l.Books[i]
	l.Books = append(l.Books[:i], l.Books[i+1:]...)
	return b
}

// Prune drops entries whose file is gone (deleted on the Kindle itself).
func (l *Library) Prune() (removed int) {
	kept := l.Books[:0]
	for _, b := range l.Books {
		if _, err := os.Stat(l.Abs(b.LPath())); err == nil {
			kept = append(kept, b)
		} else {
			removed++
		}
	}
	l.Books = kept
	return removed
}

// ErrUnsafePath is returned for lpaths that could escape the storage root.
var ErrUnsafePath = errors.New("unsafe file name")

// CleanLPath checks an lpath from calibre and forces it under documents/.
func CleanLPath(lpath string) (string, error) {
	lpath = strings.ReplaceAll(lpath, "\\", "/")
	if lpath == "" || strings.HasPrefix(lpath, "/") || strings.ContainsRune(lpath, 0) {
		return "", ErrUnsafePath
	}
	for _, part := range strings.Split(lpath, "/") {
		if part == ".." || part == "." || part == "" {
			return "", ErrUnsafePath
		}
	}
	if !strings.HasPrefix(lpath, BooksDir+"/") {
		lpath = BooksDir + "/" + lpath
	}
	return path.Clean(lpath), nil
}

// Abs maps an lpath to a real path under Root.
func (l *Library) Abs(lpath string) string {
	return filepath.Join(l.Root, filepath.FromSlash(lpath))
}

// Contained reports whether p (after resolving symlinks in its directory)
// is inside Root/documents.
func (l *Library) Contained(p string) bool {
	base, err := filepath.EvalSymlinks(filepath.Join(l.Root, BooksDir))
	if err != nil {
		return false
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		return false
	}
	return dir == base || strings.HasPrefix(dir, base+string(filepath.Separator))
}

// Space returns free and total bytes on the filesystem holding Root.
func (l *Library) Space() (free, total uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(l.Root, &st); err != nil {
		return 0, 0
	}
	return uint64(st.Bavail) * uint64(st.Bsize), uint64(st.Blocks) * uint64(st.Bsize)
}

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
