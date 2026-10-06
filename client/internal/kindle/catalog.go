package kindle

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// The Kindle's content catalog, read through cc.db with the device's own
// sqlite3 (read-only queries only). This app never changes the catalog.
// See PROTOCOL.md §9.

const CCDB = "/var/local/cc.db"

// Entry is one cc.db Entries row (only the fields we use).
type Entry struct {
	UUID            string
	Location        string
	CdeKey          string
	CdeType         string
	Thumbnail       string
	LastAccess      int64   // epoch seconds, 0 = never
	PercentFinished float64 // -1 = never opened
	ReadState       string
}

// Catalog reads cc.db.
type Catalog struct {
	DB      string // path to cc.db
	SQLite  string // sqlite3 binary
	Timeout time.Duration
}

func NewCatalog() *Catalog {
	return &Catalog{DB: CCDB, SQLite: "/usr/bin/sqlite3", Timeout: 10 * time.Second}
}

// Available reports whether we're on a Kindle with a readable catalog.
func (c *Catalog) Available() bool {
	if _, err := os.Stat(c.DB); err != nil {
		return false
	}
	_, err := os.Stat(c.SQLite)
	return err == nil
}

const sep = "\x1f"

// query runs a read-only SELECT and returns rows of fields.
func (c *Catalog) query(sql string) ([][]string, error) {
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sql)), "SELECT") {
		return nil, errors.New("catalog: only SELECT is allowed")
	}
	script := ".timeout 5000\n.separator \"" + sep + "\"\n" + sql + ";\n"
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		cmd := exec.Command(c.SQLite, c.DB)
		cmd.Stdin = strings.NewReader(script)
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil || errb.Len() > 0 {
			lastErr = fmt.Errorf("sqlite3: %v %s", err, strings.TrimSpace(errb.String()))
			time.Sleep(time.Second) // usually "database is locked"
			continue
		}
		var rows [][]string
		for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
			if line != "" {
				rows = append(rows, strings.Split(line, sep))
			}
		}
		return rows, nil
	}
	return nil, lastErr
}

func sqlQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

const entryCols = "p_uuid, p_location, ifnull(p_cdeKey,''), ifnull(p_cdeType,''), ifnull(p_thumbnail,''), " +
	"ifnull(p_lastAccess,0), ifnull(p_percentFinished,-1), ifnull(p_readState,'')"

func parseEntry(r []string) (Entry, bool) {
	if len(r) < 8 {
		return Entry{}, false
	}
	e := Entry{UUID: r[0], Location: r[1], CdeKey: r[2], CdeType: r[3], Thumbnail: r[4], ReadState: r[7]}
	fmt.Sscan(r[5], &e.LastAccess)
	fmt.Sscan(r[6], &e.PercentFinished)
	return e, true
}

// Entries returns every indexed book under dir (e.g. /mnt/us/documents/),
// keyed by full path.
func (c *Catalog) Entries(dir string) (map[string]Entry, error) {
	rows, err := c.query("SELECT " + entryCols + " FROM Entries WHERE p_type='Entry:Item' AND p_location LIKE " +
		sqlQuote(strings.ReplaceAll(dir, "%", "")+"%"))
	if err != nil {
		return nil, err
	}
	m := map[string]Entry{}
	for _, r := range rows {
		if e, ok := parseEntry(r); ok {
			m[e.Location] = e
		}
	}
	return m, nil
}

// PdocKey is the key the Kindle gives sideloaded personal documents (PDF,
// TXT): "*"
// plus the SHA-1 of the full path (confirmed on FW 5.12 with My Clippings.txt).
func PdocKey(path string) string {
	h := sha1.Sum([]byte(path))
	return "*" + hex.EncodeToString(h[:])
}
