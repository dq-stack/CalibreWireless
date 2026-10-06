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
	"strings"
	"time"

	"github.com/dq-stack/kindle-calibre-wireless/client/internal/discover"
	"github.com/dq-stack/kindle-calibre-wireless/client/internal/library"
	"github.com/dq-stack/kindle-calibre-wireless/client/internal/proto"
	"github.com/dq-stack/kindle-calibre-wireless/client/internal/session"
)

var version = "0.1.0-dev"

func main() {
	root := flag.String("root", "/mnt/us", "storage root holding documents/ and metadata.calibre")
	host := flag.String("host", "", "calibre address (skips discovery)")
	port := flag.Int("port", 9090, "calibre port when -host is set")
	bcast := flag.String("broadcast", "255.255.255.255", "discovery broadcast address")
	password := flag.String("password", "", "calibre wireless password")
	name := flag.String("name", "Kindle", "device name shown in calibre")
	exts := flag.String("formats", "azw3,mobi,azw,kfx,pdf,txt", "accepted formats, preferred first")
	readCol := flag.String("read-col", "", "calibre Yes/No column for read status, e.g. #read")
	readDateCol := flag.String("read-date-col", "", "calibre Date column for last read, e.g. #last_read")
	idle := flag.Duration("idle", 30*time.Minute, "disconnect after this long without transfers")
	logFile := flag.String("log", "", "append log to this file (default stderr)")
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
	log.Printf("kcw %s starting, root %s", version, *root)

	if err := run(*root, *host, *port, *bcast, session.Config{
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

func run(root, host string, port int, bcast string, cfg session.Config) error {
	if st, err := os.Stat(root + "/" + library.BooksDir); err != nil || !st.IsDir() {
		return fmt.Errorf("%s/%s is not a folder", root, library.BooksDir)
	}
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	if host == "" {
		status("Looking for calibre…")
		var srv discover.Server
		var err error
		for i := 0; i < 5; i++ {
			if srv, err = discover.Find(bcast, 3*time.Second); err == nil {
				break
			}
		}
		if err != nil {
			status("calibre not found. Is the wireless connection started?")
			return fmt.Errorf("discovery: %w", err)
		}
		log.Printf("found calibre on %s", srv)
		addr = net.JoinHostPort(srv.Addr, fmt.Sprint(srv.Port))
	}

	lib, err := library.Open(root)
	if err != nil {
		return err
	}
	log.Printf("%d books in %s", len(lib.Books), library.MetadataFile)

	nc, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return err
	}
	conn := proto.New(nc)
	defer conn.Close()
	log.Printf("connected to %s", addr)

	s := session.New(conn, lib, cfg, session.Hooks{Status: status})
	err = s.Run()
	if errors.Is(err, session.ErrEjected) {
		status(fmt.Sprintf("Disconnected. %d book(s) received", s.Received()))
		return nil
	}
	return err
}

// status shows a message to the user. On the Mac it's just the log; the
// Kindle launcher reads the log's STATUS lines and draws them with FBInk.
func status(msg string) {
	log.Printf("STATUS %s", msg)
}
