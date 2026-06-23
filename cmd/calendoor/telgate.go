// Telnet gateway ("TELGATE"): when an event is happening NOW and its LOCATION is
// a telnet:// address, the caller can hop straight to that BBS from the door. We
// dial the target and transparently relay bytes both ways between the caller's
// terminal and the remote, the way Synchronet's telnet_gate does - with Ctrl-]
// as the local escape to drop the link and come back.
package main

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"strings"
	"time"
)

const ctrlCloseBracket = 0x1d // Ctrl-]  (classic telnet escape; drops the gate)

// telnetTarget parses a "telnet://host[:port]" LOCATION into a dial address
// (default port 23). ok=false if it isn't a telnet URL.
func telnetTarget(loc string) (string, bool) {
	s := strings.TrimSpace(loc)
	if !strings.HasPrefix(strings.ToLower(s), "telnet://") {
		return "", false
	}
	s = s[len("telnet://"):]
	if i := strings.IndexAny(s, "/ \t"); i >= 0 { // drop any path/garbage after the host
		s = s[:i]
	}
	if s == "" {
		return "", false
	}
	host, port := s, "23"
	if h, p, err := net.SplitHostPort(s); err == nil {
		host, port = h, p
	}
	if host == "" {
		return "", false
	}
	return net.JoinHostPort(host, port), true
}

// hostPart returns the lowercased host of a "host[:port]" string.
func hostPart(s string) string {
	if h, _, err := net.SplitHostPort(s); err == nil {
		return strings.ToLower(h)
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// isLocalHost reports whether a dial target points back at this BBS (so we offer
// "you're here" instead of a hop). Needs cfg.Host to be set.
func isLocalHost(target string, cfg config) bool {
	return cfg.Host != "" && hostPart(target) == hostPart(cfg.Host)
}

// joinTargetFor returns the dial address + label if the event is happening NOW
// and carries a telnet:// location.
func joinTargetFor(e Event, now time.Time) (host, label string, ok bool) {
	if e.Start.After(now) || !e.End.After(now) { // not currently happening
		return "", "", false
	}
	if h, good := telnetTarget(e.Location); good {
		return h, e.Summary, true
	}
	return "", "", false
}

// runTelnetGate connects to host and relays until the remote closes or the caller
// presses Ctrl-]. The screen is handed wholesale to the remote during the relay.
func runTelnetGate(w *bufio.Writer, in *input, term Term, cols, rows int, host, label string) {
	cls(w)
	at(w, 1, 1, sgr("1;30;46", center("Telnet gateway", cols)))
	at(w, 3, 2, sgr("1;37", "Hopping to: "+truncate(label, cols-14)))
	at(w, 4, 2, sgr("0;37", "Connecting to "+host+" ..."))
	w.Flush()

	conn, err := net.DialTimeout("tcp", host, 12*time.Second)
	if err != nil {
		at(w, 6, 2, sgr("1;31", truncate("Could not connect: "+err.Error(), cols-2)))
		at(w, 8, 2, sgr("0;37", "Press any key to return."))
		w.Flush()
		waitKey(in)
		return
	}
	logf("telgate: connected to %s", host)
	defer conn.Close()

	at(w, 6, 2, sgr("1;32", "Connected.  Press Ctrl-]  to disconnect and return."))
	w.Flush()
	time.Sleep(700 * time.Millisecond)
	w.WriteString("\x1b[2J\x1b[H")
	w.Flush()

	// Remote -> caller: straight through to the terminal (bypassing the door's
	// buffered writer, which we've flushed).
	in.rawMode.Store(true)
	done := make(chan struct{})
	go func() {
		io.Copy(term, conn)
		close(done)
	}()

	// Caller -> remote: raw bytes, watching for the Ctrl-] escape.
relay:
	for {
		select {
		case <-done: // remote closed
			break relay
		case <-in.quitCh: // caller dropped carrier
			break relay
		case b := <-in.rawCh:
			if i := bytes.IndexByte(b, ctrlCloseBracket); i >= 0 {
				if i > 0 {
					conn.Write(b[:i])
				}
				logf("telgate: user disconnect (Ctrl-])")
				break relay
			}
			if _, err := conn.Write(b); err != nil {
				break relay
			}
		}
	}
	conn.Close()
	in.rawMode.Store(false)
	drainRaw(in) // discard any bytes that arrived during the mode switch

	cls(w)
	at(w, rows/2, (cols-30)/2+1, sgr("1;37", "Disconnected - back to calendar."))
	w.Flush()
	time.Sleep(600 * time.Millisecond)
}

// waitKey blocks for a single keypress (or carrier drop).
func waitKey(in *input) {
	select {
	case <-in.quitCh:
	case <-in.events:
	}
}

// drainRaw empties any leftover raw chunks without blocking.
func drainRaw(in *input) {
	for {
		select {
		case <-in.rawCh:
		default:
			return
		}
	}
}
