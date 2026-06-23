//go:build !windows

// Unix terminal I/O: raw mode via stty, terminal size via the TIOCGWINSZ ioctl,
// and an inherited door socket addressed by its file descriptor. On unix the
// DOOR32.SYS comm handle IS a file descriptor, so os.NewFile wraps it directly.
package main

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// enableKeepAlive turns on TCP keepalive on the inherited telnet socket so the
// kernel detects a client that vanished without a clean close (a half-open
// connection) within ~2 minutes. Without this, a blocking Read on a dead peer
// never returns EOF, the door never exits, and Synchronet never frees the node.
func enableKeepAlive(fd int) {
	if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_KEEPALIVE, 1); err != nil {
		logf("keepalive: SO_KEEPALIVE failed on fd=%d: %v", fd, err)
		return
	}
	// Probe after 60s of silence, again every 15s, drop after 4 misses (~2 min).
	// (tcpKeepIdleOpt is TCP_KEEPIDLE on Linux/BSD, TCP_KEEPALIVE on macOS.)
	unix.SetsockoptInt(fd, unix.IPPROTO_TCP, tcpKeepIdleOpt, 60)
	unix.SetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_KEEPINTVL, 15)
	unix.SetsockoptInt(fd, unix.IPPROTO_TCP, unix.TCP_KEEPCNT, 4)
	logf("keepalive enabled on door socket fd=%d (idle 60s, intvl 15s, cnt 4)", fd)
}

// shutdownSignals are the signals that should trigger a clean exit.
var shutdownSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}

// unixTerm is a raw tty (stdio) or an inherited telnet socket; both are *os.File
// here because a unix socket handle is just a file descriptor.
type unixTerm struct {
	in     *os.File
	out    *os.File
	socket bool
}

func (t *unixTerm) Write(p []byte) (int, error) { return t.out.Write(p) }

func (t *unixTerm) Close() error {
	if t.socket {
		return t.in.Close()
	}
	return nil
}

func (t *unixTerm) Read(p []byte) (int, error) {
	fd := int(t.in.Fd())
	for {
		n, err := syscall.Read(fd, p)
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			// An already-non-blocking inherited socket: poll politely.
			time.Sleep(6 * time.Millisecond)
			continue
		}
		if err != nil {
			return n, err
		}
		if n == 0 {
			return 0, io.EOF
		}
		return n, nil
	}
}

// ReadTimeout flips the fd non-blocking, polls until data or the deadline, then
// restores blocking mode. *os.File read deadlines aren't reliable on an
// inherited socket, so we drive the fd at the syscall level.
func (t *unixTerm) ReadTimeout(p []byte, d time.Duration) (int, error) {
	fd := int(t.in.Fd())
	syscall.SetNonblock(fd, true)
	defer syscall.SetNonblock(fd, false)
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		n, err := syscall.Read(fd, p)
		if n > 0 {
			return n, nil
		}
		if err == syscall.EAGAIN || err == syscall.EWOULDBLOCK {
			time.Sleep(5 * time.Millisecond)
			continue
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

// openTerm picks the inherited door socket (dropfile says type 2) or stdio, and
// puts the controlling tty into raw mode. The restore func undoes raw mode.
func openTerm(dropfile string) (Term, func(), error) {
	in, out := os.Stdin, os.Stdout
	socket := false
	if h, ok := readDoor32(dropfile); ok {
		if sf := os.NewFile(uintptr(h), "door-socket"); sf != nil {
			in, out = sf, sf
			socket = true
			enableKeepAlive(int(h)) // detect half-open client drops so the node frees up
			logf("dropfile %q: telnet socket fd=%d", dropfile, h)
		}
	} else {
		logf("no door socket in %q; using stdio", dropfile)
	}
	restore := sttyRaw()
	return &unixTerm{in: in, out: out, socket: socket}, restore, nil
}

// localTermSize returns the terminal dimensions from the kernel via ioctl. This
// only works for a real local tty; through an inherited socket it fails and the
// caller falls back to the ANSI probe.
func localTermSize() (cols, rows int, ok bool) {
	type winsize struct{ Row, Col, X, Y uint16 }
	var ws winsize
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(),
		uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&ws)))
	if e != 0 || ws.Col == 0 || ws.Row == 0 {
		return 0, 0, false
	}
	return int(ws.Col), int(ws.Row), true
}

// sttyRaw puts the tty into character-at-a-time mode and returns a restore func.
func sttyRaw() func() {
	saved, err := exec.Command("stty", "-F", "/dev/tty", "-g").Output()
	if err != nil {
		// fall back to stdin as the controlling tty
		saved, err = sttyArgs(os.Stdin, "-g")
		if err != nil {
			return func() {}
		}
	}
	// VMIN=1: block until at least one byte. With VMIN=0 an empty read returns
	// (0,nil), which is indistinguishable from EOF - that would quit instantly.
	sttyArgs(os.Stdin, "-icanon", "-echo", "-isig", "min", "1", "time", "0")
	prev := strings.TrimSpace(string(saved))
	return func() { sttyArgs(os.Stdin, prev) }
}

func sttyArgs(tty *os.File, args ...string) ([]byte, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = tty
	return cmd.Output()
}
