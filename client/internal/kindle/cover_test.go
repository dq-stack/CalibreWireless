package kindle

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// fakeMOBI builds the smallest PDB/MOBI file with an EXTH block.
func fakeMOBI(exth map[uint32]string) []byte {
	var ex bytes.Buffer
	for id, v := range exth {
		binary.Write(&ex, binary.BigEndian, id)
		binary.Write(&ex, binary.BigEndian, uint32(8+len(v)))
		ex.WriteString(v)
	}
	var rec0 bytes.Buffer
	rec0.Write(make([]byte, 16)) // PalmDOC header
	mobi := make([]byte, 0xE8)
	copy(mobi, "MOBI")
	binary.BigEndian.PutUint32(mobi[4:], uint32(len(mobi)))
	binary.BigEndian.PutUint32(mobi[0x80-16:], 0x40) // EXTH flag (at rec0+0x80)
	rec0.Write(mobi)
	rec0.WriteString("EXTH")
	binary.Write(&rec0, binary.BigEndian, uint32(12+ex.Len()))
	binary.Write(&rec0, binary.BigEndian, uint32(len(exth)))
	rec0.Write(ex.Bytes())

	hdr := make([]byte, 78+2*8+2)
	copy(hdr[60:], "BOOKMOBI")
	binary.BigEndian.PutUint16(hdr[76:], 2)
	r0 := uint32(len(hdr))
	binary.BigEndian.PutUint32(hdr[78:], r0)
	binary.BigEndian.PutUint32(hdr[86:], r0+uint32(rec0.Len()))
	out := append(hdr, rec0.Bytes()...)
	return append(out, []byte("record1")...)
}

var jpeg = []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3}

func thumb(b []byte) any {
	return []any{"374", "500", base64.StdEncoding.EncodeToString(b)}
}

func TestBookKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "b.azw3")
	os.WriteFile(p, fakeMOBI(map[uint32]string{113: "u-1", 501: "EBOK", 100: "Author"}), 0o644)
	k, typ, err := BookKey(p)
	if err != nil || k != "u-1" || typ != "EBOK" {
		t.Fatalf("got %q %q %v", k, typ, err)
	}
	os.WriteFile(p, fakeMOBI(map[uint32]string{100: "Author"}), 0o644)
	if _, _, err := BookKey(p); err != ErrNoKey {
		t.Fatalf("want ErrNoKey, got %v", err)
	}
	os.WriteFile(p, []byte("not a book at all, just text that is long enough........................................................"), 0o644)
	if _, _, err := BookKey(p); err == nil {
		t.Fatal("want error for non-MOBI")
	}
}

func TestWriteRestoreRemove(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ThumbDir), 0o755)
	book := filepath.Join(root, "b.azw3.part")
	os.WriteFile(book, fakeMOBI(map[uint32]string{113: "u-1", 501: "EBOK"}), 0o644)
	c := Covers{Root: root}

	if nc, _ := c.NotCached(book); !nc {
		t.Fatal("should be uncached before write")
	}
	name, err := c.Write(book, thumb(jpeg))
	if err != nil || name != "thumbnail_u-1_EBOK_portrait.jpg" {
		t.Fatalf("write: %q %v", name, err)
	}
	thumbPath := filepath.Join(root, ThumbDir, name)
	if b, _ := os.ReadFile(thumbPath); !bytes.Equal(b, jpeg) {
		t.Fatal("thumbnail content")
	}
	if nc, _ := c.NotCached(book); nc {
		t.Fatal("should be cached after write")
	}

	// The Kindle replaces it with Amazon's placeholder: restore puts ours back.
	os.WriteFile(thumbPath, []byte("placeholder, different size"), 0o644)
	restored, err := c.Restore()
	if err != nil || len(restored) != 1 {
		t.Fatalf("restore: %v %v", restored, err)
	}
	if b, _ := os.ReadFile(thumbPath); !bytes.Equal(b, jpeg) {
		t.Fatal("not restored")
	}
	if restored, _ := c.Restore(); len(restored) != 0 {
		t.Fatal("second restore should do nothing")
	}

	if err := c.Remove(book); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{ThumbDir, CacheDir} {
		if _, err := os.Stat(filepath.Join(root, d, name)); !os.IsNotExist(err) {
			t.Fatalf("%s not removed", d)
		}
	}
}

func TestBadThumbnail(t *testing.T) {
	if _, err := JPEG(nil); err == nil {
		t.Fatal("nil thumbnail")
	}
	if _, err := JPEG(thumb([]byte("PNG..."))); err == nil {
		t.Fatal("non-JPEG accepted")
	}
}

func TestRemoveSidecars(t *testing.T) {
	d := t.TempDir()
	book := filepath.Join(d, "Title - Author.azw3")
	os.MkdirAll(filepath.Join(d, "Title - Author.sdr"), 0o755)
	os.WriteFile(filepath.Join(d, "Title - Author.sdr", "x.azw3f"), nil, 0o644)
	os.WriteFile(filepath.Join(d, "Title - Author.apnx"), nil, 0o644)
	os.WriteFile(filepath.Join(d, "Other.azw3"), nil, 0o644)
	RemoveSidecars(book)
	left, _ := os.ReadDir(d)
	if len(left) != 1 || left[0].Name() != "Other.azw3" {
		t.Fatalf("left: %v", left)
	}
}

func TestHasCover(t *testing.T) {
	for p, want := range map[string]bool{"a.azw3": true, "a.AZW3.part": true, "a.mobi": true, "a.pdf": false, "a.pdf.part": false, "a.txt": false, "a.kfx": false} {
		if HasCover(p) != want {
			t.Errorf("HasCover(%q) != %v", p, want)
		}
	}
}
