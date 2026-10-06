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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/dq-stack/kindle-calibre-wireless/client/internal/discover"
	"github.com/dq-stack/kindle-calibre-wireless/client/internal/library"
	"github.com/dq-stack/kindle-calibre-wireless/client/internal/proto"
	"github.com/dq-stack/kindle-calibre-wireless/client/internal/session"
)

var version = "0.1.0-dev"

type options struct {
	root, host, bcast string
	port, udpPort     int
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

	status("Looking for calibre…")
	srv, err := discover.Find(o.bcast, o.udpPort, 3*time.Second)
	for i := 1; err != nil && i < 4; i++ {
		select {
		case <-stop:
			status("Stopped")
			return errors.New("stopped while looking for calibre")
		default:
		}
		srv, err = discover.Find(o.bcast, o.udpPort, 3*time.Second)
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
		status("calibre not found. In calibre: Connect/share > Start wireless device connection")
		return fmt.Errorf("discovery: %w", err)
	}

	lib, err := library.Open(o.root)
	if err != nil {
		return err
	}
	log.Printf("%d books in %s", len(lib.Books), library.MetadataFile)

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

	s := session.New(conn, lib, cfg, session.Hooks{Status: status})
	err = s.Run()
	switch {
	case errors.Is(err, session.ErrEjected):
		status(fmt.Sprintf("Disconnected. %d book(s) received", s.Received()))
		return nil
	case stopped.Load():
		status(fmt.Sprintf("Stopped. %d book(s) received", s.Received()))
		return nil
	}
	status("Connection lost")
	return err
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
