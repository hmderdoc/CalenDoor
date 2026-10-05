// UTF-8 -> CP437 down-conversion. Calendar feeds are UTF-8 (umlauts, curly
// quotes, em-dashes, emoji, ...) but the door writes single-byte CP437 to a BBS
// terminal, so feed/config text is folded to CP437 once, where it enters the
// door: an exact CP437 glyph when one exists, otherwise the closest plain
// stand-in (a dash for an em-dash, an unaccented letter for one CP437 lacks).
// After this every byte is one screen column, which the layout code relies on.
package main

import (
	"strings"
	"unicode/utf8"
)

// cp437High is the Unicode meaning of CP437 bytes 0x80..0xFF, in order.
const cp437High = "ÇüéâäàåçêëèïîìÄÅ" +
	"ÉæÆôöòûùÿÖÜ¢£¥₧ƒ" +
	"áíóúñÑªº¿⌐¬½¼¡«»" +
	"░▒▓│┤╡╢╖╕╣║╗╝╜╛┐" +
	"└┴┬├─┼╞╟╚╔╩╦╠═╬╧" +
	"╨╤╥╙╘╒╓╫╪┘┌█▄▌▐▀" +
	"αßΓπΣσµτΦΘΩδ∞φε∩" +
	"≡±≥≤⌠⌡÷≈°∙·√ⁿ²■\u00a0"

// cp437Fallbacks maps characters CP437 lacks to a stand-in: every rune in the
// first string becomes the second (which may hold raw CP437 bytes).
var cp437Fallbacks = [][2]string{
	// punctuation
	{"‘’‚‛′`´ʼ", "'"},
	{"“”„‟″¨", `"`},
	{"‹", "<"}, {"›", ">"},
	{"‐‑‒–—―−¯", "-"},
	{"…", "..."},
	{"•‣◦●○▪", "\xf9"},
	{"⁄∕", "/"}, {"¦", "|"}, {"¸", ","}, {"ˆ", "^"}, {"˜∼", "~"},
	{"†‡", "+"}, {"×✗✘", "x"}, {"★☆✱", "*"}, {"✓✔", "\xfb"},
	// spaces that should stay a space (NBSP included: byte 0xFF is telnet IAC)
	{"\u00a0\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u202f\u205f\u3000", " "},
	// symbols
	{"©", "(c)"}, {"®", "(r)"}, {"™", "(tm)"}, {"€", "EUR"}, {"№", "No."},
	{"¹", "1"}, {"³", "3"}, {"¾", "3/4"}, {"§", "S"}, {"¶", "P"},
	{"←", "<-"}, {"→", "->"}, {"↑", "^"}, {"↓", "v"}, {"↔", "<->"}, {"⇒", "=>"},
	{"≠", "!="}, {"♥❤", "<3"}, {"℃", "\xf8C"}, {"℉", "\xf8F"},
	// look-alikes of glyphs CP437 does have
	{"μ", "\xe6"}, {"β", "\xe1"}, {"Ω", "\xea"}, {"∑", "\xe4"}, {"─━", "\xc4"}, {"│┃", "\xb3"},
	// accented Latin letters CP437 lacks
	{"ÀÁÂÃĀĂĄ", "A"}, {"ãāăą", "a"},
	{"ĆĈĊČ", "C"}, {"ćĉċč", "c"},
	{"ĎĐÐ", "D"}, {"ďđð", "d"},
	{"ÈÊËĒĔĖĘĚ", "E"}, {"ēĕėęě", "e"},
	{"ĜĞĠĢ", "G"}, {"ĝğġģ", "g"},
	{"ĤĦ", "H"}, {"ĥħ", "h"},
	{"ÌÍÎÏĨĪĬĮİ", "I"}, {"ĩīĭįı", "i"},
	{"Ĵ", "J"}, {"ĵ", "j"}, {"Ķ", "K"}, {"ķĸ", "k"},
	{"ĹĻĽĿŁ", "L"}, {"ĺļľŀł", "l"},
	{"ŃŅŇ", "N"}, {"ńņňŉ", "n"},
	{"ÒÓÔÕØŌŎŐ", "O"}, {"õøōŏő", "o"},
	{"ŔŖŘ", "R"}, {"ŕŗř", "r"},
	{"ŚŜŞŠȘ", "S"}, {"śŝşšșſ", "s"},
	{"ŢŤŦȚ", "T"}, {"ţťŧț", "t"},
	{"ÙÚÛŨŪŬŮŰŲ", "U"}, {"ũūŭůűų", "u"},
	{"Ŵ", "W"}, {"ŵ", "w"}, {"ÝŶŸ", "Y"}, {"ýŷ", "y"},
	{"ŹŻŽ", "Z"}, {"źżž", "z"},
	{"Þ", "Th"}, {"þ", "th"}, {"Œ", "OE"}, {"œ", "oe"}, {"Ĳ", "IJ"}, {"ĳ", "ij"}, {"ẞ", "SS"},
}

// cp437Compose lists, per combining mark, base/composed letter pairs, so
// decomposed text ("u" + U+0308) still lands on the precomposed glyph.
var cp437Compose = map[rune]string{
	0x0300: "AÀEÈIÌOÒUÙaàeèiìoòuù",
	0x0301: "AÁEÉIÍOÓUÚYÝaáeéiíoóuúyý",
	0x0302: "AÂEÊIÎOÔUÛaâeêiîoôuû",
	0x0303: "AÃNÑOÕaãnñoõ",
	0x0308: "AÄEËIÏOÖUÜYŸaäeëiïoöuüyÿ",
	0x030a: "AÅaå",
	0x0327: "CÇcç",
}

// cp1252High decodes bytes 0x80..0x9F of Windows-1252, the usual culprit when a
// feed isn't valid UTF-8 (0 = unassigned).
var cp1252High = [32]rune{
	'€', 0, '‚', 'ƒ', '„', '…', '†', '‡', 'ˆ', '‰', 'Š', '‹', 'Œ', 0, 'Ž', 0,
	0, '‘', '’', '“', '”', '•', '–', '—', '˜', '™', 'š', '›', 'œ', 0, 'ž', 'Ÿ',
}

var (
	cp437Map      = map[rune]string{}
	cp437Composed = map[[2]rune]rune{}
)

func init() {
	b := byte(0x80)
	for _, r := range cp437High {
		if b != 0xff { // never emit 0xFF (telnet IAC); NBSP falls back to a space
			cp437Map[r] = string([]byte{b})
		}
		b++
	}
	for _, fb := range cp437Fallbacks {
		for _, r := range fb[0] {
			if _, exact := cp437Map[r]; !exact {
				cp437Map[r] = fb[1]
			}
		}
	}
	for mark, pairs := range cp437Compose {
		rs := []rune(pairs)
		for i := 0; i+1 < len(rs); i += 2 {
			cp437Composed[[2]rune{rs[i], mark}] = rs[i+1]
		}
	}
}

// toCP437 folds UTF-8 text to CP437 bytes. Newlines survive; other control
// characters (including ESC, so a feed can't inject ANSI) are dropped. Emoji
// and invisible formatting characters vanish; anything else unmappable is "?".
func toCP437(s string) string {
	rs := make([]rune, 0, len(s))
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && n == 1 { // not UTF-8: read the byte as Windows-1252
			r = rune(s[i])
			if r >= 0x80 && r <= 0x9f {
				r = cp1252High[r-0x80]
			}
		}
		i += n
		if r >= 0x0300 && r <= 0x036f { // combining mark: compose with the base, else drop
			if l := len(rs); l > 0 {
				if c, ok := cp437Composed[[2]rune{rs[l-1], r}]; ok {
					rs[l-1] = c
				}
			}
			continue
		}
		rs = append(rs, r)
	}

	var sb strings.Builder
	sb.Grow(len(rs))
	for _, r := range rs {
		switch {
		case r == '\n':
			sb.WriteByte('\n')
		case r == '\t':
			sb.WriteByte(' ')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			// control character: drop
		case r < 0x7f:
			sb.WriteByte(byte(r))
		default:
			if m, ok := cp437Map[r]; ok {
				sb.WriteString(m)
			} else if !cp437Invisible(r) {
				sb.WriteByte('?')
			}
		}
	}
	return sb.String()
}

// toCP437Line is toCP437 for single-line fields: whitespace runs (including any
// left behind by a dropped emoji) collapse to one space and the ends are trimmed.
func toCP437Line(s string) string {
	return strings.Join(strings.FieldsFunc(toCP437(s), func(r rune) bool { return r == ' ' || r == '\n' }), " ")
}

// cp437Invisible reports runes that are dropped rather than shown as "?":
// zero-width/formatting characters, variation selectors, and emoji/pictographs.
func cp437Invisible(r rune) bool {
	switch {
	case r == 0x00ad, r == 0xfeff, // soft hyphen, BOM
		r >= 0x200b && r <= 0x200f, r >= 0x2028 && r <= 0x202e, r >= 0x2060 && r <= 0x206f,
		r >= 0xfe00 && r <= 0xfe0f,   // variation selectors
		r >= 0x2600 && r <= 0x27bf,   // misc symbols + dingbats
		r >= 0x2b00 && r <= 0x2bff,   // misc symbols and arrows (stars, etc.)
		r >= 0x1f000 && r <= 0x1faff, // emoji + pictographs
		r >= 0xe0000 && r <= 0xe0fff: // tags + variation selectors supplement
		return true
	}
	return false
}
