// Monthly grid view: a 7-column calendar for the focused month with color-coded
// event titles in each day cell, today and the focused day highlighted.
package main

import (
	"bufio"
	"fmt"
	"time"
)

// eventColorSGR returns the SGR foreground code for an event's calendar.
func eventColorSGR(cfg config, e Event) string {
	if e.CalIdx >= 0 && e.CalIdx < len(cfg.Calendars) {
		return sgrFor(cfg.Calendars[e.CalIdx].Color)
	}
	return "97"
}

// weekdayLabels returns the 7 column headers starting at cfg.WeekStart.
func weekdayLabels(cfg config) []string {
	names := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	out := make([]string, 7)
	for i := 0; i < 7; i++ {
		out[i] = names[(int(cfg.WeekStart)+i)%7]
	}
	return out
}

// gridStart returns the first cell date (the WeekStart on/before the 1st).
func gridStart(focus time.Time, cfg config) time.Time {
	first := time.Date(focus.Year(), focus.Month(), 1, 0, 0, 0, 0, cfg.Loc)
	back := (int(first.Weekday()) - int(cfg.WeekStart) + 7) % 7
	return first.AddDate(0, 0, -back)
}

// weeksInMonth returns how many week-rows the month spans in this grid (4-6).
func weeksInMonth(focus time.Time, cfg config) int {
	start := gridStart(focus, cfg)
	last := time.Date(focus.Year(), focus.Month()+1, 0, 0, 0, 0, 0, cfg.Loc) // last day
	days := int(last.Sub(start).Hours()/24) + 1
	return (days + 6) / 7
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

func renderMonth(w *bufio.Writer, cols, rows int, cfg config, events []Event, focus, today time.Time) {
	weeks := weeksInMonth(focus, cfg)
	cellW := (cols - 8) / 7 // fill the available width (no cap)
	if cellW < 8 {
		cellW = 8
	}
	gridW := 7*cellW + 8
	x0 := (cols-gridW)/2 + 1
	if x0 < 1 {
		x0 = 1
	}

	// Vertical space: rows 1 title, 2 weekday labels, 3 top rule; footer at bottom.
	// Fill the available height too (no cap) so taller terminals show more per day.
	avail := rows - 4
	cellH := avail/weeks - 1
	if cellH < 2 {
		cellH = 2
	}

	th := cfg.Theme
	bc := th.borderSGR(focus.Month())

	// --- header (row 1): just the month + year, in this month's accent color ---
	monthLbl := focus.Format("January 2006")
	at(w, 1, (cols-len(monthLbl))/2+1, sgr(th.titleSGR(focus.Month()), monthLbl))

	// --- weekday header (row 2) ---
	labels := weekdayLabels(cfg)
	for i, lbl := range labels {
		cx := x0 + 1 + i*(cellW+1)
		at(w, 2, cx, sgr("1;"+th.weekday, center(lbl, cellW)))
	}

	// --- grid ---
	colX := func(i int) int { return x0 + i*(cellW+1) }
	// top rule
	drawRule(w, 3, x0, cellW, bxTL, bxTT, bxTR, bc)
	day := gridStart(focus, cfg)
	for wk := 0; wk < weeks; wk++ {
		base := 4 + wk*(cellH+1)
		// vertical separators for each content row
		for r := 0; r < cellH; r++ {
			for i := 0; i <= 7; i++ {
				at(w, base+r, colX(i), sgr(bc, bxV))
			}
		}
		for d := 0; d < 7; d++ {
			cellX := colX(d) + 1
			inMonth := day.Month() == focus.Month()
			// day number (top-left of cell)
			numStyle := th.day
			if !inMonth {
				numStyle = th.dim
			}
			if sameDay(day, today) {
				numStyle = th.today
			} else if sameDay(day, focus) {
				numStyle = th.focus
			}
			num := fmt.Sprintf("%2d", day.Day())
			at(w, base, cellX, sgr(numStyle, num))
			// events
			evs := eventsOn(events, day)
			lines := cellH - 1
			for li := 0; li < lines; li++ {
				ey := base + 1 + li
				if li == lines-1 && len(evs) > lines {
					at(w, ey, cellX, sgr("0;90", truncate(fmt.Sprintf("+%d more", len(evs)-li), cellW-1)))
					break
				}
				if li >= len(evs) {
					break
				}
				e := evs[li]
				label := e.Summary
				if !e.AllDay {
					label = e.Start.In(cfg.Loc).Format("15:04") + " " + label
				}
				at(w, ey, cellX, sgr(eventColorSGR(cfg, e), truncate(label, cellW-1)))
			}
			day = day.AddDate(0, 0, 1)
		}
		// rule below the week
		mid, l, r := bxX, bxLT, bxRT
		if wk == weeks-1 {
			mid, l, r = bxBT, bxBL, bxBR
		}
		drawRule(w, base+cellH, x0, cellW, l, mid, r, bc)
	}

	drawHints(w, cols, rows, th, []hint{
		{"M/W/D/U", "views"}, {"arrows", "move"}, {"PgUp/Dn", "month"}, {"C", "cal"},
		{"T", "today"}, {"S", "settings"}, {"R", "refresh"}, {"Q", "quit"},
	})
}

// drawRule draws a horizontal divider with the given left/mid/right junctions in color.
func drawRule(w *bufio.Writer, row, x0, cellW int, left, mid, right, color string) {
	s := left
	for i := 0; i < 7; i++ {
		s += repeat(bxH, cellW)
		if i < 6 {
			s += mid
		}
	}
	s += right
	at(w, row, x0, sgr(color, s))
}

// drawFooter is the simple single-color footer (black bar) used by secondary
// screens; the main views use drawHints for colored key/description pairs.
func drawFooter(w *bufio.Writer, cols, rows int, help string) {
	at(w, rows, 1, sgr("0;37;40", center(help, cols-1)))
}
