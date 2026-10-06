// Package readsync decides what read status and last-read date to report to
// calibre for each book. See docs/PROTOCOL.md §9.
//
// calibre writes whatever we report into its columns when it differs from
// the value calibre last sent us. Reporting the Kindle's state blindly would
// undo changes made in calibre (books read on paper, ticked by hand), so we
// report a Kindle value only when it changed on the Kindle since the last
// sync, and otherwise echo calibre's own value back.
package readsync

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dq-stack/kindle-calibre-wireless/client/internal/kindle"
	"github.com/dq-stack/kindle-calibre-wireless/client/internal/library"
)

// Rule decides when the Kindle considers a book read.
type Rule struct {
	Marked  bool    // the Kindle's own "Mark as Read" (p_readState)
	Percent float64 // also read at or above this progress; 0 = off
}

// KindleState is what the Kindle says about one book.
type KindleState struct {
	Read     bool   `json:"read"`
	LastRead string `json:"last"` // ISO 8601 UTC, "" = never opened
}

// FromEntry derives the Kindle state from a cc.db row.
func FromEntry(e kindle.Entry, r Rule) KindleState {
	var s KindleState
	opened := e.PercentFinished >= 0 // never-opened books have NULL progress
	if r.Marked && isMarkedRead(e.ReadState) {
		s.Read = true
	}
	if r.Percent > 0 && opened && e.PercentFinished >= r.Percent {
		s.Read = true
	}
	if opened && e.LastAccess > 0 {
		s.LastRead = time.Unix(e.LastAccess, 0).UTC().Format("2006-01-02T15:04:05+00:00")
	}
	return s
}

// isMarkedRead interprets p_readState: "Mark as Read" sets it to 1 (FW 5.12,
// confirmed on the PW2); unread books have it empty.
func isMarkedRead(v string) bool {
	return v == "1"
}

// Store remembers the Kindle state we last reported, per lpath, in a small
// JSON file next to the app.
type Store struct {
	path string
	mu   sync.Mutex
	last map[string]KindleState
}

func OpenStore(path string) *Store {
	s := &Store{path: path, last: map[string]KindleState{}}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &s.last)
	}
	return s
}

func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", " ")
	if err := enc.Encode(s.last); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Report returns the values to send calibre for one book and records the
// Kindle state as seen. readCol/dateCol are calibre's column names
// ("#read", "#last_read"); either may be "".
func (s *Store) Report(lpath string, b library.Book, k KindleState, readCol, dateCol string) (isRead any, lastRead any) {
	s.mu.Lock()
	prev, seen := s.last[lpath]
	s.last[lpath] = k
	s.mu.Unlock()

	calRead := CalibreValue(b, readCol)
	calDate := CalibreValue(b, dateCol)

	// Read status.
	switch {
	case !seen:
		// First sync for this book: a Kindle "read" is news; a Kindle
		// "unread" must not clear calibre's value.
		if k.Read {
			isRead = true
		} else {
			isRead = calRead
		}
	case k.Read != prev.Read:
		isRead = k.Read
	default:
		isRead = calRead
	}

	// Last read date.
	switch {
	case k.LastRead != "" && (!seen || k.LastRead != prev.LastRead):
		lastRead = k.LastRead
	default:
		lastRead = calDate
	}
	return isRead, lastRead
}

// CalibreValue is a custom column's value in calibre's metadata for a book,
// or nil.
func CalibreValue(b library.Book, col string) any {
	if col == "" {
		return nil
	}
	um, _ := b["user_metadata"].(map[string]any)
	field, _ := um[col].(map[string]any)
	v := field["#value#"]
	if s, ok := v.(string); ok && (s == "None" || strings.TrimSpace(s) == "") {
		return nil
	}
	return v
}
