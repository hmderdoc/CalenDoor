// Weekly view: the seven days of the focused week as columns, each listing its
// events with start times, color-coded by calendar. More vertical room per day
// than the month grid, so it shows more per day.
package main

import (
	"bufio"
	"fmt"
	"time"
)

// weekStartOf returns the cfg.WeekStart on/before focus.
func weekStartOf(focus time.Time, cfg config) time.Time {
	back := (int(focus.Weekday()) - int(cfg.WeekStart) + 7) % 7
	return focus.AddDate(0, 0, -back)
}

func renderWeek(w *bufio.Writer, cols, rows int, cfg config, events []Event, focus, today time.Time) {
	th := cfg.Theme
	bc := th.borderSGR(focus.Month())
	start := weekStartOf(focus, cfg)
	end := start.AddDate(0, 0, 6)

	// --- header (row 1): the week range, in the month's accent color ---
	banner := start.Format("Jan 2") + " - " + end.Format("Jan 2, 2006")
	at(w, 1, (cols-len(banner))/2+1, sgr(th.titleSGR(focus.Month()), banner))

	cellW := (cols - 8) / 7
	if cellW < 8 {
		cellW = 8
	}
	gridW := 7*cellW + 8
	x0 := (cols-gridW)/2 + 1
	if x0 < 1 {
		x0 = 1
	}
	colX := func(i int) int { return x0 + i*(cellW+1) }

	// Day headers (row 2) + top rule (row 3).
	for i := 0; i < 7; i++ {
		d := start.AddDate(0, 0, i)
		style := "1;" + th.weekday
		if sameDay(d, today) {
			style = th.today
		} else if sameDay(d, focus) {
			style = th.focus
		}
		at(w, 2, colX(i)+1, sgr(style, center(d.Format("Mon 2"), cellW)))
	}
	drawRule(w, 3, x0, cellW, bxTL, bxTT, bxTR, bc)

	top := 4
	bottom := rows - 2 // leave row rows-1 for the bottom rule, rows for the footer
	for r := top; r <= bottom; r++ { // vertical separators down the body
		for i := 0; i <= 7; i++ {
			at(w, r, colX(i), sgr(bc, bxV))
		}
	}
	for i := 0; i < 7; i++ {
		d := start.AddDate(0, 0, i)
		evs := eventsOn(events, d)
		cx := colX(i) + 1
		row := top
		for _, e := range evs {
			if row > bottom {
				at(w, row-1, cx, sgr("0;90", truncate(fmt.Sprintf("+%d", len(evs)-(row-top)), cellW-1)))
				break
			}
			label := e.Summary
			if !e.AllDay {
				label = e.Start.In(cfg.Loc).Format("15:04") + " " + label
			}
			at(w, row, cx, sgr(eventColorSGR(cfg, e), truncate(label, cellW-1)))
			row++
		}
		if len(evs) == 0 {
			at(w, top, cx, sgr("0;90", truncate(mdot, cellW-1)))
		}
	}
	drawRule(w, rows-1, x0, cellW, bxBL, bxBT, bxBR, bc) // close the grid at the bottom
	drawHints(w, cols, rows, th, []hint{
		{"left/right", "day"}, {"up/dn", "week"}, {"ENTER", "day"},
		{"M/U", "views"}, {"C", "cal"}, {"T", "today"}, {"Q", "quit"},
	})
}
