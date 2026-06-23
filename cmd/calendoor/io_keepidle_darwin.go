//go:build darwin

package main

import "golang.org/x/sys/unix"

// macOS spells the TCP keepalive idle-time socket option TCP_KEEPALIVE; it has no
// TCP_KEEPIDLE (which is the Linux/BSD name). See enableKeepAlive in io_unix.go.
const tcpKeepIdleOpt = unix.TCP_KEEPALIVE
