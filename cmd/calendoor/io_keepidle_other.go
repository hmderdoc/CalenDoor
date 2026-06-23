//go:build !windows && !darwin

package main

import "golang.org/x/sys/unix"

// Linux and the BSDs spell the TCP keepalive idle-time socket option TCP_KEEPIDLE.
// macOS uses TCP_KEEPALIVE instead (see io_keepidle_darwin.go).
const tcpKeepIdleOpt = unix.TCP_KEEPIDLE
