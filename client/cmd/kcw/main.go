// kcw is a calibre wireless device client for the stock Kindle reader.
// It finds calibre on the network, connects, and serves until calibre
// disconnects. The same binary runs on the Mac as a test client.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dq-stack/kindle-calibre-wireless/client/internal/discover"
	"github.com/dq-stack/kindle-calibre-wireless/client/internal/kindle"
	"github.com/dq-stack/kindle-calibre-wireless/client/internal/library"
	"github.com/dq-stack/kindle-calibre-wireless/client/internal/proto"
	"github.com/dq-stack/kindle-calibre-wireless/client/internal/session"
)

var version = "0.1.0-dev"

type options struct {
	root, host, bcast string
	port, udpPort     int
	apnx              bool
}

func main() {
	var o options
	flag.StringVar(&o.root, "root", "/mnt/us", "storage root holding documents/ and metadata.calibre")
	flag.StringVar(&o.host, "host", "", "calibre address; used when discovery finds nothing")
	flag.IntVar(&o.port, "port", 9090, "calibre port for -host")
	flag.StringVar(&o.bcast, "broadcast", "255.255.255.255", "discovery broadcast address")
	flag.IntVar(&o.udpPort, "udp-port", discover.LocalPort, "local UDP port for discovery (0 = any)")
	password := flag.String("password", "", "calibre wireless password")
	name := flag.String("name", "Kindle", "device name shown in calibre")
	exts := flag.String("formats", "azw3,mobi,azw,kfx,pdf,txt", "accepted formats, preferred first")
	readCol := flag.String("read-col", "", "calibre Yes/No column for read status, e.g. #read")
	readDateCol := flag.String("read-date-col", "", "calibre Date column for last read, e.g. #last_read")
	idle := flag.Duration("idle", 30*time.Minute, "disconnect after this long without transfers")
	logFile := flag.String("log", "", "append log to this file (default stderr)")
	statusCmd := flag.String("status", "", "program that shows status messages on screen (fbink or eips)")
	flag.BoolVar(&o.apnx, "apnx", true, "write page numbers (APNX, calibre's fast method) for MOBI/AZW3 books")
	flag.Parse()

	if *logFile != "" {
		f, err := os.OpenFile(*logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		log.SetOutput(io.MultiWriter(os.Stderr, f))
	}
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	log.Printf("kcw %s starting, root %s", version, o.root)
	screen = *statusCmd

	if err := run(o, session.Config{
		AppName:     "KindleCalibreWireless",
		Version:     version,
		DeviceName:  *name,
		Password:    *password,
		Extensions:  strings.Split(*exts, ","),
		CoverHeight: 500,
		ReadCol:     *readCol,
		ReadDateCol: *readDateCol,
		IdleTimeout: *idle,
	}); err != nil {
		log.Printf("stopped: %v", err)
		os.Exit(1)
	}
	log.Printf("stopped: calibre disconnected")
}

func run(o options, cfg session.Config) error {
	if st, err := os.Stat(o.root + "/" + library.BooksDir); err != nil || !st.IsDir() {
		return fmt.Errorf("%s/%s is not a folder", o.root, library.BooksDir)
	}

	// SIGTERM/SIGINT (the scriptlet tapped again, USB plugged in) close the
	// connection; the session then unwinds and removes any half-written book.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	var stopped atomic.Bool

	local := discover.LocalAddrs()
	log.Printf("this device: %v", local)
	where := ""
	if len(local) > 0 {
		where = " from " + strings.SplitN(local[0], "/", 2)[0]
	}
	status("Looking for calibre" + where + "…")
	// With a fixed HOST, one quick search is enough before falling back to it.
	rounds := 3
	if o.host != "" {
		rounds = 1
	}
	var srv discover.Server
	var err error
	for i := 0; i < rounds; i++ {
		select {
		case <-stop:
			status("Stopped")
			return errors.New("stopped while looking for calibre")
		default:
		}
		if srv, err = discover.Find(o.bcast, o.udpPort, 3*time.Second); err == nil {
			break
		}
	}
	var addr string
	switch {
	case err == nil:
		log.Printf("found calibre on %s", srv)
		addr = net.JoinHostPort(srv.Addr, fmt.Sprint(srv.Port))
	case o.host != "":
		log.Printf("discovery failed (%v), using %s", err, o.host)
		addr = net.JoinHostPort(o.host, fmt.Sprint(o.port))
	default:
		status("calibre not found" + where + ". Same network? Wireless connection started?")
		return fmt.Errorf("discovery: %w", err)
	}

	lib, err := library.Open(o.root)
	if err != nil {
		return err
	}
	log.Printf("%d books in %s", len(lib.Books), library.MetadataFile)
	covers := kindle.Covers{Root: o.root}
	fixCovers(covers, lib, "connect")
	defer fixCovers(covers, lib, "disconnect")
	if o.apnx {
		addPageNumbers(lib)
	}

	// While online the Kindle can swap a new book's cover for Amazon's
	// placeholder a few seconds after indexing it; put ours back promptly.
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	go func() {
		for range tick.C {
			if r, _ := covers.Restore(); len(r) > 0 {
				log.Printf("covers restored during session: %v", r)
			}
		}
	}()

	catalog := kindle.NewCatalog()
	if !catalog.Available() {
		catalog = nil // e.g. the Mac test client
	}
	var jobs sync.WaitGroup
	defer func() {
		done := make(chan struct{})
		go func() { jobs.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(90 * time.Second):
			log.Printf("gave up waiting for background jobs")
		}
	}()

	nc, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		status("Can't connect to calibre at " + addr)
		return err
	}
	conn := proto.New(nc)
	defer conn.Close()
	log.Printf("connected to %s", addr)
	status("Connected to calibre at " + addr)
	go func() {
		<-stop
		stopped.Store(true)
		conn.Close()
	}()

	s := session.New(conn, lib, cfg, session.Hooks{
		Status: status,
		BookReady: func(tmp string, b library.Book) {
			final := strings.TrimSuffix(tmp, ".part")
			switch {
			case kindle.HasCover(tmp):
				if name, err := covers.Write(tmp, b["thumbnail"]); err != nil {
					log.Printf("cover for %s: %v", b.LPath(), err)
				} else {
					log.Printf("cover written: %s", name)
				}
				if o.apnx {
					if n, err := kindle.WriteAPNX(tmp, final); err != nil {
						log.Printf("page numbers for %s: %v", b.LPath(), err)
					} else {
						log.Printf("page numbers written: %d pages", n)
					}
				}
			case kindle.IsPDF(tmp):
				thumb, err := covers.WritePDF(final, b["thumbnail"])
				if err != nil {
					log.Printf("PDF cover for %s: %v", b.LPath(), err)
					return
				}
				log.Printf("PDF cover written: %s", thumb)
				if catalog != nil {
					jobs.Add(1)
					go func() {
						defer jobs.Done()
						setPDFThumbnail(catalog, final, thumb)
					}()
				}
			}
		},
		BookDeleting: func(abs string, b library.Book) {
			switch {
			case kindle.HasCover(abs):
				if err := covers.Remove(abs); err != nil {
					log.Printf("removing cover for %s: %v", b.LPath(), err)
				}
			case kindle.IsPDF(abs):
				covers.RemovePDF(abs)
			}
		},
		BookDeleted: func(abs string, b library.Book) {
			kindle.RemoveSidecars(abs)
		},
	})
	err = s.Run()
	switch {
	case errors.Is(err, session.ErrEjected):
		status(fmt.Sprintf("Disconnected. %d book(s) received", s.Received()))
		return nil
	case stopped.Load():
		status(fmt.Sprintf("Stopped. %d book(s) received", s.Received()))
		return nil
	}
	var shown session.ShownError
	if !errors.As(err, &shown) {
		status("Connection lost")
	}
	return err
}

// fixCovers puts back covers the Kindle replaced (the Amazon cover bug) and
// writes covers for calibre books that have none yet.
func fixCovers(c kindle.Covers, lib *library.Library, when string) {
	restored, err := c.Restore()
	if err != nil {
		log.Printf("cover restore (%s): %v", when, err)
	}
	for _, n := range restored {
		log.Printf("cover restored (%s): %s", when, n)
	}
	added := 0
	for _, b := range lib.Books {
		abs := lib.Abs(b.LPath())
		if b["thumbnail"] == nil || !kindle.HasCover(abs) {
			continue
		}
		missing, err := c.NotCached(abs)
		if err != nil || !missing {
			continue
		}
		if name, err := c.Write(abs, b["thumbnail"]); err == nil {
			log.Printf("cover added (%s): %s", when, name)
			added++
		}
	}
	if len(restored) > 0 || added > 0 {
		log.Printf("covers (%s): %d restored, %d added", when, len(restored), added)
	}
}

// addPageNumbers writes page numbers for calibre books that have none.
func addPageNumbers(lib *library.Library) {
	added := 0
	for _, b := range lib.Books {
		abs := lib.Abs(b.LPath())
		// Only books calibre sent (they carry a cover); not Amazon purchases.
		if b["thumbnail"] == nil || !kindle.HasCover(abs) {
			continue
		}
		if _, err := os.Stat(kindle.APNXPath(abs)); err == nil {
			continue
		}
		if n, err := kindle.WriteAPNX(abs, abs); err != nil {
			log.Printf("page numbers for %s: %v", b.LPath(), err)
		} else {
			log.Printf("page numbers added: %s (%d pages)", b.LPath(), n)
			added++
		}
	}
	if added > 0 {
		log.Printf("page numbers added for %d book(s)", added)
	}
}

// setPDFThumbnail waits for the Kindle to index a new PDF, then points its
// catalog entry at our cover file. EXPERIMENTAL: the "thumbnail" field name
// is inferred from cc.db's p_thumbnail column.
func setPDFThumbnail(c *kindle.Catalog, path, thumb string) {
	e, err := c.WaitFor(path, 75*time.Second)
	if err != nil {
		log.Printf("PDF cover: %v", err)
		return
	}
	log.Printf("PDF indexed: uuid=%s key=%s type=%s thumbnail=%q", e.UUID, e.CdeKey, e.CdeType, e.Thumbnail)
	if e.Thumbnail == thumb {
		return
	}
	err = c.Change([]map[string]any{{"update": map[string]any{
		"type": "Entry:Item", "uuid": e.UUID, "thumbnail": thumb,
	}}})
	if err != nil {
		log.Printf("PDF cover: catalog update failed: %v", err)
		return
	}
	if e2, err := c.WaitFor(path, 5*time.Second); err == nil {
		log.Printf("PDF cover: catalog now has thumbnail=%q", e2.Thumbnail)
	}
}

// screen is the status display program: an FBInk binary, eips, or "".
var screen string

// status logs a message and draws it near the bottom of the screen.
func status(msg string) {
	log.Printf("STATUS %s", msg)
	if screen == "" {
		return
	}
	var cmd *exec.Cmd
	if strings.HasSuffix(screen, "eips") {
		// eips has no "clear line"; pad so a shorter message hides a longer one.
		cmd = exec.Command(screen, "0", "38", fmt.Sprintf("%-60.60s", " "+msg))
	} else {
		cmd = exec.Command(screen, "-q", "-m", "-p", "-y", "-4", msg)
	}
	if err := cmd.Run(); err != nil {
		log.Printf("status display: %v", err)
	}
}
