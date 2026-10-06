package library

import "testing"

func TestCleanLPath(t *testing.T) {
	good := map[string]string{
		"Voss, Chris/Never Split.azw3":           "documents/Voss, Chris/Never Split.azw3",
		"documents/Voss, Chris/Never Split.azw3": "documents/Voss, Chris/Never Split.azw3",
		`Author\Book.mobi`:                       "documents/Author/Book.mobi",
	}
	for in, want := range good {
		if got, err := CleanLPath(in); err != nil || got != want {
			t.Errorf("CleanLPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "/etc/passwd", "../x.azw3", "a/../../x", "a//b", "./a", "a/\x00b"} {
		if got, err := CleanLPath(bad); err == nil {
			t.Errorf("CleanLPath(%q) = %q, want error", bad, got)
		}
	}
}
