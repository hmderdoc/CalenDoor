// Timezone picker: a scrollable lightbar of common zones showing each one's
// current local time, so a caller picks "the clock that matches mine." Shown on
// first run and from the settings screen. Navigation repaints only the rows that
// change (no full-screen clear), so the lightbar doesn't flicker.
package main

import (
	"bufio"
	"time"
)

type tzEntry struct{ label, zone string }

// A curated list (full IANA is hundreds of entries); covers the zones a BBS
// crowd actually spans. Add more here as needed.
var tzList = []tzEntry{
	{"UTC", "UTC"},
	{"Hawaii", "Pacific/Honolulu"},
	{"Alaska", "America/Anchorage"},
	{"US Pacific - Los Angeles", "America/Los_Angeles"},
	{"US Mountain - Denver", "America/Denver"},
	{"US Arizona - Phoenix (no DST)", "America/Phoenix"},
	{"US Central - Chicago", "America/Chicago"},
	{"US Eastern - New York", "America/New_York"},
	{"Canada Atlantic - Halifax", "America/Halifax"},
	{"Mexico City", "America/Mexico_City"},
	{"Brazil - Sao Paulo", "America/Sao_Paulo"},
	{"UK - London", "Europe/London"},
	{"Central Europe - Paris/Berlin", "Europe/Paris"},
	{"Eastern Europe - Athens", "Europe/Athens"},
	{"Moscow", "Europe/Moscow"},
	{"UAE - Dubai", "Asia/Dubai"},
	{"India - Kolkata", "Asia/Kolkata"},
	{"Thailand - Bangkok", "Asia/Bangkok"},
	{"China - Shanghai", "Asia/Shanghai"},
	{"Singapore", "Asia/Singapore"},
	{"Japan - Tokyo", "Asia/Tokyo"},
	{"Australia West - Perth", "Australia/Perth"},
	{"Australia East - Sydney", "Australia/Sydney"},
	{"New Zealand - Auckland", "Pacific/Auckland"},
}

const tzListTop = 4

// drawTZRow paints one picker row, fully overwriting columns 2..cols-1 so a
// move cleanly replaces the previous highlight (no clear needed).
func drawTZRow(w *bufio.Writer, cols, row, idx int, selected bool) {
	e := tzList[idx]
	now := "--:--"
	if loc, err := time.LoadLocation(e.zone); err == nil {
		now = time.Now().In(loc).Format("Mon 15:04")
	}
	fw := cols - 4
	if fw < 14 {
		fw = 14
	}
	label := e.label
	if len(label) > fw-len(now)-1 {
		label = truncate(label, fw-len(now)-1)
	}
	gap := fw - len(label) - len(now)
	if gap < 1 {
		gap = 1
	}
	if selected {
		at(w, row, 2, sgr("1;33", "> ")+sgr("1;30;46", label+repeat(" ", gap)+now))
	} else {
		at(w, row, 2, "  "+sgr("0;37", label)+repeat(" ", gap)+sgr("0;36", now))
	}
}

// drawTZList repaints just the list region (no full-screen clear), used on first
// paint and when the scroll window moves.
func drawTZList(w *bufio.Writer, cols, rows, sel, scroll, visible int) {
	for i := 0; i < visible; i++ {
		idx := scroll + i
		if idx < len(tzList) {
			drawTZRow(w, cols, tzListTop+i, idx, idx == sel)
		} else {
			at(w, tzListTop+i, 2, repeat(" ", cols-2)) // clear a trailing empty row
		}
	}
}

func runTZPicker(w *bufio.Writer, in *input, cols, rows int, startZone string) string {
	sel := 0
	for i, e := range tzList {
		if e.zone == startZone {
			sel = i
		}
	}
	scroll, prevSel, prevScroll := 0, -1, -2
	first := true
	for {
		visible := rows - tzListTop - 1
		if visible < 1 {
			visible = 1
		}
		if sel < scroll {
			scroll = sel
		}
		if sel >= scroll+visible {
			scroll = sel - visible + 1
		}
		switch {
		case first:
			cls(w)
			at(w, 1, 1, sgr("1;30;46", center("Choose your timezone", cols)))
			at(w, 2, 2, sgr("0;37", truncate("Events display in the zone you pick.  Current: "+startZone, cols-2)))
			drawTZList(w, cols, rows, sel, scroll, visible)
			drawFooter(w, cols, rows, "up/dn select  \xb3  ENTER choose  \xb3  ESC keep current")
		case scroll != prevScroll:
			drawTZList(w, cols, rows, sel, scroll, visible) // window moved: repaint list only
		case sel != prevSel:
			if prevSel >= scroll && prevSel < scroll+visible {
				drawTZRow(w, cols, tzListTop+prevSel-scroll, prevSel, false)
			}
			drawTZRow(w, cols, tzListTop+sel-scroll, sel, true)
		}
		w.Flush()
		first, prevSel, prevScroll = false, sel, scroll

		select {
		case <-in.quitCh:
			return ""
		case k := <-in.events:
			switch {
			case k == keyUp:
				if sel > 0 {
					sel--
				}
			case k == keyDown:
				if sel < len(tzList)-1 {
					sel++
				}
			case k == keyPgUp:
				if sel -= visible; sel < 0 {
					sel = 0
				}
			case k == keyPgDn:
				if sel += visible; sel > len(tzList)-1 {
					sel = len(tzList) - 1
				}
			case k == keyEnter:
				return tzList[sel].zone
			case k == keyEsc || k == keyBack || (k > 0 && toLower(rune(k)) == 'q'):
				return ""
			}
		}
	}
}
