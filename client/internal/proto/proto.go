// Package proto implements the framing of calibre's Smart Device App protocol:
// length-prefixed JSON messages of the form <len>[opcode, {args}], with raw
// book bytes streamed in between. See docs/PROTOCOL.md §2.
package proto

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// Opcodes, from calibre's driver.py.
const (
	OK                    = 0
	SetCalibreDeviceInfo  = 1
	SetCalibreDeviceName  = 2
	GetDeviceInformation  = 3
	TotalSpace            = 4
	FreeSpace             = 5
	GetBookCount          = 6
	SendBooklists         = 7
	SendBook              = 8
	GetInitializationInfo = 9
	BookDone              = 11
	Noop                  = 12
	DeleteBook            = 13
	GetBookFileSegment    = 14
	GetBookMetadata       = 15
	SendBookMetadata      = 16
	DisplayMessage        = 17
	CalibreBusy           = 18
	SetLibraryInfo        = 19
	Error                 = 20
	GetCollections        = 21
	UpdateCollections     = 22
)

var names = map[int]string{
	OK: "OK", SetCalibreDeviceInfo: "SET_CALIBRE_DEVICE_INFO", SetCalibreDeviceName: "SET_CALIBRE_DEVICE_NAME",
	GetDeviceInformation: "GET_DEVICE_INFORMATION", TotalSpace: "TOTAL_SPACE", FreeSpace: "FREE_SPACE",
	GetBookCount: "GET_BOOK_COUNT", SendBooklists: "SEND_BOOKLISTS", SendBook: "SEND_BOOK",
	GetInitializationInfo: "GET_INITIALIZATION_INFO", BookDone: "BOOK_DONE", Noop: "NOOP",
	DeleteBook: "DELETE_BOOK", GetBookFileSegment: "GET_BOOK_FILE_SEGMENT", GetBookMetadata: "GET_BOOK_METADATA",
	SendBookMetadata: "SEND_BOOK_METADATA", DisplayMessage: "DISPLAY_MESSAGE", CalibreBusy: "CALIBRE_BUSY",
	SetLibraryInfo: "SET_LIBRARY_INFO", Error: "ERROR", GetCollections: "GET_COLLECTIONS",
	UpdateCollections: "UPDATE_COLLECTIONS",
}

// Name returns the calibre name of an opcode, for logs.
func Name(op int) string {
	if n, ok := names[op]; ok {
		return n
	}
	return "OP_" + strconv.Itoa(op)
}

// maxMessage guards against a corrupt length prefix. Book lists with thumbnails
// can be large, but a single message is one book's metadata at most.
const maxMessage = 64 << 20

// Conn is one TCP connection to calibre.
type Conn struct {
	c       net.Conn
	r       *bufio.Reader
	Timeout time.Duration // per-read deadline; 0 = none
}

func New(c net.Conn) *Conn {
	return &Conn{c: c, r: bufio.NewReaderSize(c, 64<<10)}
}

func (c *Conn) Close() error { return c.c.Close() }

func (c *Conn) deadline() {
	if c.Timeout > 0 {
		c.c.SetReadDeadline(time.Now().Add(c.Timeout))
	}
}

// Read returns the next JSON message's opcode and raw argument object.
func (c *Conn) Read() (int, json.RawMessage, error) {
	c.deadline()
	var digits []byte
	for {
		b, err := c.r.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		if b == '[' {
			break
		}
		if b < '0' || b > '9' || len(digits) > 12 {
			return 0, nil, fmt.Errorf("bad length prefix %q", append(digits, b))
		}
		digits = append(digits, b)
	}
	n, err := strconv.Atoi(string(digits))
	if err != nil || n < 2 || n > maxMessage {
		return 0, nil, fmt.Errorf("bad message length %q", digits)
	}
	buf := make([]byte, n)
	buf[0] = '['
	if _, err := io.ReadFull(c.r, buf[1:]); err != nil {
		return 0, nil, err
	}
	var msg []json.RawMessage
	if err := json.Unmarshal(buf, &msg); err != nil || len(msg) < 1 {
		return 0, nil, fmt.Errorf("bad message %.80q: %v", buf, err)
	}
	var op int
	if err := json.Unmarshal(msg[0], &op); err != nil {
		return 0, nil, fmt.Errorf("bad opcode %q", msg[0])
	}
	var arg json.RawMessage = []byte("{}")
	if len(msg) > 1 {
		arg = msg[1]
	}
	return op, arg, nil
}

// Send writes one JSON message.
func (c *Conn) Send(op int, arg any) error {
	if arg == nil {
		arg = struct{}{}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode([]any{op, arg}); err != nil {
		return err
	}
	body := bytes.TrimRight(b.Bytes(), "\n")
	out := make([]byte, 0, len(body)+12)
	out = strconv.AppendInt(out, int64(len(body)), 10)
	out = append(out, body...)
	_, err := c.c.Write(out)
	return err
}

// ReadRaw copies exactly n raw bytes (a book's content) to w.
func (c *Conn) ReadRaw(w io.Writer, n int64) error {
	for n > 0 {
		c.deadline()
		chunk := n
		if chunk > 1<<20 {
			chunk = 1 << 20
		}
		got, err := io.CopyN(w, c.r, chunk)
		n -= got
		if err != nil {
			return err
		}
	}
	return nil
}

// WriteRaw streams raw bytes to calibre.
func (c *Conn) WriteRaw(r io.Reader) error {
	_, err := io.Copy(c.c, r)
	return err
}
