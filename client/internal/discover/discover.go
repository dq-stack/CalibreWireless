// Package discover finds calibre's wireless device server via its UDP
// broadcast ports. See docs/PROTOCOL.md §1.
package discover

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"time"
)

// Ports calibre listens on for discovery datagrams.
var Ports = []int{54982, 48123, 39001, 44044, 59678}

var reply = regexp.MustCompile(`calibre wireless device client \(on (.*)\);(\d*),(\d+)$`)

// Server is a calibre instance that answered.
type Server struct {
	Host string // calibre's hostname as it reports it
	Addr string // IP to connect to
	Port int    // smart device TCP port
}

func (s Server) String() string { return fmt.Sprintf("%s (%s:%d)", s.Host, s.Addr, s.Port) }

// ParseReply parses calibre's discovery answer.
func ParseReply(msg string) (host string, port int, err error) {
	m := reply.FindStringSubmatch(msg)
	if m == nil {
		return "", 0, fmt.Errorf("unexpected discovery reply %q", msg)
	}
	port, err = strconv.Atoi(m[3])
	return m[1], port, err
}

// LocalPort is the fixed port we send from, so the Kindle's firewall can let
// calibre's reply in with a single rule (the same port KOReader uses).
const LocalPort = 8134

// Find broadcasts to each calibre port in turn and returns the first answer.
// target is normally "255.255.255.255"; tests can use 127.0.0.1.
// localPort 0 picks any free port.
func Find(target string, localPort int, wait time.Duration) (Server, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: localPort})
	if err != nil {
		return Server{}, err
	}
	defer conn.Close()
	// calibre answers on whichever of its ports it managed to open, so send
	// to all of them at once and take the first answer.
	var lastErr error = errors.New("no answer from calibre")
	sent := 0
	for _, p := range Ports {
		dst := &net.UDPAddr{IP: net.ParseIP(target), Port: p}
		if _, err := conn.WriteToUDP([]byte("hello"), dst); err != nil {
			lastErr = err
			continue
		}
		sent++
	}
	if sent == 0 {
		return Server{}, lastErr
	}
	buf := make([]byte, 512)
	conn.SetReadDeadline(time.Now().Add(wait))
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return Server{}, lastErr // timed out
		}
		host, port, err := ParseReply(string(buf[:n]))
		if err != nil {
			lastErr = err
			continue
		}
		return Server{Host: host, Addr: from.IP.String(), Port: port}, nil
	}
}

// LocalAddrs lists this device's IPv4 addresses, for logs and the status line.
func LocalAddrs() []string {
	var out []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && ipn.IP.To4() != nil {
			out = append(out, ipn.String())
		}
	}
	return out
}
