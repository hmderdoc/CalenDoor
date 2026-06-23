// Keyboard input: a reader goroutine that decodes the door's byte stream into
// key events (printable runes + arrows/page/home/end/enter/esc), the same
// ESC-disambiguation trick the spekder door uses (read-ahead with a short
// timeout so a lone Esc isn't mistaken for the start of an arrow sequence).
package main

import (
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// key is a decoded keypress: a positive value is a literal rune; the negatives
// below are the special keys.
type key int

const (
	keyEsc   key = -1
	keyEnter key = -2
	keyUp    key = -3
	keyDown  key = -4
	keyLeft  key = -5
	keyRight key = -6
	keyPgUp  key = -7
	keyPgDn  key = -8
	keyHome  key = -9
	keyEnd   key = -10
	keyBack  key = -11
	keyTab   key = -12
)

const escTimeout = 60 * time.Millisecond

type input struct {
	events   chan key
	quitCh   chan struct{}
	quitOnce sync.Once
	rawMode  atomic.Bool    // when set, the reader forwards raw bytes (telnet gateway) instead of decoding keys
	rawCh    chan []byte    // raw byte chunks while rawMode is set
	resizeCh chan [2]int    // latest [cols,rows] from a terminal window-size report
}

func newInput(t Term) *input {
	in := &input{
		events:   make(chan key, 1024), // big buffer so a pasted URL burst isn't dropped
		quitCh:   make(chan struct{}),
		rawCh:    make(chan []byte, 256),
		resizeCh: make(chan [2]int, 1),
	}
	go in.reader(t)
	return in
}

func (in *input) signalQuit() { in.quitOnce.Do(func() { close(in.quitCh) }) }

func (in *input) push(k key) {
	select {
	case in.events <- k:
	default: // never block the reader on a full queue
	}
}

// escNext returns the next byte of an escape sequence, either from the current
// read buffer or by briefly blocking for one more byte over the channel.
func (in *input) escNext(t Term, buf []byte, n int, i *int) (byte, bool) {
	if *i+1 < n {
		*i++
		return buf[*i], true
	}
	var pb [1]byte
	if m, _ := t.ReadTimeout(pb[:], escTimeout); m > 0 {
		return pb[0], true
	}
	return 0, false
}

func (in *input) reader(t Term) {
	buf := make([]byte, 32)
	iac := 0
	for {
		n, err := t.Read(buf)
		if err != nil || n == 0 {
			logf("input reader exit: n=%d err=%v", n, err)
			in.signalQuit()
			return
		}
		if in.rawMode.Load() { // telnet gateway: forward bytes verbatim, no decoding
			cp := make([]byte, n)
			copy(cp, buf[:n])
			select {
			case in.rawCh <- cp:
			default: // relay drains promptly; drop only if wildly backed up
			}
			continue
		}
		for i := 0; i < n; i++ {
			c := buf[i]
			if iac > 0 { // telnet IAC: swallow the 2 option bytes
				iac--
				continue
			}
			if c == 0xFF {
				iac = 2
				continue
			}
			if c == 27 { // ESC: arrow/SS3 sequence, or a lone Esc
				nb, have := in.escNext(t, buf, n, &i)
				if have && (nb == '[' || nb == 'O') {
					fin, ok := in.escNext(t, buf, n, &i)
					var params []byte
					for ok && fin >= '0' && fin <= ';' { // CSI numeric params
						params = append(params, fin)
						fin, ok = in.escNext(t, buf, n, &i)
					}
					if ok {
						in.csi(fin, params)
					}
					continue
				}
				in.push(keyEsc)
				continue
			}
			switch c {
			case 3: // Ctrl-C
				in.signalQuit()
				return
			case '\r', '\n':
				in.push(keyEnter)
			case '\t':
				in.push(keyTab)
			case 0x7f, 0x08:
				in.push(keyBack)
			default:
				if c >= 0x20 && c < 0x7f {
					in.push(key(rune(c)))
				}
			}
		}
	}
}

// csi maps a CSI/SS3 final byte (+ any numeric params) to a special key.
func (in *input) csi(fin byte, params []byte) {
	switch fin {
	case 'A':
		in.push(keyUp)
	case 'B':
		in.push(keyDown)
	case 'C':
		in.push(keyRight)
	case 'D':
		in.push(keyLeft)
	case 'H':
		in.push(keyHome)
	case 'F':
		in.push(keyEnd)
	case 't': // xterm window-size report: ESC[8;rows;cols t
		in.onWindowReport(params)
	case '~':
		switch string(params) {
		case "5":
			in.push(keyPgUp)
		case "6":
			in.push(keyPgDn)
		case "1", "7":
			in.push(keyHome)
		case "4", "8":
			in.push(keyEnd)
		}
	}
}

// onWindowReport parses ESC[8;rows;cols t and posts the latest size (cols,rows).
func (in *input) onWindowReport(params []byte) {
	parts := strings.Split(string(params), ";")
	if len(parts) < 3 || parts[0] != "8" {
		return
	}
	rows, _ := strconv.Atoi(parts[1])
	cols, _ := strconv.Atoi(parts[2])
	if cols <= 0 || rows <= 0 {
		return
	}
	select { // keep only the latest report
	case <-in.resizeCh:
	default:
	}
	select {
	case in.resizeCh <- [2]int{cols, rows}:
	default:
	}
}
