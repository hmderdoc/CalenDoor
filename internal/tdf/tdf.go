// Package tdf renders TheDraw (.TDF) fonts to ANSI for big in-game lettering
// (titles, "FRAGGED", countdowns, scoreboards). It's a Go port of Synchronet's
// tdfonts_lib.js / tdfiglet, reading the same .TDF binaries. A few fonts are
// embedded so the door stays self-contained across platforms.
package tdf

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
)

//go:embed fonts/*.tdf
var fontFS embed.FS

const (
	numChars = 94
	charlist = "!\"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\\]^_`abcdefghijklmnopqrstuvwxyz{|}~"

	outlineFont = 0
	blockFont   = 1
	colorFont   = 2

	lightGray = 7
)

// magic + the 4-byte separator that precedes each font block in a file.
var (
	magic   = []byte("\x13TheDraw FONTS file\x1a")
	fontSep = []byte{0x55, 0xaa, 0x00, 0xff}
)

// outlineMap converts an outline font's letter codes to CP437 box-drawing bytes
// (from the TheDraw spec). Unmapped letters render as a space.
var outlineMap = map[byte]byte{
	'A': 205, 'B': 196, 'C': 179, 'D': 186, 'E': 213, 'F': 187, 'G': 214,
	'H': 191, 'I': 200, 'J': 190, 'K': 192, 'L': 189, 'M': 181, 'N': 199,
	'O': 32, '@': 32, '&': 38,
}

type cell struct {
	ch  byte
	col byte // CGA attribute: low nibble fg, high nibble bg
}

type glyph struct {
	width int
	cells []cell // width * Font.Height, row-major, space-padded
}

// Font is one parsed TheDraw font.
type Font struct {
	Name    string
	Type    int
	Spacing int
	Height  int
	glyphs  [numChars]*glyph
}

// RenderOpts tunes Render. The zero value keeps the font's own colors.
type RenderOpts struct {
	// Recolor replaces every cell's foreground with FG (CGA 0..15) -- use it to
	// theme block/outline fonts. Leave false to keep the font's colors.
	Recolor bool
	FG      int
	// SpaceWidth is the gap (cols) inserted for a ' '. 0 picks a sensible default.
	SpaceWidth int
	// Transparent (RenderCentered only) draws only ink cells, leaving the cells
	// behind the letters' empty space untouched so the scene shows through.
	Transparent bool
}

// Fit returns the largest of the named embedded fonts whose rendering of s fits
// within maxCols (ok=true). If none fit, it returns the smallest available with
// ok=false (caller should fall back to plain text). Pass names large->small or
// any order; size is measured, not assumed.
func Fit(s string, maxCols int, names ...string) (f *Font, ok bool) {
	var smallest *Font
	smallW := 1 << 30
	bestW := -1
	for _, n := range names {
		cand, err := Embedded(n, 0)
		if err != nil {
			continue
		}
		w, _ := cand.Measure(s)
		if w < smallW {
			smallW, smallest = w, cand
		}
		if w <= maxCols && w > bestW {
			bestW, f = w, cand
		}
	}
	if f != nil {
		return f, true
	}
	return smallest, false
}

// Embedded returns one of the fonts compiled into the binary, by base name
// (e.g. "block", "union"). index selects a font within a multi-font file.
func Embedded(name string, index int) (*Font, error) {
	data, err := fontFS.ReadFile("fonts/" + name + ".tdf")
	if err != nil {
		return nil, err
	}
	return Parse(data, index)
}

// List returns the base names of every embedded font file.
func List() []string {
	entries, err := fontFS.ReadDir("fonts")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, ".tdf") {
			names = append(names, strings.TrimSuffix(n, ".tdf"))
		}
	}
	return names
}

// EmbeddedAll decodes every font in an embedded .tdf file (by base name).
func EmbeddedAll(name string) []*Font {
	data, err := fontFS.ReadFile("fonts/" + name + ".tdf")
	if err != nil {
		return nil
	}
	return ParseAll(data)
}

// ParseAll decodes every font in a (possibly multi-font) .TDF file.
func ParseAll(data []byte) []*Font {
	var fonts []*Font
	for i := 0; ; i++ {
		f, err := Parse(data, i)
		if err != nil {
			break
		}
		fonts = append(fonts, f)
	}
	return fonts
}

// Measure returns the rendered width and height of s in this font (default opts),
// without building the strings — for catalog/fit decisions.
func (f *Font) Measure(s string) (width, height int) {
	items := f.layout(s)
	return f.blockWidth(items, f.Height/2+1), f.Height
}

// Parse decodes the font at the given index from raw .TDF bytes.
func Parse(data []byte, index int) (*Font, error) {
	if len(data) < len(magic) || !bytes.Equal(data[:len(magic)], magic) {
		return nil, fmt.Errorf("tdf: bad magic")
	}
	// Font blocks start at each separator (the first one is just past the magic).
	var starts []int
	for i := len(magic); i+4 <= len(data); i++ {
		if bytes.Equal(data[i:i+4], fontSep) {
			starts = append(starts, i)
		}
	}
	if index < 0 || index >= len(starts) {
		return nil, fmt.Errorf("tdf: font index %d of %d", index, len(starts))
	}
	s := starts[index]
	// Header, relative to the separator at s:
	//   +4 namelen, +5 name(16), +21 type, +22 spacing, +23..24 blocksize,
	//   +25.. 94 uint16 glyph offsets, +213 glyph data.
	if s+213 > len(data) {
		return nil, fmt.Errorf("tdf: truncated header")
	}
	nameLen := int(data[s+4])
	if nameLen > 16 {
		nameLen = 16
	}
	f := &Font{
		Name:    strings.TrimRight(string(data[s+5:s+5+nameLen]), "\x00 "),
		Type:    int(data[s+21]),
		Spacing: int(data[s+22]),
	}
	var off [numChars]int
	for i := 0; i < numChars; i++ {
		o := s + 25 + i*2
		off[i] = int(data[o]) | int(data[o+1])<<8
	}
	base := s + 213
	// First pass: overall height = tallest glyph (the cell grid uses it).
	for i := 0; i < numChars; i++ {
		if off[i] == 0xffff {
			continue
		}
		if p := base + off[i]; p+1 < len(data) {
			if h := int(data[p+1]); h > f.Height {
				f.Height = h
			}
		}
	}
	if f.Height == 0 {
		f.Height = 1
	}
	for i := 0; i < numChars; i++ {
		if off[i] == 0xffff {
			continue
		}
		f.glyphs[i] = parseGlyph(data, base+off[i], f.Type, f.Height)
	}
	return f, nil
}

func parseGlyph(data []byte, p, ftype, height int) *glyph {
	if p+2 > len(data) {
		return nil
	}
	width := int(data[p])
	p += 2 // skip width, glyph-height (we lay out on font height)
	g := &glyph{width: width, cells: make([]cell, width*height)}
	for i := range g.cells {
		g.cells[i] = cell{ch: ' ', col: lightGray}
	}
	row, col := 0, 0
	for p < len(data) && data[p] != 0x00 {
		ch := data[p]
		p++
		if ch == 0x0d { // end of row
			row++
			col = 0
			continue
		}
		color := byte(lightGray)
		if ftype == colorFont {
			if p >= len(data) {
				break
			}
			color = data[p]
			p++
		}
		if ch < 0x20 {
			ch = ' '
		}
		if ftype == outlineFont {
			if m, ok := outlineMap[ch]; ok {
				ch = m
			} else {
				ch = ' '
			}
		}
		if idx := row*width + col; idx >= 0 && idx < len(g.cells) {
			g.cells[idx] = cell{ch: ch, col: color}
			col++
		}
	}
	return g
}

func (f *Font) lookup(c byte) *glyph {
	idx := strings.IndexByte(charlist, c)
	if idx >= 0 && f.glyphs[idx] != nil {
		return f.glyphs[idx]
	}
	// fall back to uppercase (many block fonts are upper-only)
	if c >= 'a' && c <= 'z' {
		return f.lookup(c - 32)
	}
	return nil
}

// cgaSGR maps a CGA attribute byte to an ANSI SGR parameter string (fg;bg).
func cgaSGR(attr byte, recolor bool, fg int) string {
	f := int(attr & 0x0f)
	if recolor {
		f = fg & 0x0f
	}
	bg := int((attr >> 4) & 0x07)
	fgCode := []int{30, 34, 32, 36, 31, 35, 33, 37, 90, 94, 92, 96, 91, 95, 93, 97}[f]
	bgCode := []int{40, 44, 42, 46, 41, 45, 43, 47}[bg]
	return fmt.Sprintf("%d;%d", fgCode, bgCode)
}

type item struct {
	g  *glyph
	sp bool
}

func (f *Font) layout(s string) []item {
	var items []item
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			items = append(items, item{sp: true})
			continue
		}
		if g := f.lookup(s[i]); g != nil {
			items = append(items, item{g: g})
		}
	}
	return items
}

func (f *Font) blockWidth(items []item, spaceW int) int {
	w := 0
	for k, it := range items {
		if it.sp {
			w += spaceW
			continue
		}
		w += it.g.width
		if k < len(items)-1 {
			w += f.Spacing
		}
	}
	return w
}

// Render lays out s and returns one ANSI string per row (CP437 bytes + SGR; no
// cursor positioning), plus the block's pixel width and height.
func (f *Font) Render(s string, opts RenderOpts) (lines []string, width, height int) {
	spaceW := opts.SpaceWidth
	if spaceW == 0 {
		spaceW = f.Height/2 + 1
	}
	items := f.layout(s)
	height = f.Height
	width = f.blockWidth(items, spaceW)
	out := make([]string, height)
	for r := 0; r < height; r++ {
		var b strings.Builder
		last := ""
		for k, it := range items {
			if it.sp {
				b.WriteString("\x1b[0m")
				last = ""
				b.WriteString(strings.Repeat(" ", spaceW))
				continue
			}
			g := it.g
			for x := 0; x < g.width; x++ {
				c := g.cells[r*g.width+x]
				sgr := cgaSGR(c.col, opts.Recolor, opts.FG)
				if sgr != last {
					b.WriteString("\x1b[")
					b.WriteString(sgr)
					b.WriteByte('m')
					last = sgr
				}
				b.WriteByte(c.ch)
			}
			if k < len(items)-1 { // inter-letter spacing
				b.WriteString("\x1b[0m")
				last = ""
				b.WriteString(strings.Repeat(" ", f.Spacing))
			}
		}
		b.WriteString("\x1b[0m")
		out[r] = b.String()
	}
	return out, width, height
}

// RenderCentered returns a ready-to-write ANSI block: the rendered text
// horizontally centered in cols, positioned starting at 1-based topRow. With
// opts.Transparent, only ink cells are emitted (the scene shows through gaps);
// otherwise full rows (opaque) are written.
func (f *Font) RenderCentered(s string, cols, topRow int, opts RenderOpts) string {
	items := f.layout(s)
	spaceW := opts.SpaceWidth
	if spaceW == 0 {
		spaceW = f.Height/2 + 1
	}
	pad := (cols - f.blockWidth(items, spaceW)) / 2
	if pad < 0 {
		pad = 0
	}
	var b strings.Builder
	if !opts.Transparent {
		lines, _, _ := f.Render(s, opts)
		for i, ln := range lines {
			fmt.Fprintf(&b, "\x1b[%d;%dH", topRow+i, pad+1)
			b.WriteString(ln)
		}
		return b.String()
	}
	// Transparent: position each ink (non-space) cell absolutely; skip gaps.
	for r := 0; r < f.Height; r++ {
		col := pad
		lastSGR := ""
		lastRow, lastCol := -1, -2
		for k, it := range items {
			if it.sp {
				col += spaceW
				continue
			}
			g := it.g
			for x := 0; x < g.width; x++ {
				c := g.cells[r*g.width+x]
				if c.ch == ' ' {
					continue
				}
				scr, sc := topRow+r, col+x+1
				if scr != lastRow || sc != lastCol {
					fmt.Fprintf(&b, "\x1b[%d;%dH", scr, sc)
				}
				if sgr := cgaSGR(c.col, opts.Recolor, opts.FG); sgr != lastSGR {
					b.WriteString("\x1b[")
					b.WriteString(sgr)
					b.WriteByte('m')
					lastSGR = sgr
				}
				b.WriteByte(c.ch)
				lastRow, lastCol = scr, sc+1
			}
			col += g.width
			if k < len(items)-1 {
				col += f.Spacing
			}
		}
	}
	b.WriteString("\x1b[0m")
	return b.String()
}
