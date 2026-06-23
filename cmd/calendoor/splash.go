// Startup splash: a live digital clock. The HH / MM / SS digit groups are
// rendered with a TheDraw LCD-style font from the embedded library; the colons
// are drawn by hand (two blocks) and slotted between the groups. The font's
// multi-tone colors are remapped (not masked) to the configured hue so the
// segment shading survives. Ticks once a second; any key enters the calendar.
package main

import (
	"bufio"
	"strings"
	"time"

	"github.com/hmderdoc/CalenDoor/internal/tdf"
)

// repoURL is shown (quietly) on the splash so callers know where to find a copy
// or updates of the door.
const repoURL = "github.com/hmderdoc/CalenDoor"

// clockFont returns the configured clock font (default "computrx"), falling back
// through a few clean digital fonts; nil if none parse.
func clockFont(cfg config) *tdf.Font {
	for _, n := range []string{cfg.ClockFont, "computrx", "digitx", "digital2"} {
		if n == "" {
			continue
		}
		if fonts := tdf.EmbeddedAll(n); len(fonts) > 0 {
			if w, _ := fonts[0].Measure("0"); w > 0 { // must actually render digits
				return fonts[0]
			}
		}
	}
	return nil
}

// clockFace renders HH:MM:SS as font digit-groups with hand-drawn colons between
// them, recolored through cmap. Returns the lines and the total visible width.
func clockFace(f *tdf.Font, t time.Time, cmap map[int]string, colonColor string) (lines []string, width int) {
	hh, wHH, h := f.Render(t.Format("15"), tdf.RenderOpts{})
	mm, wMM, _ := f.Render(t.Format("04"), tdf.RenderOpts{})
	ss, wSS, _ := f.Render(t.Format("05"), tdf.RenderOpts{})
	for i := range hh {
		hh[i] = applyClockMap(hh[i], cmap)
	}
	for i := range mm {
		mm[i] = applyClockMap(mm[i], cmap)
	}
	for i := range ss {
		ss[i] = applyClockMap(ss[i], cmap)
	}
	// a colon: two stacked 2-wide blocks at ~1/3 and ~2/3 of the height
	d1, d2 := h/3, 2*h/3
	colon := make([]string, h)
	for i := range colon {
		if i == d1 || i == d2 {
			colon[i] = "\x1b[" + colonColor + "m\xdb\xdb\x1b[0m"
		} else {
			colon[i] = "  "
		}
	}
	lines = make([]string, h)
	for i := 0; i < h; i++ {
		lines[i] = hh[i] + " " + colon[i] + " " + mm[i] + " " + colon[i] + " " + ss[i]
	}
	width = wHH + 1 + 2 + 1 + wMM + 1 + 2 + 1 + wSS
	return lines, width
}

func runSplash(w *bufio.Writer, in *input, cols, rows int, cfg config) {
	f := clockFont(cfg)
	name := cfg.Theme.clock
	loc := cfg.Loc

	var cmap map[int]string
	h := 1
	if f != nil {
		h = f.Height
		full, _, _ := f.Render("0123456789", tdf.RenderOpts{}) // sample the full palette
		cmap = buildClockMap(name, strings.Join(full, "\n"))
	}
	top := (rows-h)/2 + 1 // clock centered, with room for the date above it
	if top < 3 {
		top = 3
	}
	dateRow := top - 2 // date sits above the clock
	colonColor := clockANSI(name, "96")

	cls(w)
	at(w, top+h+1, 1, sgr("1;35", center(cfg.Title, cols))) // title beneath the clock, magenta
	at(w, rows-2, 1, sgr("0;37", center("press any key to enter the calendar", cols))) // regular gray
	at(w, rows-1, 1, sgr("0;90", center(repoURL+"  "+version, cols))) // repo link + version: quiet dark-gray footer

	draw := func() {
		now := time.Now().In(loc)
		if f != nil {
			lines, width := clockFace(f, now, cmap, colonColor)
			left := (cols-width)/2 + 1
			if left < 1 {
				left = 1
			}
			for i := 0; i < h; i++ { // clear the band so a width change rolls cleanly
				at(w, top+i, 1, repeat(" ", cols-1))
			}
			for i, ln := range lines {
				at(w, top+i, left, ln)
			}
		} else {
			ts := now.Format("15:04:05")
			at(w, rows/2, (cols-len(ts))/2+1, sgr("1;"+clockANSI(name, "96"), ts))
		}
		at(w, dateRow, 1, sgr("1;91", center(now.Format("Monday, January 2, 2006"), cols))) // light red
		w.Flush()
	}
	draw()

	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-in.quitCh:
			return
		case <-in.events:
			return
		case <-tick.C:
			draw()
		}
	}
}
