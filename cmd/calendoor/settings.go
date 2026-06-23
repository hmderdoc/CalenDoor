// Settings screen: a small lightbar menu reachable with S. Today it holds the
// display timezone and a (read-only) calendar list; it's the home for in-app
// calendar management and QR subscribe in later phases. Like the timezone
// picker, moving the bar repaints only the changed rows (no flicker).
package main

import (
	"bufio"
	"fmt"
	"strings"
)

var settingsItems = []string{"Display timezone", "Shared calendars", "My calendars"}

const settingsTop = 4

func settingsValue(a *app, i int) string {
	switch i {
	case 0:
		return a.cfg.Loc.String()
	case 1:
		return fmt.Sprintf("%d available", len(a.sysCals))
	case 2:
		return fmt.Sprintf("%d added", a.personalCount())
	}
	return ""
}

func drawSettingsRow(w *bufio.Writer, cols int, a *app, i int, selected bool) {
	row := settingsTop + i*2
	label, val := settingsItems[i], settingsValue(a, i)
	field := padRight(label, 22) + val
	if selected {
		at(w, row, 2, sgr("1;33", "> ")+sgr("1;30;46", padRight(field, cols-4)))
	} else {
		at(w, row, 2, "  "+sgr("0;37", padRight(label, 22))+sgr("1;36", padRight(val, cols-4-22)))
	}
}

func runSettings(w *bufio.Writer, in *input, cols, rows int, a *app, handle string, prefs userPrefs) {
	sel, prevSel, first := 0, -1, true
	for {
		if first {
			cls(w)
			at(w, 1, 1, sgr("1;30;46", center(a.cfg.Title+"  -  Settings", cols)))
			for i := range settingsItems {
				drawSettingsRow(w, cols, a, i, i == sel)
			}
			at(w, rows-3, 4, sgr("0;37", truncate("Shared calendars = subscribe on your phone (QR).  My calendars = add your own.", cols-6)))
			drawFooter(w, cols, rows, "up/dn  \xb3  ENTER change  \xb3  ESC back")
		} else if sel != prevSel {
			drawSettingsRow(w, cols, a, prevSel, false)
			drawSettingsRow(w, cols, a, sel, true)
		}
		w.Flush()
		first, prevSel = false, sel

		select {
		case <-in.quitCh:
			return
		case k := <-in.events:
			switch {
			case k == keyUp:
				if sel > 0 {
					sel--
				}
			case k == keyDown:
				if sel < len(settingsItems)-1 {
					sel++
				}
			case k == keyEnter:
				switch sel {
				case 0:
					if zone := runTZPicker(w, in, cols, rows, a.cfg.Loc.String()); zone != "" && zone != a.cfg.Loc.String() {
						if a.applyTZ(zone) {
							prefs.TZ[handle] = zone
							prefs.save()
							a.splash(w, cols, rows, "Reloading in "+zone+"...")
							a.reload()
						}
					}
				case 1:
					runCalendarList(w, in, cols, rows, a)
				case 2:
					runMyCalendars(w, in, cols, rows, a, handle, prefs)
				}
				first = true // returning from a sub-screen: full repaint
			case k == keyEsc || k == keyBack || (k > 0 && toLower(rune(k)) == 'q'):
				return
			}
		}
	}
}

const calTop = 4

func drawCalRow(w *bufio.Writer, cols int, cals []Calendar, i int, selected bool) {
	c := cals[i]
	row := calTop + i
	swatch := sgr(sgrFor(c.Color), sqr+" ")
	name := c.Name
	if selected {
		field := padRight(name, 22) + truncate(hostOf(c.URL), cols-6-22)
		at(w, row, 2, sgr("1;33", "> ")+swatch+sgr("1;30;46", padRight(field, cols-6)))
	} else {
		at(w, row, 2, "  "+swatch+sgr("1;37", padRight(name, 22))+
			sgr("0;90", padRight(truncate(hostOf(c.URL), cols-28), cols-28)))
	}
}

// runCalendarList lists the calendars; ENTER opens the phone-subscribe (QR)
// screen for the selected one. (Adding calendars is a sysop task done outside the
// door, so there's no config talk here.)
func runCalendarList(w *bufio.Writer, in *input, cols, rows int, a *app) {
	cals := a.sysCals // shared (system) calendars only; personal ones are already on the phone
	if len(cals) == 0 {
		cls(w)
		at(w, 1, 1, sgr("1;30;46", center("Shared calendars", cols)))
		at(w, rows/2, (cols-44)/2+1, sgr("1;91", "No shared calendars are set up on this board yet."))
		drawFooter(w, cols, rows, "ESC back")
		w.Flush()
		for {
			select {
			case <-in.quitCh:
				return
			case k := <-in.events:
				if k == keyEsc || k == keyBack || (k > 0 && toLower(rune(k)) == 'q') {
					return
				}
			}
		}
	}

	sel, prevSel, first := 0, -1, true
	for {
		if first {
			cls(w)
			at(w, 1, 1, sgr("1;30;46", center("Shared calendars  -  subscribe on your phone", cols)))
			for i := range cals {
				drawCalRow(w, cols, cals, i, i == sel)
			}
			drawFooter(w, cols, rows, "up/dn select  \xb3  ENTER subscribe (QR)  \xb3  ESC back")
		} else if sel != prevSel {
			drawCalRow(w, cols, cals, prevSel, false)
			drawCalRow(w, cols, cals, sel, true)
		}
		w.Flush()
		first, prevSel = false, sel

		select {
		case <-in.quitCh:
			return
		case k := <-in.events:
			switch {
			case k == keyUp:
				if sel > 0 {
					sel--
				}
			case k == keyDown:
				if sel < len(cals)-1 {
					sel++
				}
			case k == keyEnter:
				runQRSubscribe(w, in, cols, rows, cals[sel])
				first = true
			case k == keyEsc || k == keyBack || (k > 0 && toLower(rune(k)) == 'q'):
				return
			}
		}
	}
}

// hostOf returns just the host of a feed URL, so the settings list never prints a
// secret iCal address in full.
func hostOf(u string) string {
	for _, p := range []string{"https://", "http://", "webcal://"} {
		u = strings.TrimPrefix(u, p)
	}
	if i := strings.IndexByte(u, '/'); i >= 0 {
		return u[:i] + "/..."
	}
	return u
}
