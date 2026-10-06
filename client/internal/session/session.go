// Package session answers calibre's requests over one connection.
// calibre drives the conversation; we only reply. See docs/PROTOCOL.md §3-8.
package session

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/dq-stack/kindle-calibre-wireless/client/internal/library"
	"github.com/dq-stack/kindle-calibre-wireless/client/internal/proto"
)

// Config is what the user can set.
type Config struct {
	AppName     string
	Version     string
	DeviceName  string   // shown in calibre
	Password    string   // must match calibre's, if calibre has one
	Extensions  []string // formats we accept, in calibre's preference order
	CoverHeight int
	ReadCol     string // calibre Yes/No column for read status, e.g. "#read"
	ReadDateCol string // calibre Date column for last read, e.g. "#last_read"
	IdleTimeout time.Duration
}

// Hooks lets the Kindle-specific parts (covers, sidecars, collections, read
// status) plug in without the protocol code knowing about them.
type Hooks struct {
	// BookReceived runs after a book file is written and recorded.
	BookReceived func(abs string, b library.Book)
	// BookDeleted runs after a book file is removed.
	BookDeleted func(abs string, b library.Book)
	// ReadState fills _is_read_ / _last_read_date_ for one book; nil = no sync.
	ReadState func(abs string, b library.Book) (isRead any, lastRead any)
	// Collections receives calibre's collection map (name -> lpaths).
	Collections func(map[string][]string)
	// Status shows a short message to the user.
	Status func(string)
}

// ErrEjected means calibre ended the connection normally.
var ErrEjected = errors.New("calibre disconnected")

type Session struct {
	cfg   Config
	hooks Hooks
	lib   *library.Library
	c     *proto.Conn

	supportsSync bool
	received     int
	lastWork     time.Time
}

func New(c *proto.Conn, lib *library.Library, cfg Config, hooks Hooks) *Session {
	if hooks.Status == nil {
		hooks.Status = func(string) {}
	}
	return &Session{cfg: cfg, hooks: hooks, lib: lib, c: c}
}

// Run serves calibre until it disconnects, the link drops, or the idle
// timeout passes.
func (s *Session) Run() error {
	// calibre sends a NOOP about every 5 s while connected, so a long silence
	// means it went away without closing the socket.
	s.c.Timeout = 90 * time.Second
	s.lastWork = time.Now()
	for {
		op, arg, err := s.c.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return ErrEjected
			}
			return err
		}
		if op != proto.Noop {
			s.lastWork = time.Now()
			log.Printf("<- %s", proto.Name(op))
		}
		if err := s.handle(op, arg); err != nil {
			return err
		}
		if s.cfg.IdleTimeout > 0 && time.Since(s.lastWork) > s.cfg.IdleTimeout {
			return fmt.Errorf("idle for %s, disconnecting", s.cfg.IdleTimeout)
		}
	}
}

func (s *Session) ok(v any) error { return s.c.Send(proto.OK, v) }

type obj = map[string]any

func (s *Session) handle(op int, raw json.RawMessage) error {
	switch op {
	case proto.GetInitializationInfo:
		return s.initInfo(raw)
	case proto.GetDeviceInformation:
		return s.ok(obj{
			"device_info":    s.lib.Drive,
			"version":        s.cfg.Version,
			"device_version": s.cfg.Version,
		})
	case proto.SetCalibreDeviceInfo:
		var d map[string]any
		if err := json.Unmarshal(raw, &d); err == nil && d != nil {
			s.lib.Drive = d
			if err := s.lib.SaveDrive(); err != nil {
				log.Printf("saving driveinfo: %v", err)
			}
		}
		return s.ok(nil)
	case proto.SetCalibreDeviceName, proto.SetLibraryInfo:
		return s.ok(nil)
	case proto.FreeSpace:
		free, _ := s.lib.Space()
		return s.ok(obj{"free_space_on_device": free})
	case proto.TotalSpace:
		_, total := s.lib.Space()
		return s.ok(obj{"total_space_on_device": total})
	case proto.GetBookCount:
		return s.bookCount(raw)
	case proto.Noop:
		return s.noop(raw)
	case proto.SendBook:
		return s.sendBook(raw)
	case proto.SendBooklists:
		return s.booklists(raw)
	case proto.SendBookMetadata:
		return s.bookMetadata(raw)
	case proto.DeleteBook:
		return s.deleteBooks(raw)
	case proto.GetBookFileSegment:
		return s.fileSegment(raw)
	case proto.DisplayMessage:
		var a struct{ MessageKind int }
		json.Unmarshal(raw, &a)
		if a.MessageKind == 1 {
			s.hooks.Status("Wrong calibre password")
			return errors.New("calibre rejected the password")
		}
		return nil
	case proto.CalibreBusy:
		s.hooks.Status("calibre is busy with another device")
		return errors.New("calibre is busy with another device")
	default:
		log.Printf("ignoring opcode %s: %.200s", proto.Name(op), raw)
		return nil
	}
}

func (s *Session) initInfo(raw json.RawMessage) error {
	var a struct {
		PasswordChallenge  string `json:"passwordChallenge"`
		CurrentLibraryName string `json:"currentLibraryName"`
		CalibreVersion     []int  `json:"calibre_version"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return err
	}
	log.Printf("calibre %v, library %q", a.CalibreVersion, a.CurrentLibraryName)
	hash := ""
	if a.PasswordChallenge != "" {
		h := sha1.Sum([]byte(s.cfg.Password + a.PasswordChallenge))
		hash = hex.EncodeToString(h[:])
	}
	pathLens := obj{}
	for _, e := range s.cfg.Extensions {
		pathLens[e] = 37
	}
	info := obj{
		"versionOK":               true,
		"appName":                 s.cfg.AppName,
		"ccVersionNumber":         s.cfg.Version,
		"deviceKind":              s.cfg.DeviceName,
		"deviceName":              s.cfg.DeviceName,
		"acceptedExtensions":      s.cfg.Extensions,
		"extensionPathLengths":    pathLens,
		"maxBookContentPacketLen": 65536,
		"canStreamBooks":          true,
		"canStreamMetadata":       true,
		"canReceiveBookBinary":    true,
		"canDeleteMultipleBooks":  true,
		"canSendOkToSendbook":     true,
		"canAcceptLibraryInfo":    true,
		"canUseCachedMetadata":    true,
		"cacheUsesLpaths":         true,
		"useUuidFileNames":        false,
		"coverHeight":             s.cfg.CoverHeight,
		"passwordHash":            hash,
	}
	if s.cfg.ReadCol != "" {
		info["isReadSyncCol"] = s.cfg.ReadCol
	}
	if s.cfg.ReadDateCol != "" {
		info["isReadDateSyncCol"] = s.cfg.ReadDateCol
	}
	s.hooks.Status("Connected to calibre")
	return s.ok(info)
}

func (s *Session) bookCount(raw json.RawMessage) error {
	var a struct {
		WillUseCachedMetadata bool `json:"willUseCachedMetadata"`
		SupportsSync          bool `json:"supportsSync"`
	}
	json.Unmarshal(raw, &a)
	s.supportsSync = a.SupportsSync
	if n := s.lib.Prune(); n > 0 {
		log.Printf("dropped %d books no longer on the device", n)
		s.lib.Save()
	}
	books := s.lib.Books
	if err := s.ok(obj{"count": len(books), "willStream": true, "willScan": true}); err != nil {
		return err
	}
	for i, b := range books {
		var m obj
		if a.WillUseCachedMetadata {
			m = obj{
				"priKey":        i,
				"uuid":          b["uuid"],
				"lpath":         b.LPath(),
				"last_modified": b["last_modified"],
				"extension":     path.Ext(b.LPath()),
			}
		} else {
			m = s.fullMetadata(b)
		}
		s.addReadState(m, b)
		if err := s.ok(m); err != nil {
			return err
		}
	}
	return nil
}

// fullMetadata is the stored book plus the read-sync fields.
func (s *Session) fullMetadata(b library.Book) obj {
	m := obj{}
	for k, v := range b {
		m[k] = v
	}
	s.addReadState(m, b)
	return m
}

func (s *Session) addReadState(m obj, b library.Book) {
	if !s.supportsSync || s.hooks.ReadState == nil {
		return
	}
	isRead, lastRead := s.hooks.ReadState(s.lib.Abs(b.LPath()), b)
	m["_is_read_"] = isRead
	m["_last_read_date_"] = lastRead
}

func (s *Session) noop(raw json.RawMessage) error {
	var a struct {
		Ejecting bool `json:"ejecting"`
		Count    *int `json:"count"`
		PriKey   *int `json:"priKey"`
	}
	json.Unmarshal(raw, &a)
	switch {
	case a.Ejecting:
		s.ok(nil)
		return ErrEjected
	case a.Count != nil:
		return nil // announces how many priKey requests follow; no reply
	case a.PriKey != nil:
		if *a.PriKey < 0 || *a.PriKey >= len(s.lib.Books) {
			return fmt.Errorf("calibre asked for unknown book %d", *a.PriKey)
		}
		return s.ok(s.fullMetadata(s.lib.Books[*a.PriKey]))
	default:
		return s.ok(nil)
	}
}

func (s *Session) sendBook(raw json.RawMessage) error {
	var a struct {
		LPath      string          `json:"lpath"`
		Length     int64           `json:"length"`
		Metadata   json.RawMessage `json:"metadata"`
		ThisBook   int             `json:"thisBook"`
		TotalBooks int             `json:"totalBooks"`
		WantsOK    bool            `json:"wantsSendOkToSendbook"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return err
	}
	if !a.WantsOK {
		// We always advertise canSendOkToSendbook; without it we couldn't refuse
		// or move a book, so treat this as a protocol error.
		return errors.New("calibre did not ask for SEND_BOOK confirmation")
	}
	refuse := func(why string) error {
		log.Printf("refusing %q: %s", a.LPath, why)
		s.hooks.Status("Can't receive " + path.Base(a.LPath) + ": " + why)
		return s.c.Send(proto.Error, obj{"message": why})
	}
	lpath, err := library.CleanLPath(a.LPath)
	if err != nil {
		return refuse(err.Error())
	}
	var meta library.Book
	if err := decodeNumbers(a.Metadata, &meta); err != nil || meta == nil {
		return refuse("invalid book metadata")
	}
	if free, _ := s.lib.Space(); free < uint64(a.Length)+128<<10 {
		return refuse("not enough free space on the Kindle")
	}
	abs := s.lib.Abs(lpath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return refuse("can't create folder")
	}
	if !s.lib.Contained(abs) {
		return refuse("path is outside the documents folder")
	}
	tmp := abs + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return refuse("can't write file")
	}
	if err := s.ok(obj{"lpath": lpath}); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	s.hooks.Status(fmt.Sprintf("Receiving %d/%d: %s", a.ThisBook+1, a.TotalBooks, meta.Str("title")))
	err = s.c.ReadRaw(f, a.Length)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, abs)
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("receiving %s: %w", lpath, err)
	}
	meta["lpath"] = lpath
	meta["size"] = a.Length
	s.lib.Put(meta)
	if err := s.lib.Save(); err != nil {
		log.Printf("saving book list: %v", err)
	}
	s.received++
	log.Printf("received %s (%d bytes)", lpath, a.Length)
	if s.hooks.BookReceived != nil {
		s.hooks.BookReceived(abs, meta)
	}
	if a.ThisBook+1 == a.TotalBooks {
		s.hooks.Status(fmt.Sprintf("Received %d book(s)", a.TotalBooks))
	}
	return nil
}

func (s *Session) booklists(raw json.RawMessage) error {
	var a struct {
		Count       int                 `json:"count"`
		Collections map[string][]string `json:"collections"`
	}
	json.Unmarshal(raw, &a)
	if s.hooks.Collections != nil && a.Collections != nil {
		s.hooks.Collections(a.Collections)
	}
	if a.Count == 0 {
		return s.lib.Save()
	}
	return nil // SEND_BOOK_METADATA messages follow
}

func (s *Session) bookMetadata(raw json.RawMessage) error {
	var a struct {
		Index int             `json:"index"`
		Count int             `json:"count"`
		Data  json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return err
	}
	var b library.Book
	if err := decodeNumbers(a.Data, &b); err == nil && b != nil {
		if s.lib.Index(b.LPath()) >= 0 {
			s.lib.Put(b)
		}
	}
	if a.Index+1 >= a.Count {
		return s.lib.Save()
	}
	return nil
}

func (s *Session) deleteBooks(raw json.RawMessage) error {
	var a struct {
		LPaths []string `json:"lpaths"`
	}
	json.Unmarshal(raw, &a)
	if err := s.ok(nil); err != nil {
		return err
	}
	for _, lp := range a.LPaths {
		uuid := ""
		clean, err := library.CleanLPath(lp)
		if err == nil && clean == lp {
			abs := s.lib.Abs(lp)
			if b := s.lib.Get(lp); b != nil {
				uuid = b.Str("uuid")
				if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
					log.Printf("deleting %s: %v", lp, err)
				}
				s.lib.Remove(lp)
				if s.hooks.BookDeleted != nil {
					s.hooks.BookDeleted(abs, b)
				}
				log.Printf("deleted %s", lp)
			} else {
				log.Printf("asked to delete unknown book %s", lp)
			}
		} else {
			log.Printf("refusing to delete unsafe path %q", lp)
		}
		if err := s.ok(obj{"uuid": uuid}); err != nil {
			return err
		}
	}
	s.hooks.Status(fmt.Sprintf("Deleted %d book(s)", len(a.LPaths)))
	return s.lib.Save()
}

func (s *Session) fileSegment(raw json.RawMessage) error {
	var a struct {
		LPath string `json:"lpath"`
	}
	json.Unmarshal(raw, &a)
	clean, err := library.CleanLPath(a.LPath)
	var f *os.File
	if err == nil && clean == a.LPath {
		f, err = os.Open(s.lib.Abs(a.LPath))
	}
	if err != nil {
		// calibre loops until it gets OK; an empty file ends the request.
		log.Printf("calibre asked for %q: %v", a.LPath, err)
		return s.ok(obj{"fileLength": 0})
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return s.ok(obj{"fileLength": 0})
	}
	if err := s.ok(obj{"fileLength": st.Size()}); err != nil {
		return err
	}
	return s.c.WriteRaw(io.LimitReader(f, st.Size()))
}

func decodeNumbers(raw json.RawMessage, v any) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	return d.Decode(v)
}

// Received is how many books arrived in this session.
func (s *Session) Received() int { return s.received }
