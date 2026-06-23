// QR rendering for the phone-subscribe screen. We draw the module matrix with
// CP437 upper-half-blocks (0xDF): two modules stacked per character cell, which
// both halves the row count AND makes each module roughly square (a terminal cell
// is ~twice as tall as wide), so the code scans cleanly. Dark modules are black,
// light modules white.
package main

import (
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// qrHalfBlock returns the rendered lines and the matrix width (in columns,
// including the quiet zone). ok=false if encoding failed.
func qrHalfBlock(content string, quiet int) (lines []string, width int, ok bool) {
	q, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		return nil, 0, false
	}
	q.DisableBorder = true // we add our own (tighter) quiet zone to control size
	bm := q.Bitmap()
	n := len(bm)
	if n == 0 {
		return nil, 0, false
	}
	size := n + 2*quiet
	grid := make([][]bool, size)
	for i := range grid {
		grid[i] = make([]bool, size)
	}
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			grid[y+quiet][x+quiet] = bm[y][x]
		}
	}
	for y := 0; y < size; y += 2 {
		var b strings.Builder
		last := ""
		for x := 0; x < size; x++ {
			top := grid[y][x]
			bot := false
			if y+1 < size {
				bot = grid[y+1][x]
			}
			fg, bg := "97", "107" // light = white
			if top {
				fg = "30" // dark top half = black
			}
			if bot {
				bg = "40" // dark bottom half = black
			}
			sgr := fg + ";" + bg
			if sgr != last { // only re-emit SGR when the cell color changes
				b.WriteString("\x1b[" + sgr + "m")
				last = sgr
			}
			b.WriteByte(0xDF)
		}
		b.WriteString("\x1b[0m")
		lines = append(lines, b.String())
	}
	return lines, size, true
}
