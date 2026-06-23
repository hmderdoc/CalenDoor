// "My Calendars": per-caller personal feeds. A caller pastes their own private
// iCal link (Google secret address / iCloud public webcal), the door validates
// it, stores it in their prefs, and overlays their events on the shared
// calendars - read-only, visible only to them.
package main

import (
	"bufio"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const calMyTop = 4

func drawMyRow(w *bufio.Writer, cols int, cals []userCal, i int, selected bool) {
	row := calMyTop + i
	if i == len(cals) { // the trailing "add" row
		txt := "+ Add a calendar"
		if selected {
			at(w, row, 2, sgr("1;33", "> ")+sgr("1;30;42", padRight(txt, cols-4)))
		} else {
			at(w, row, 2, "  "+sgr("1;32", padRight(txt, cols-4)))
		}
		return
	}
	c := cals[i]
	swatch := sgr(sgrFor(c.Color), sqr+" ")
	if selected {
		field := padRight(c.Name, 22) + truncate(hostOf(c.URL), cols-6-22)
		at(w, row, 2, sgr("1;33", "> ")+swatch+sgr("1;30;46", padRight(field, cols-6)))
	} else {
		at(w, row, 2, "  "+swatch+sgr("1;37", padRight(c.Name, 22))+
			sgr("0;90", padRight(truncate(hostOf(c.URL), cols-28), cols-28)))
	}
}

func runMyCalendars(w *bufio.Writer, in *input, cols, rows int, a *app, handle string, prefs userPrefs) {
	sel, prevSel, first := 0, -1, true
	for {
		cals := prefs.Cals[handle]
		n := len(cals) + 1 // + the add row
		if sel >= n {
			sel = n - 1
		}
		if sel < 0 {
			sel = 0
		}
		if first {
			cls(w)
			at(w, 1, 1, sgr("1;30;46", center("My Calendars", cols)))
			at(w, 2, 2, sgr("0;37", truncate("Your own feeds, shown only to you, over the shared calendars.", cols-2)))
			for i := 0; i < n; i++ {
				drawMyRow(w, cols, cals, i, i == sel)
			}
			for r := calMyTop + n; r <= rows-2; r++ { // clear leftovers from a longer list
				at(w, r, 2, repeat(" ", cols-2))
			}
			drawFooter(w, cols, rows, "up/dn  \xb3  ENTER add  \xb3  D remove  \xb3  ESC back")
		} else if sel != prevSel {
			drawMyRow(w, cols, cals, prevSel, false)
			drawMyRow(w, cols, cals, sel, true)
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
				if sel < n-1 {
					sel++
				}
			case k == keyEnter:
				if sel == len(cals) { // add row
					addMyCalendar(w, in, cols, rows, a, handle, prefs)
					first = true
				}
			case k > 0 && toLower(rune(k)) == 'a':
				addMyCalendar(w, in, cols, rows, a, handle, prefs)
				first = true
			case k > 0 && toLower(rune(k)) == 'd':
				if sel < len(cals) {
					c := cals[sel]
					if yesNo(w, in, cols, rows, []string{"Remove your calendar:", "  " + c.Name, "", "(It stays on your phone; this only removes the overlay.)"}) {
						prefs.Cals[handle] = append(append([]userCal{}, cals[:sel]...), cals[sel+1:]...)
						prefs.save()
						a.rebuildCalendars(prefs, handle)
						a.splash(w, cols, rows, "Reloading...")
						a.reload()
					}
					first = true
				}
			case k == keyEsc || k == keyBack || (k > 0 && toLower(rune(k)) == 'q'):
				return
			}
		}
	}
}

// addMyCalendar runs the paste-URL -> name -> validate -> confirm flow.
func addMyCalendar(w *bufio.Writer, in *input, cols, rows int, a *app, handle string, prefs userPrefs) {
	help := []string{
		"Paste your calendar's PRIVATE iCal link (https:// or webcal://).",
		"Google: Settings > your calendar > Integrate > Secret address in iCal format.",
		"iCloud: share the calendar Public, then copy its webcal link.",
		"Note: the link is stored on this BBS and lets it read your calendar.",
	}
	url, ok := promptLine(w, in, cols, rows, "Add a calendar  -  paste the link", help, "")
	if !ok || strings.TrimSpace(url) == "" {
		return
	}
	name, ok := promptLine(w, in, cols, rows, "Add a calendar  -  name it", []string{"A short label (you can change it later):"}, "My Calendar")
	if !ok {
		return
	}
	if strings.TrimSpace(name) == "" {
		name = "My Calendar"
	}

	a.splash(w, cols, rows, "Checking calendar...")
	client := &http.Client{Timeout: 20 * time.Second}
	data, err := fetchICS(client, url)
	var confirm []string
	if err != nil {
		confirm = []string{"Couldn't load that link:", "  " + truncate(err.Error(), cols-6), "", "Add it anyway?"}
	} else {
		confirm = []string{fmt.Sprintf("Loaded OK - found %d events.", len(parseICS(data, 0, a.cfg.Loc))), "", "Add this calendar?"}
	}
	if !yesNo(w, in, cols, rows, confirm) {
		return
	}

	color := paletteOrder[(len(a.sysCals)+len(prefs.Cals[handle]))%len(paletteOrder)]
	prefs.Cals[handle] = append(prefs.Cals[handle], userCal{Name: name, URL: strings.TrimSpace(url), Color: color})
	prefs.save()
	a.rebuildCalendars(prefs, handle)
	a.splash(w, cols, rows, "Reloading...")
	a.reload()
}

// promptLine reads a single line (typed or pasted). It draws its chrome once and
// repaints only the input field; a paste arrives as a burst, so it drains all
// queued bytes before each repaint (and the input buffer is large) to avoid
// dropping characters from a long URL.
func promptLine(w *bufio.Writer, in *input, cols, rows int, title string, help []string, prefill string) (string, bool) {
	buf := []rune(prefill)
	field := func() {
		fw := cols - 4
		show := string(buf)
		if len(show) > fw-1 {
			show = show[len(show)-(fw-1):]
		}
		at(w, 7, 2, sgr("1;30;47", padRight(show+"_", fw)))
		w.Flush()
	}
	cls(w)
	at(w, 1, 1, sgr("1;30;46", center(title, cols)))
	for i, h := range help {
		if i < 4 {
			at(w, 3+i, 2, sgr("0;37", truncate(h, cols-2)))
		}
	}
	at(w, rows-1, 2, sgr("0;37", "ENTER save   ESC cancel   BACKSPACE delete"))
	field()

	apply := func(k key) (done, submit bool) {
		switch {
		case k == keyEnter:
			return true, true
		case k == keyEsc:
			return true, false
		case k == keyBack:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
			}
		case k > 0 && k < 0x7f && len(buf) < 1024:
			buf = append(buf, rune(k))
		}
		return false, false
	}
	for {
		select {
		case <-in.quitCh:
			return "", false
		case k := <-in.events:
			if done, submit := apply(k); done {
				if submit {
					return strings.TrimSpace(string(buf)), true
				}
				return "", false
			}
			// drain a paste burst before repainting
		drain:
			for {
				select {
				case k2 := <-in.events:
					if done, submit := apply(k2); done {
						if submit {
							return strings.TrimSpace(string(buf)), true
						}
						return "", false
					}
				default:
					break drain
				}
			}
			field()
		}
	}
}

// yesNo shows a confirmation; only Y confirms (any other key cancels).
func yesNo(w *bufio.Writer, in *input, cols, rows int, lines []string) bool {
	cls(w)
	at(w, 1, 1, sgr("1;30;46", center("Confirm", cols)))
	for i, l := range lines {
		at(w, 3+i, 2, sgr("0;37", truncate(l, cols-2)))
	}
	at(w, rows-1, 2, sgr("1;33", "Y = yes      any other key = no"))
	w.Flush()
	for {
		select {
		case <-in.quitCh:
			return false
		case k := <-in.events:
			return k > 0 && toLower(rune(k)) == 'y'
		}
	}
}
