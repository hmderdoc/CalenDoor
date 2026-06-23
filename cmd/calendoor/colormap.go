// Tone-preserving recolor for the multi-color TheDraw clock font. Masking every
// cell to one color flattens the shading into an unreadable blob; instead we map
// each DISTINCT color the font uses to a distinct shade of the target hue,
// ordered by brightness - so the segment depth survives, just in a new color.
package main

import (
	"sort"
	"strconv"
	"strings"
)

// clockRamps are dark->light shade ramps per color name (ANSI fg codes).
var clockRamps = map[string][]string{
	"cyan":     {"30", "34", "36", "96", "97"},
	"red":      {"30", "31", "91", "93", "97"},
	"lightred": {"30", "31", "91", "93", "97"},
	"green":    {"30", "32", "92", "93", "97"},
	"amber":    {"30", "33", "93", "93", "97"},
	"yellow":   {"30", "33", "93", "93", "97"},
	"orange":   {"30", "31", "33", "93", "97"},
	"blue":     {"30", "34", "94", "96", "97"},
	"magenta":  {"30", "35", "95", "93", "97"},
	"pink":     {"30", "35", "95", "93", "97"},
	"white":    {"30", "37", "37", "97", "97"},
}

// lumBase is perceived brightness per ANSI base color (black,red,green,yellow,
// blue,magenta,cyan,white); bright variants add 8.
var lumBase = [8]int{0, 2, 4, 6, 1, 3, 5, 7}

func lumOf(code int) int {
	switch {
	case code >= 30 && code <= 37:
		return lumBase[code-30]
	case code >= 90 && code <= 97:
		return lumBase[code-90] + 8
	}
	return 0
}

// classifySGR returns the fg-equivalent code of an SGR param (bg codes are
// normalized to their fg by subtracting 10), whether it was a background, and ok.
func classifySGR(p string) (fgEquiv int, isBg, ok bool) {
	n, err := strconv.Atoi(p)
	if err != nil {
		return 0, false, false
	}
	switch {
	case n >= 30 && n <= 37, n >= 90 && n <= 97:
		return n, false, true
	case n >= 40 && n <= 47, n >= 100 && n <= 107:
		return n - 10, true, true
	}
	return 0, false, false
}

// buildClockMap inspects one or more rendered samples (e.g. all ten digits), and
// returns a code->shade map: the font's distinct colors, ordered by brightness,
// mapped onto the named hue's ramp. Built once and reused across the HH/MM/SS
// groups so they share consistent shading. nil for an unknown hue.
func buildClockMap(name string, samples ...string) map[int]string {
	ramp := clockRamps[name]
	if len(ramp) == 0 {
		return nil
	}
	distinct := map[int]bool{}
	for _, s := range samples {
		forEachSGR(s, func(params []string) {
			for _, p := range params {
				if fe, _, ok := classifySGR(p); ok {
					distinct[fe] = true
				}
			}
		})
	}
	if len(distinct) == 0 {
		return nil
	}
	codes := make([]int, 0, len(distinct))
	for c := range distinct {
		codes = append(codes, c)
	}
	sort.Slice(codes, func(i, j int) bool { return lumOf(codes[i]) < lumOf(codes[j]) })
	tgt := make(map[int]string, len(codes))
	for i, c := range codes {
		idx := len(ramp) - 1
		if len(codes) > 1 {
			idx = i * (len(ramp) - 1) / (len(codes) - 1)
		}
		tgt[c] = ramp[idx]
	}
	return tgt
}

// applyClockMap rewrites a rendered string's colors through a code->shade map
// (from buildClockMap). nil map -> unchanged (keep the font's native colors).
func applyClockMap(s string, tgt map[int]string) string {
	if tgt == nil {
		return s
	}
	return rewriteSGR(s, func(p string) string {
		fe, isBg, ok := classifySGR(p)
		if !ok {
			return p
		}
		rf := tgt[fe]
		if rf == "" {
			return p
		}
		if isBg {
			n, _ := strconv.Atoi(rf)
			return strconv.Itoa(n + 10)
		}
		return rf
	})
}

// forEachSGR calls fn with the parameter list of each "\x1b[...m" sequence.
func forEachSGR(s string, fn func(params []string)) {
	for i := 0; i+1 < len(s); i++ {
		if s[i] != 0x1b || s[i+1] != '[' {
			continue
		}
		j := i + 2
		for j < len(s) && s[j] != 'm' && !(s[j] >= '@' && s[j] <= '~') {
			j++
		}
		if j < len(s) && s[j] == 'm' {
			fn(strings.Split(s[i+2:j], ";"))
		}
		i = j
	}
}

// rewriteSGR rewrites every "\x1b[...m" sequence, mapping each parameter.
func rewriteSGR(s string, mapParam func(string) string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if i+1 < len(s) && s[i] == 0x1b && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' && !(s[j] >= '@' && s[j] <= '~') {
				j++
			}
			if j < len(s) && s[j] == 'm' {
				parts := strings.Split(s[i+2:j], ";")
				for k, p := range parts {
					parts[k] = mapParam(p)
				}
				b.WriteString("\x1b[")
				b.WriteString(strings.Join(parts, ";"))
				b.WriteByte('m')
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
