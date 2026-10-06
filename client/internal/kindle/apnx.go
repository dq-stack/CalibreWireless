package kindle

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// charsPerPage is calibre's "fast" APNX method: one page per 2300 characters
// of uncompressed text (calibre's default for Kindle USB transfers).
const charsPerPage = 2300

// APNXPath is where FW 5.x reads page numbers for a book: inside the book's
// .sdr folder, not beside the book (confirmed on a PW2, FW 5.12).
func APNXPath(bookFile string) string {
	base := strings.TrimSuffix(filepath.Base(bookFile), filepath.Ext(bookFile))
	return filepath.Join(filepath.Dir(bookFile), base+".sdr", base+".apnx")
}

// WriteAPNX builds page numbers for src (which may still be the .part file)
// and writes them for the book that will live at finalPath.
func WriteAPNX(src, finalPath string) (pages int, err error) {
	h, err := ReadHeader(src)
	if err != nil {
		return 0, err
	}
	b, pages, err := BuildAPNX(h)
	if err != nil {
		return 0, err
	}
	p := APNXPath(finalPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return 0, err
	}
	return pages, writeFile(p, b)
}

// BuildAPNX produces an APNX file in the same layout as calibre's
// APNXBuilder.generate_apnx.
func BuildAPNX(h Header) ([]byte, int, error) {
	if h.TextLength <= 0 {
		return nil, 0, errors.New("book has no text length")
	}
	var locs []uint32
	for c := 0; c < h.TextLength; c += charsPerPage {
		locs = append(locs, uint32(c))
	}
	if len(locs) > 0xFFFF {
		return nil, 0, fmt.Errorf("too many pages (%d)", len(locs))
	}
	cdeType := h.CdeType
	if cdeType == "" {
		cdeType = "EBOK"
	}
	var g [4]byte
	rand.Read(g[:])
	guid := hex.EncodeToString(g[:])

	// calibre builds these JSON strings by hand; key order matters to nobody
	// but we keep it identical anyway.
	var content string
	if h.MobiVersion == 8 {
		content = fmt.Sprintf(`{"contentGuid":"%s","asin":%s,"cdeType":%s,"format":"MOBI_8","fileRevisionId":"1","acr":%s}`,
			guid, jsonStr(h.Key), jsonStr(cdeType), jsonStr(h.Name))
	} else {
		content = fmt.Sprintf(`{"contentGuid":"%s","asin":%s,"cdeType":%s,"fileRevisionId":"1"}`,
			guid, jsonStr(h.Key), jsonStr(cdeType))
	}
	page := fmt.Sprintf(`{"asin":%s,"pageMap":"(1,a,1)"}`, jsonStr(h.Key))

	var out bytes.Buffer
	w := func(v any) { binary.Write(&out, binary.BigEndian, v) }
	w(uint32(65537))
	w(uint32(12 + len(content)))
	w(uint32(len(content)))
	out.WriteString(content)
	w(uint16(1))
	w(uint16(len(page)))
	w(uint16(len(locs)))
	w(uint16(32))
	out.WriteString(page)
	for _, l := range locs {
		w(l)
	}
	return out.Bytes(), len(locs), nil
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
