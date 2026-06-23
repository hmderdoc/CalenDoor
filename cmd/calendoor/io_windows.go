//go:build windows

// Windows terminal I/O. Two channels, matching the unix layer:
//
//   - door mode: the BBS hands us an inherited Winsock SOCKET via DOOR32.SYS.
//     Unlike unix, that handle is NOT a file descriptor, so it cannot be wrapped
//     with os.NewFile - we talk to it with WSARecv/WSASend directly. This is the
//     common path on Windows BBSes (Synchronet Win32, Mystic Win32, ...).
//   - stdio mode: no socket in the dropfile -> read/write the console (or a
//     redirected pipe from a door launcher). We enable VT processing/input on a
//     real console so the same ANSI renderer and key reader work unchanged.
package main

import (
	"io"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// shutdownSignals: Windows only meaningfully delivers Interrupt (Ctrl-C / Ctrl-
// Break). SIGTERM is never delivered, so we don't list it.
var shutdownSignals = []os.Signal{os.Interrupt}

// ---------------------------------------------------------------------------
// door mode: inherited Winsock handle
// ---------------------------------------------------------------------------

type winSock struct{ h windows.Handle }

func (s *winSock) Write(p []byte) (int, error) {
	total := 0
	for total < len(p) {
		n, err := sockSend(s.h, p[total:])
		// On a non-blocking socket a full send buffer reports WSAEWOULDBLOCK; that
		// is backpressure, not failure. Wait for the buffer to drain and retry so
		// a busy frame doesn't kill the session. WSAEINTR is a benign interruption.
		if err == windows.WSAEWOULDBLOCK || err == windows.WSAEINTR {
			time.Sleep(2 * time.Millisecond)
			continue
		}
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
		total += n
	}
	return total, nil
}

func (s *winSock) Read(p []byte) (int, error) {
	for {
		n, err := sockRecv(s.h, p)
		// A BBS may hand us a socket already in non-blocking mode (Synchronet
		// Win32, ELEBBS, ...). WSARecv then returns WSAEWOULDBLOCK with no data
		// rather than blocking; poll politely instead of treating it as EOF.
		// WSAEINTR is a benign interruption - just retry.
		if err == windows.WSAEWOULDBLOCK || err == windows.WSAEINTR {
			time.Sleep(6 * time.Millisecond)
			continue
		}
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, io.EOF
		}
		return n, nil
	}
}

func (s *winSock) ReadTimeout(p []byte, d time.Duration) (int, error) {
	// SO_RCVTIMEO covers a blocking socket; for an inherited non-blocking socket
	// it has no effect and WSARecv returns WSAEWOULDBLOCK immediately, so we also
	// poll against our own deadline. Either way we never report the absence of
	// data as a fatal error - the caller (ANSI size probe) wants 0,nil on timeout.
	ms := int(d / time.Millisecond)
	if ms <= 0 {
		ms = 1
	}
	windows.SetsockoptInt(s.h, windows.SOL_SOCKET, windows.SO_RCVTIMEO, ms)
	defer windows.SetsockoptInt(s.h, windows.SOL_SOCKET, windows.SO_RCVTIMEO, 0)
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		n, err := sockRecv(s.h, p)
		if n > 0 {
			return n, nil
		}
		if err == windows.WSAEWOULDBLOCK || err == windows.WSAEINTR {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if err == windows.WSAETIMEDOUT {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, io.EOF
		}
	}
	return 0, nil
}

func (s *winSock) Close() error { return windows.Closesocket(s.h) }

func sockRecv(h windows.Handle, p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	buf := windows.WSABuf{Len: uint32(len(p)), Buf: &p[0]}
	var recvd, flags uint32
	if err := windows.WSARecv(h, &buf, 1, &recvd, &flags, nil, nil); err != nil {
		return 0, err
	}
	return int(recvd), nil
}

func sockSend(h windows.Handle, p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	buf := windows.WSABuf{Len: uint32(len(p)), Buf: &p[0]}
	var sent uint32
	if err := windows.WSASend(h, &buf, 1, &sent, 0, nil, nil); err != nil {
		return 0, err
	}
	return int(sent), nil
}

// ---------------------------------------------------------------------------
// stdio mode: console or redirected pipe
// ---------------------------------------------------------------------------

type winStdio struct {
	in  *os.File
	out *os.File
}

func (t *winStdio) Write(p []byte) (int, error) { return t.out.Write(p) }
func (t *winStdio) Close() error                { return nil }

func (t *winStdio) Read(p []byte) (int, error) {
	n, err := t.in.Read(p)
	if err != nil {
		return n, err
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

// ReadTimeout is intentionally a no-op on the Windows stdio path: a local
// console reports its size via the console API (so the ANSI probe is never
// needed there), and a blind read on a redirected pipe can't be cancelled
// without leaking a pending read into the key reader. Callers fall back to the
// 80x25 default or the explicit "<cols> <rows>" args. The socket path, which is
// where a remote terminal actually answers the probe, uses winSock.ReadTimeout.
func (t *winStdio) ReadTimeout(p []byte, d time.Duration) (int, error) {
	return 0, nil
}

// ---------------------------------------------------------------------------
// openTerm / size / raw mode
// ---------------------------------------------------------------------------

func openTerm(dropfile string) (Term, func(), error) {
	if h, ok := readDoor32(dropfile); ok {
		// Make sure Winsock is initialised in this process before we touch the
		// inherited handle (the BBS started it in its own process).
		var data windows.WSAData
		if err := windows.WSAStartup(uint32(0x202), &data); err != nil {
			logf("WSAStartup: %v", err)
		}
		logf("dropfile %q: winsock handle=%d", dropfile, h)
		return &winSock{h: windows.Handle(uintptr(h))}, func() {}, nil
	}
	logf("no door socket in %q; using stdio", dropfile)
	restore := enableConsoleVT()
	return &winStdio{in: os.Stdin, out: os.Stdout}, restore, nil
}

// enableConsoleVT switches the console into VT (ANSI) output + input and turns
// off line buffering and echo so keys arrive raw, returning a restore func. On
// a redirected pipe (no console) GetConsoleMode fails and this is a no-op.
func enableConsoleVT() func() {
	inH := windows.Handle(os.Stdin.Fd())
	outH := windows.Handle(os.Stdout.Fd())

	var inMode, outMode uint32
	haveIn := windows.GetConsoleMode(inH, &inMode) == nil
	haveOut := windows.GetConsoleMode(outH, &outMode) == nil

	if haveOut {
		windows.SetConsoleMode(outH, outMode|windows.ENABLE_PROCESSED_OUTPUT|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	}
	if haveIn {
		newIn := inMode &^ (windows.ENABLE_LINE_INPUT | windows.ENABLE_ECHO_INPUT | windows.ENABLE_PROCESSED_INPUT)
		newIn |= windows.ENABLE_VIRTUAL_TERMINAL_INPUT
		windows.SetConsoleMode(inH, newIn)
	}
	return func() {
		if haveIn {
			windows.SetConsoleMode(inH, inMode)
		}
		if haveOut {
			windows.SetConsoleMode(outH, outMode)
		}
	}
}

// localTermSize reads the console window dimensions. Fails (-> ANSI probe / args
// / default) when stdout isn't a console, e.g. the door socket or a pipe.
func localTermSize() (cols, rows int, ok bool) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(os.Stdout.Fd()), &info); err != nil {
		return 0, 0, false
	}
	cols = int(info.Window.Right - info.Window.Left + 1)
	rows = int(info.Window.Bottom - info.Window.Top + 1)
	if cols <= 0 || rows <= 0 {
		return 0, 0, false
	}
	return cols, rows, true
}
