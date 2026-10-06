package session

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dq-stack/kindle-calibre-wireless/client/internal/library"
	"github.com/dq-stack/kindle-calibre-wireless/client/internal/proto"
)

// fakeCalibre plays calibre's side of the conversation over a pipe.
type fakeCalibre struct {
	t *testing.T
	c *proto.Conn
}

func (f *fakeCalibre) call(op int, arg any) (int, map[string]any) {
	f.t.Helper()
	if err := f.c.Send(op, arg); err != nil {
		f.t.Fatal(err)
	}
	return f.recv()
}

func (f *fakeCalibre) recv() (int, map[string]any) {
	f.t.Helper()
	op, raw, err := f.c.Read()
	if err != nil {
		f.t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(raw, &m)
	return op, m
}

func setup(t *testing.T, cfg Config) (*fakeCalibre, *library.Library, chan error) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "documents"), 0o755)
	lib, err := library.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	a, b := net.Pipe()
	s := New(proto.New(b), lib, cfg, Hooks{})
	done := make(chan error, 1)
	go func() { done <- s.Run(); b.Close() }()
	fc := &fakeCalibre{t: t, c: proto.New(a)}
	fc.c.Timeout = 5 * time.Second
	t.Cleanup(func() { a.Close() })
	return fc, lib, done
}

func TestFullConversation(t *testing.T) {
	fc, lib, done := setup(t, Config{AppName: "test", DeviceName: "Kindle", Password: "pw",
		Extensions: []string{"azw3"}, CoverHeight: 500})

	op, info := fc.call(proto.GetInitializationInfo, map[string]any{
		"passwordChallenge": "2026-10-06T00:00:00+00:00", "calibre_version": []int{9, 13, 0},
	})
	if op != proto.OK || info["versionOK"] != true {
		t.Fatalf("init: %d %v", op, info)
	}
	want := sha1.Sum([]byte("pw" + "2026-10-06T00:00:00+00:00"))
	if info["passwordHash"] != hex.EncodeToString(want[:]) {
		t.Fatalf("hash %v", info["passwordHash"])
	}
	for _, k := range []string{"canStreamBooks", "canStreamMetadata", "canReceiveBookBinary", "canDeleteMultipleBooks"} {
		if info[k] != true {
			t.Errorf("%s not set", k)
		}
	}

	_, di := fc.call(proto.GetDeviceInformation, nil)
	uuid := di["device_info"].(map[string]any)["device_store_uuid"]
	if uuid == "" || uuid == nil {
		t.Fatal("no device_store_uuid")
	}
	fc.call(proto.SetCalibreDeviceInfo, map[string]any{"device_store_uuid": uuid, "device_name": "Kindle", "prefix": ""})
	if _, err := os.Stat(filepath.Join(lib.Root, "driveinfo.calibre")); err != nil {
		t.Fatal("driveinfo not saved")
	}
	_, fs := fc.call(proto.FreeSpace, nil)
	if fs["free_space_on_device"].(float64) <= 0 {
		t.Fatal("no free space reported")
	}

	fc.call(proto.Noop, map[string]any{"ejecting": true})
	if err := <-done; err != ErrEjected {
		t.Fatalf("got %v", err)
	}
}

func TestSendBookAndList(t *testing.T) {
	fc, lib, _ := setup(t, Config{Extensions: []string{"azw3"}})
	content := []byte("BOOKMOBI pretend book content")
	if err := fc.c.Send(proto.SendBook, map[string]any{
		"lpath": "Voss, Chris/Never Split.azw3", "length": len(content),
		"metadata": map[string]any{"title": "Never Split", "uuid": "u-1", "lpath": "Voss, Chris/Never Split.azw3",
			"last_modified": "2026-10-01T00:00:00+00:00", "size": 1, "series_index": 1.5},
		"thisBook": 0, "totalBooks": 1, "wantsSendOkToSendbook": true,
	}); err != nil {
		t.Fatal(err)
	}
	op, r := fc.recv()
	if op != proto.OK || r["lpath"] != "documents/Voss, Chris/Never Split.azw3" {
		t.Fatalf("send book reply %d %v", op, r)
	}
	// calibre streams the raw bytes straight after our OK.
	if err := fc.c.WriteRaw(bytes.NewReader(content)); err != nil {
		t.Fatal(err)
	}

	// The next message proves the stream stayed in sync.
	op, _ = fc.call(proto.Noop, nil)
	if op != proto.OK {
		t.Fatal("noop after book")
	}
	got, err := os.ReadFile(filepath.Join(lib.Root, "documents/Voss, Chris/Never Split.azw3"))
	if err != nil || string(got) != string(content) {
		t.Fatalf("book file %q %v", got, err)
	}

	// Book list with the metadata cache: short entries, then priKey requests.
	if err := fc.c.Send(proto.GetBookCount, map[string]any{"willUseCachedMetadata": true}); err != nil {
		t.Fatal(err)
	}
	_, cnt := fc.recv()
	if cnt["count"].(float64) != 1 {
		t.Fatalf("count %v", cnt)
	}
	_, short := fc.recv()
	if short["lpath"] != "documents/Voss, Chris/Never Split.azw3" || short["extension"] != ".azw3" || short["priKey"].(float64) != 0 {
		t.Fatalf("short entry %v", short)
	}
	fc.c.Send(proto.Noop, map[string]any{"count": 1})
	_, full := fc.call(proto.Noop, map[string]any{"priKey": 0})
	if full["title"] != "Never Split" || full["series_index"].(float64) != 1.5 {
		t.Fatalf("full metadata %v", full)
	}

	// metadata.calibre is in calibre's USB format: a JSON list.
	var saved []map[string]any
	b, _ := os.ReadFile(filepath.Join(lib.Root, "metadata.calibre"))
	if err := json.Unmarshal(b, &saved); err != nil || len(saved) != 1 {
		t.Fatalf("metadata.calibre: %v %s", err, b)
	}

	// Delete: one OK, then one OK {uuid} per lpath.
	op, _ = fc.call(proto.DeleteBook, map[string]any{"lpaths": []string{"documents/Voss, Chris/Never Split.azw3"}})
	if op != proto.OK {
		t.Fatal("delete ack")
	}
	_, del := fc.recv()
	if del["uuid"] != "u-1" {
		t.Fatalf("delete reply %v", del)
	}
	if _, err := os.Stat(filepath.Join(lib.Root, "documents/Voss, Chris/Never Split.azw3")); !os.IsNotExist(err) {
		t.Fatal("book not deleted")
	}
	if len(lib.Books) != 0 {
		t.Fatal("book still listed")
	}
	if _, err := os.Stat(filepath.Join(lib.Root, "documents/Voss, Chris")); !os.IsNotExist(err) {
		t.Fatal("empty author folder left behind")
	}
	if _, err := os.Stat(filepath.Join(lib.Root, "documents")); err != nil {
		t.Fatal("documents folder removed")
	}
}

func TestRefuseUnsafePath(t *testing.T) {
	fc, _, _ := setup(t, Config{})
	op, r := fc.call(proto.SendBook, map[string]any{
		"lpath": "../../etc/passwd", "length": 3, "metadata": map[string]any{},
		"thisBook": 0, "totalBooks": 1, "wantsSendOkToSendbook": true,
	})
	if op != proto.Error || r["message"] == nil {
		t.Fatalf("expected ERROR, got %d %v", op, r)
	}
	// Still in sync.
	if op, _ := fc.call(proto.Noop, nil); op != proto.OK {
		t.Fatal("noop after refusal")
	}
}

func TestEject(t *testing.T) {
	fc, _, done := setup(t, Config{})
	fc.call(proto.Noop, map[string]any{"ejecting": true})
	if err := <-done; err != ErrEjected {
		t.Fatalf("got %v", err)
	}
}
