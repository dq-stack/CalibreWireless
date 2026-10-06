package readsync

import (
	"path/filepath"
	"testing"

	"github.com/dq-stack/kindle-calibre-wireless/client/internal/kindle"
	"github.com/dq-stack/kindle-calibre-wireless/client/internal/library"
)

func book(read any) library.Book {
	return library.Book{"lpath": "documents/a.azw3", "user_metadata": map[string]any{
		"#read":      map[string]any{"#value#": read},
		"#last_read": map[string]any{"#value#": "None"},
	}}
}

func TestFromEntry(t *testing.T) {
	r := Rule{Marked: true, Percent: 98}
	if s := FromEntry(kindle.Entry{PercentFinished: -1, LastAccess: 1790000000}, r); s.Read || s.LastRead != "" {
		t.Fatalf("never opened: %+v", s)
	}
	if s := FromEntry(kindle.Entry{PercentFinished: 99, LastAccess: 1790000000}, r); !s.Read || s.LastRead != "2026-09-21T14:13:20+00:00" {
		t.Fatalf("finished: %+v", s)
	}
	if s := FromEntry(kindle.Entry{PercentFinished: 99}, Rule{Marked: true}); s.Read {
		t.Fatal("percent rule off but counted as read")
	}
	// Marked as read without ever opening it (seen on the device).
	if s := FromEntry(kindle.Entry{PercentFinished: -1, ReadState: "1", LastAccess: 1791282049}, Rule{Marked: true}); !s.Read || s.LastRead != "" {
		t.Fatalf("marked read: %+v", s)
	}
}

func TestReportKeepsCalibreEdits(t *testing.T) {
	st := OpenStore(filepath.Join(t.TempDir(), "rs.json"))
	unread := KindleState{}
	// First sync, Kindle unread, calibre says read (paper book): keep calibre's.
	if r, d := st.Report("documents/a.azw3", book(true), unread, "#read", "#last_read"); r != true || d != nil {
		t.Fatalf("first sync: %v %v", r, d)
	}
	// Nothing changed on the Kindle: echo calibre, even if calibre changed.
	if r, _ := st.Report("documents/a.azw3", book(false), unread, "#read", "#last_read"); r != false {
		t.Fatalf("calibre edit lost: %v", r)
	}
	// Finished on the Kindle: report it.
	done := KindleState{Read: true, LastRead: "2026-10-06T10:00:00+00:00"}
	if r, d := st.Report("documents/a.azw3", book(false), done, "#read", "#last_read"); r != true || d != done.LastRead {
		t.Fatalf("kindle change: %v %v", r, d)
	}
	// Same Kindle state again, calibre now agrees: echo.
	if r, d := st.Report("documents/a.azw3", book(true), done, "#read", "#last_read"); r != true || d != nil {
		t.Fatalf("steady: %v %v", r, d)
	}
	st.Save()
	if st2 := OpenStore(st.path); st2.last["documents/a.azw3"] != done {
		t.Fatal("store not saved")
	}
}
