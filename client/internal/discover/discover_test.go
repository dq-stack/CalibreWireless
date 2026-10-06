package discover

import "testing"

func TestParseReply(t *testing.T) {
	host, port, err := ParseReply("calibre wireless device client (on Daniels-MacBook-Air);8080,9090")
	if err != nil || host != "Daniels-MacBook-Air" || port != 9090 {
		t.Fatalf("got %q %d %v", host, port, err)
	}
	// No content server running: the first number is empty.
	if _, port, err := ParseReply("calibre wireless device client (on mac);,9091"); err != nil || port != 9091 {
		t.Fatalf("got %d %v", port, err)
	}
	if _, _, err := ParseReply("something else"); err == nil {
		t.Fatal("expected error")
	}
}
