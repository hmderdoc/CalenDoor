// Daily view: an agenda for the focused day - all-day events, then timed events
// in order, with times, location, and a short description.
package main

import (
	"bufio"
	"strings"
	"time"
)

func renderDay(w *bufio.Writer, cols, rows int, cfg config, events []Event, focus, today time.Time) {
	th := cfg.Theme
	banner := focus.Format("Monday, January 2, 2006")
	if sameDay(focus, today) {
		banner += "  (today)"
	}
	at(w, 1, (cols-len(banner))/2+1, sgr(th.titleSGR(focus.Month()), banner))

	evs := eventsOn(events, focus)
	if len(evs) == 0 {
		at(w, rows/2, (cols-12)/2+1, sgr("1;91", "No events."))
		drawHints(w, cols, rows, th, dayHints())
		return
	}

	const indent = 16
	row := 3
	maxRow := rows - 2
	for _, e := range evs {
		if row > maxRow {
			at(w, row, 3, sgr("1;91", "(more events - widen or resize the window)"))
			break
		}
		// time (cyan) + title (bold calendar color)
		at(w, row, 3, sgr("0;36", padRight(timeRange(e, cfg.Loc), indent-3))+
			sgr("1;"+eventColorSGR(cfg, e), truncate(e.Summary, cols-indent-2)))
		row++
		// location: blue @, light-magenta value; then the calendar name in its color
		if e.Location != "" && row <= maxRow {
			seg := "\x1b[0;34m@ \x1b[1;95m" + truncate(e.Location, cols-indent-2) + "\x1b[0m"
			at(w, row, indent, seg)
			row++
		}
		if ci := e.CalIdx; ci >= 0 && ci < len(cfg.Calendars) && row <= maxRow {
			at(w, row, indent, sgr("0;"+eventColorSGR(cfg, e), "["+cfg.Calendars[ci].Name+"]"))
			row++
		}
		// description: WRAPPED (not truncated), white
		if e.Desc != "" {
			for _, ln := range wrapText(strings.ReplaceAll(e.Desc, "\n", " "), cols-indent-2) {
				if row > maxRow {
					break
				}
				at(w, row, indent, sgr("1;37", ln))
				row++
			}
		}
		row++ // blank line between events
	}
	drawHints(w, cols, rows, th, dayHints())
}

func dayHints() []hint {
	return []hint{
		{"left/right", "day"}, {"M/W/U", "views"}, {"C", "cal"}, {"T", "today"}, {"ESC", "back"}, {"Q", "quit"},
	}
}
