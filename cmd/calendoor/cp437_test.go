package main

import "testing"

func TestToCP437(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain ASCII", "plain ASCII"},
		{"Grüße aus Köln, Äpfel & Öl", "Gr\x81\xe1e aus K\x94ln, \x8epfel & \x99l"},
		{"u\u0308ber cafe\u0301", "\x81ber caf\x82"},         // decomposed umlaut/acute
		{"“quoted” ‘single’ it’s", `"quoted" 'single' it's`}, // curly quotes
		{"9–5 — done…", "9-5 - done..."},                     // en/em dash, ellipsis
		{"São Paulo, Łódź, Dvořák", "Sao Paulo, L\xa2dz, Dvor\xa0k"},
		{"a\u00a0b\u200bc", "a bc"}, // NBSP -> space, zero-width dropped
		{"50° ½ ±2 ≥ 1", "50\xf8 \xab \xf12 \xf2 1"},
		{"© 2026 Foo™ 5€", "(c) 2026 Foo(tm) 5EUR"},
		{"line1\nline2\ttab\x1b[31m", "line1\nline2 tab[31m"}, // ESC stripped
		{"Привет", "??????"},                                  // no CP437 equivalent
		{"caf\xe9 \x93hi\x94", "caf\x82 \"hi\""},              // Windows-1252 bytes, not UTF-8
	}
	for _, c := range cases {
		if got := toCP437(c.in); got != c.want {
			t.Errorf("toCP437(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToCP437Line(t *testing.T) {
	if got := toCP437Line("🎉 Party  🎂 time \n"); got != "Party time" {
		t.Errorf("got %q", got)
	}
}

// No input may yield 0xFF (telnet IAC) or a C0 control other than newline.
func TestToCP437SafeBytes(t *testing.T) {
	for r := rune(0); r < 0x30000; r++ {
		for _, b := range []byte(toCP437(string(r))) {
			if b == 0xff || (b < 0x20 && b != '\n') || b == 0x7f {
				t.Fatalf("rune U+%04X produced unsafe byte 0x%02x", r, b)
			}
		}
	}
}
