// Theming: all calendar colors are data, configurable in the ini's [colors]
// section, with sane defaults. A per-month accent (one color per month) drives
// the month/week title + grid border so each month has its own feel.
package main

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// theme holds ANSI SGR foreground codes per role (today/focus are full styles).
type theme struct {
	weekday  string    // weekday header labels
	day      string    // in-month day numbers
	dim      string    // other-month day numbers
	today    string    // today's day-number badge (full SGR, incl. bg)
	focus    string    // the focused/selected day number (full SGR)
	title    string    // base month/year header color (fallback)
	border   string    // base grid color (fallback)
	hintKey  string     // footer: the key
	hintText string     // footer: what it does
	clock    string     // splash 7-segment clock color (ANSI fg, bright)
	months   [12]string // per-month accent (fg code) for title + border; "" = use base
}

// clockANSI resolves a clock color name to a bright ANSI foreground code.
func clockANSI(name, def string) string {
	m := map[string]string{
		"cyan": "96", "red": "91", "lightred": "91", "green": "92", "lightgreen": "92",
		"yellow": "93", "amber": "93", "orange": "93", "blue": "94", "magenta": "95",
		"pink": "95", "white": "97",
	}
	if c, ok := m[strings.ToLower(strings.TrimSpace(name))]; ok {
		return c
	}
	return def
}

func defaultTheme() theme {
	t := theme{
		weekday:  "96",      // cyan
		day:      "37",      // white
		dim:      "90",      // gray (leading/trailing days)
		today:    "1;30;43", // black on yellow
		focus:    "1;33;4",  // bright yellow, underlined
		title:    "93",      // yellow (fallback)
		border:   "94",      // blue grid (fallback)
		hintKey:  "93",      // bright yellow keys
		hintText: "37",      // white descriptions
		clock:    "cyan",    // splash clock hue (ramp name)
	}
	// A per-month accent rotation (loosely seasonal); overrides title+border.
	t.months = [12]string{
		"96", "95", "92", "93", "92", "96",
		"91", "93", "33", "91", "33", "94",
	}
	return t
}

// themePalette maps friendly color names to SGR foreground codes.
var themePalette = map[string]string{
	"black": "30", "red": "31", "green": "32", "yellow": "33",
	"blue": "34", "magenta": "35", "cyan": "36", "white": "37",
	"gray": "90", "grey": "90",
	"brightred": "91", "brightgreen": "92", "brightyellow": "93", "brightblue": "94",
	"brightmagenta": "95", "brightcyan": "96", "brightwhite": "97",
	// aliases (nearest 16-color)
	"orange": "33", "amber": "33", "pink": "95", "purple": "35", "teal": "36", "lime": "92",
}

// themeColor resolves a color name to an SGR fg code, falling back to def.
func themeColor(name, def string) string {
	if c, ok := themePalette[strings.ToLower(strings.TrimSpace(name))]; ok {
		return c
	}
	return def
}

// bgOf turns a foreground SGR code into the matching background code.
func bgOf(fg string) string {
	if n, err := strconv.Atoi(fg); err == nil {
		return strconv.Itoa(n + 10) // 3x->4x, 9x->10x
	}
	return "43"
}

// titleSGR / borderSGR apply the month's accent (or the base) for that month.
func (t theme) titleSGR(m time.Month) string {
	if a := t.months[int(m)-1]; a != "" {
		return "1;" + a
	}
	return "1;" + t.title
}

func (t theme) borderSGR(m time.Month) string {
	if a := t.months[int(m)-1]; a != "" {
		return a
	}
	return t.border
}

// hint is one footer item: a key and what it does.
type hint struct{ key, desc string }

// drawHints paints the footer on a black bar, keys and descriptions in distinct
// (themed) colors, centered.
func drawHints(w *bufio.Writer, cols, rows int, t theme, hints []hint) {
	at(w, rows, 1, "\x1b[40m"+repeat(" ", cols-1)+"\x1b[0m") // black bar (skip the last cell)
	vis := 0
	for i, h := range hints {
		vis += len(h.key)
		if h.desc != "" {
			vis += 1 + len(h.desc)
		}
		if i < len(hints)-1 {
			vis += 3 // " | " separator
		}
	}
	col := (cols-vis)/2 + 1
	if col < 1 {
		col = 1
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[%d;%dH", rows, col)
	for i, h := range hints {
		b.WriteString("\x1b[" + t.hintKey + ";40m" + h.key)
		if h.desc != "" {
			b.WriteString("\x1b[" + t.hintText + ";40m " + h.desc)
		}
		if i < len(hints)-1 {
			b.WriteString("\x1b[90;40m \xb3 ")
		}
	}
	b.WriteString("\x1b[0m")
	w.WriteString(b.String())
}
