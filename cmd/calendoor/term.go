// Platform-neutral terminal I/O: the Term abstraction the rest of the door
// talks to, dropfile parsing, and the ANSI terminal-size probe. The concrete
// Term implementations (raw tty + inherited socket on unix; console VT + Winsock
// on Windows) live in io_unix.go / io_windows.go behind build tags.
package main

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// Term is the door's single I/O channel. It is either an inherited connection
// from the BBS (a telnet/raw socket handed over via the dropfile) or the
// process's standard input/output. Implementations put the local terminal into
// raw mode where applicable.
type Term interface {
	io.Writer
	io.Closer
	// Read blocks until at least one input byte is available, returning a
	// non-nil error (e.g. io.EOF) when the connection drops. Transient
	// would-block conditions on a non-blocking socket are handled internally.
	Read(p []byte) (int, error)
	// ReadTimeout reads whatever input is available within d and returns
	// (0, nil) on timeout. Used only by the size probe, before the reader
	// goroutine starts.
	ReadTimeout(p []byte, d time.Duration) (int, error)
}

// readDoor32 reads a DOOR32.SYS dropfile and returns the comm handle when the
// caller is on a socket the BBS inherited to us. DOOR32.SYS line 1 is the comm
// type (0=local, 1=serial, 2=telnet socket) and line 2 is the handle. We accept
// type 2 (socket); anything else -> stdio. This is the cross-BBS standard
// dropfile (Synchronet, Mystic, ENiGMA1/2, Talisman, ... all emit it).
func readDoor32(path string) (uint64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var lines []string
	for sc.Scan() {
		lines = append(lines, strings.TrimSpace(sc.Text()))
	}
	if len(lines) < 2 || lines[0] != "2" {
		return 0, false
	}
	h, err := strconv.ParseUint(lines[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return h, true
}

// probeSize asks the caller's terminal for its dimensions over the I/O channel,
// the same approach the avatar_chat door uses: the cursor-position trick (park
// the cursor at the extreme so the terminal clamps to its real size, then DSR),
// with xterm's window-size report as a fallback. Used when the OS can't report
// the size directly (i.e. the door/socket path).
//
// MUST be called BEFORE the input reader goroutine starts, or it would swallow
// the ESC[<rows>;<cols>R reply.
func probeSize(t Term, timeout time.Duration) (cols, rows int, ok bool) {
	if c, r, found := probeWith(t, []byte("\x1b7\x1b[999;999H\x1b[6n\x1b8"), parseCursorReport, timeout); found {
		return c, r, true
	}
	if c, r, found := probeWith(t, []byte("\x1b[18t"), parseWindowReport, timeout); found {
		return c, r, true
	}
	return 0, 0, false
}

func probeWith(t Term, query []byte, parse func([]byte) (int, int, bool), timeout time.Duration) (cols, rows int, ok bool) {
	if _, err := t.Write(query); err != nil {
		return 0, 0, false
	}
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 0, 128)
	scratch := make([]byte, 64)
	for time.Now().Before(deadline) && len(buf) < 256 {
		rem := time.Until(deadline)
		if rem > 50*time.Millisecond {
			rem = 50 * time.Millisecond
		}
		n, err := t.ReadTimeout(scratch, rem)
		if n > 0 {
			buf = append(buf, scratch[:n]...)
			if r, c, found := parse(buf); found {
				return c, r, true
			}
		} else if err != nil {
			break
		}
	}
	return 0, 0, false
}

// parseCursorReport scans for ESC [ <rows> ; <cols> R (DSR cursor report).
func parseCursorReport(buf []byte) (rows, cols int, ok bool) {
	for i := 0; i+4 < len(buf); i++ {
		if buf[i] != 0x1B || buf[i+1] != '[' {
			continue
		}
		j, r := i+2, 0
		for j < len(buf) && buf[j] >= '0' && buf[j] <= '9' {
			r = r*10 + int(buf[j]-'0')
			j++
		}
		if r == 0 || j >= len(buf) || buf[j] != ';' {
			continue
		}
		j++
		c := 0
		for j < len(buf) && buf[j] >= '0' && buf[j] <= '9' {
			c = c*10 + int(buf[j]-'0')
			j++
		}
		if c == 0 || j >= len(buf) || buf[j] != 'R' {
			continue
		}
		return r, c, true
	}
	return 0, 0, false
}

// parseWindowReport scans for ESC [ 8 ; <rows> ; <cols> t (xterm size report).
func parseWindowReport(buf []byte) (rows, cols int, ok bool) {
	for i := 0; i+5 < len(buf); i++ {
		if buf[i] != 0x1B || buf[i+1] != '[' || buf[i+2] != '8' || buf[i+3] != ';' {
			continue
		}
		j, r := i+4, 0
		for j < len(buf) && buf[j] >= '0' && buf[j] <= '9' {
			r = r*10 + int(buf[j]-'0')
			j++
		}
		if r == 0 || j >= len(buf) || buf[j] != ';' {
			continue
		}
		j++
		c := 0
		for j < len(buf) && buf[j] >= '0' && buf[j] <= '9' {
			c = c*10 + int(buf[j]-'0')
			j++
		}
		if c == 0 || j >= len(buf) || buf[j] != 't' {
			continue
		}
		return r, c, true
	}
	return 0, 0, false
}
