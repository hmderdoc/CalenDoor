// ANSI + CP437 rendering helpers. The door writes raw CP437 to a BBS terminal,
// so box-drawing uses the high-bit CP437 glyphs (rendered by SyncTERM et al.).
package main

import (
	"bufio"
	"fmt"
	"strings"
)

// wrapText word-wraps s (collapsing newlines into paragraph breaks) to width.
func wrapText(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			switch {
			case line == "":
				line = word
			case len(line)+1+len(word) <= width:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func cls(w *bufio.Writer)        { w.WriteString("\x1b[2J\x1b[H") }
func hideCursor(w *bufio.Writer) { w.WriteString("\x1b[?25l") }
func showCursor(w *bufio.Writer) { w.WriteString("\x1b[?25h") }
func resetAttr(w *bufio.Writer)  { w.WriteString("\x1b[0m") }

// at moves the cursor to a 1-based (row,col) and writes s.
func at(w *bufio.Writer, row, col int, s string) {
	fmt.Fprintf(w, "\x1b[%d;%dH%s", row, col, s)
}

// sgr wraps s in an SGR sequence and a reset.
func sgr(code, s string) string { return "\x1b[" + code + "m" + s + "\x1b[0m" }

// CP437 box-drawing (single line) + markers.
const (
	bxH    = "\xc4" // horizontal
	bxV    = "\xb3" // vertical
	bxTL   = "\xda" // top-left corner
	bxTR   = "\xbf" // top-right corner
	bxBL   = "\xc0" // bottom-left corner
	bxBR   = "\xd9" // bottom-right corner
	bxLT   = "\xc3" // left tee
	bxRT   = "\xb4" // right tee
	bxTT   = "\xc2" // top tee
	bxBT   = "\xc1" // bottom tee
	bxX    = "\xc5" // cross
	sqr    = "\xfe" // small filled square (event marker)
	mdot   = "\xf9" // middle dot
)

// truncate clips s to at most w columns (byte width; CP437 is single-byte).
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if len(s) <= w {
		return s
	}
	if w <= 1 {
		return s[:w]
	}
	return s[:w-1] + ">"
}

// padRight pads (or clips) s to exactly w columns.
func padRight(s string, w int) string {
	s = truncate(s, w)
	for len(s) < w {
		s += " "
	}
	return s
}

// center centers s within w columns.
func center(s string, w int) string {
	s = truncate(s, w)
	if len(s) >= w {
		return s
	}
	left := (w - len(s)) / 2
	out := ""
	for i := 0; i < left; i++ {
		out += " "
	}
	out += s
	for len(out) < w {
		out += " "
	}
	return out
}

func repeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
