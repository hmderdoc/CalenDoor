// Upcoming view (the default): a scrollable agenda of what's next across all
// calendars, grouped by day, each row led by a colored "starts in" countdown
// (NOW / in 5 min / in 4 hr / tomorrow / in 3 days). ENTER opens that day.
package main

import (
	"bufio"
	"fmt"
	"strings"
	"time"
)

// upcomingEvents returns events that are ongoing or still to come, sorted.
func upcomingEvents(events []Event, now time.Time) []Event {
	var out []Event
	for _, e := range events {
		if e.End.After(now) {
			out = append(out, e)
		}
	}
	sortEvents(out)
	return out
}

// startsIn is the compact countdown shown in the lead column.
func startsIn(start, end, now time.Time) string {
	if !start.After(now) {
		if end.After(now) {
			return "NOW"
		}
		return "ended"
	}
	d := start.Sub(now)
	switch {
	case d < time.Minute:
		return "<1 min"
	case d < time.Hour:
		return fmt.Sprintf("in %d min", int(d.Minutes()))
	case d < 24*time.Hour:
		h, m := int(d.Hours()), int(d.Minutes())%60
		if m == 0 {
			return fmt.Sprintf("in %d hr", h)
		}
		return fmt.Sprintf("in %dh%dm", h, m)
	case d < 48*time.Hour:
		return "tomorrow"
	case d < 7*24*time.Hour:
		return fmt.Sprintf("in %d days", int(d.Hours())/24)
	default:
		return fmt.Sprintf("in %d wk", int(d.Hours())/24/7)
	}
}


func renderUpcoming(w *bufio.Writer, cols, rows int, cfg config, events []Event, now time.Time, sel int) {
	th := cfg.Theme
	// masthead: brand (cyan), a clean separator bar (magenta), view name (yellow).
	brand, sep, view := cfg.Title, " \xb3 ", "Upcoming"
	hc := (cols-(len(brand)+len(sep)+len(view)))/2 + 1
	if hc < 1 {
		hc = 1
	}
	at(w, 1, hc, sgr("1;96", brand)+sgr("1;35", sep)+sgr("1;93", view))

	list := upcomingEvents(events, now)
	if len(list) == 0 {
		at(w, rows/2, (cols-18)/2+1, sgr("1;91", "Nothing upcoming."))
		drawHints(w, cols, rows, th, upcomingHints())
		return
	}
	if sel >= len(list) {
		sel = len(list) - 1
	}
	if sel < 0 {
		sel = 0
	}
	today := dayOf(now, cfg.Loc)

	// Flatten to display rows (date header + events) so scrolling is exact.
	type drow struct {
		header bool
		raw    bool   // detail row whose text is already styled (location/JOIN line)
		text   string
		sub    string // date header: the relative-day suffix (today/tomorrow/...)
		idx    int
	}
	var disp []drow
	selRow, lastDay := 0, ""
	for i, e := range list {
		dl := e.Start.In(cfg.Loc).Format("Mon, Jan 2")
		if dl != lastDay {
			if lastDay != "" {
				disp = append(disp, drow{idx: -2}) // blank line between day groups
			}
			disp = append(disp, drow{header: true, text: dl, sub: relativeDay(dayOf(e.Start, cfg.Loc), today)})
			lastDay = dl
		}
		if i == sel {
			selRow = len(disp)
		}
		disp = append(disp, drow{idx: i})
		// Every event shows its details inline (no drill-down needed): the location
		// (with a shimmering JOIN NOW for a hoppable live event), then the description.
		if ll := joinLine(cfg, e, now, cols); ll != "" {
			disp = append(disp, drow{idx: -1, raw: true, text: ll})
		}
		for _, line := range descLines(e, cols) {
			disp = append(disp, drow{idx: -1, text: line})
		}
	}

	top, bottom := 3, rows-2
	visible := bottom - top + 1
	scroll := 0
	if selRow >= visible {
		scroll = selRow - visible + 1
	}

	r := top
	for di := scroll; di < len(disp) && r <= bottom; di++ {
		d := disp[di]
		if d.idx == -2 { // blank spacer between day groups
			at(w, r, 1, repeat(" ", cols-1))
			r++
			continue
		}
		if d.header { // date divider: a rule across the left, date tucked to the right
			relday := strings.TrimSpace(d.sub)
			right := d.text
			if relday != "" {
				right += "  " + relday
			}
			rcol := cols - 1 - len(right)
			if rcol < 2 {
				rcol = 2
			}
			if rcol > 2 {
				at(w, r, 1, sgr("0;36", repeat("\xc4", rcol-2)))
			}
			at(w, r, rcol, sgr("1;35", d.text)) // date in magenta
			if relday != "" {
				at(w, r, rcol+len(d.text), "  "+styledRelday(relday))
			}
			r++
			continue
		}
		if d.idx < 0 { // detail line: pre-styled location/JOIN line, or wrapped description
			if d.raw {
				at(w, r, 1, d.text)
			} else {
				at(w, r, 1, sgr("1;37", padRight("     "+d.text, cols-2)))
			}
			r++
			continue
		}
		e := list[d.idx]
		isSel := d.idx == sel
		tr := timeRange(e, cfg.Loc)
		chip, chipFg := chipFor(e, now, cfg)

		// the event TITLE is the hero (light red); time range + chip are a quieter
		// right-hand cluster. Selection is a multi-colored ">>>" arrow (no bg bar).
		cursor := "    "
		if isSel {
			cursor = "\x1b[1;91m>\x1b[1;93m>\x1b[1;92m>\x1b[0m " // red/yellow/green arrow
		}
		seg := func(fg, s string) string { return "\x1b[" + fg + "m" + s }

		rightLen := len(tr) + 2 + len(chip)
		titleW := cols - 6 - rightLen
		if titleW < 8 {
			titleW = 8
		}
		line := cursor +
			seg("1;"+eventColorSGR(cfg, e), padRight(truncate(e.Summary, titleW), titleW)) + // calendar color
			seg("0;90", " ") +
			seg("0;36", tr) +
			seg("0;90", "  ") +
			seg(chipFg, chip)
		at(w, r, 1, line+"\x1b[0m")
		r++
	}
	for ; r <= bottom; r++ { // clear any leftover rows below the list
		at(w, r, 2, repeat(" ", cols-2))
	}
	hints := upcomingHints()
	if sel < len(list) && cfg.TelnetGate { // selected a live remote BBS? promote the hop
		if host, _, ok := joinTargetFor(list[sel], now); ok && !isLocalHost(host, cfg) {
			hints = []hint{
				{"ENTER", "hop to " + truncate(host, 22)},
				{"up/dn", "select"}, {"M/W", "views"}, {"Q", "quit"},
			}
		}
	}
	drawHints(w, cols, rows, th, hints)
}

// chipFor returns the right-hand status chip: HERE NOW (live on this board),
// NOW (any ongoing event), or the countdown to start. The JOIN call-to-action
// lives on the detail row (joinLine), not here.
func chipFor(e Event, now time.Time, cfg config) (string, string) {
	if !e.Start.After(now) { // ongoing
		if host, _, tn := joinTargetFor(e, now); tn && isLocalHost(host, cfg) {
			return "HERE NOW", "1;96" // bright cyan: live, right here
		}
		return "NOW", "1;92" // bright green
	}
	rel := startsIn(e.Start, e.End, now)
	switch d := e.Start.Sub(now); {
	case d < time.Hour:
		return rel, "1;93" // bright yellow (imminent)
	case d < 24*time.Hour:
		return rel, "1;96" // bright cyan (today)
	default:
		return rel, "0;36" // cyan (later)
	}
}

// joinLine is the detail row that surfaces an event's location: "@<location>"
// (blue @, light-blue location), with a shimmering "JOIN NOW" in front when it's a
// live, hoppable BBS. Returns "" when there's no location.
func joinLine(cfg config, e Event, now time.Time, cols int) string {
	if e.Location == "" {
		return ""
	}
	join := ""
	if host, _, tn := joinTargetFor(e, now); tn && cfg.TelnetGate && !isLocalHost(host, cfg) {
		join = "\x1b[1;5;92mJOIN NOW\x1b[0m  " // shimmering (blinking) green
	}
	loc := truncate(e.Location, cols-22)
	return "     " + join + "\x1b[0;34m@\x1b[1;95m" + loc + "\x1b[0m"
}

// descLines wraps an event's description (no truncation/ellipsis).
func descLines(e Event, cols int) []string {
	if e.Desc == "" {
		return nil
	}
	return wrapText(strings.ReplaceAll(e.Desc, "\n", " "), cols-8)
}

// timeRange renders an event's span (which conveys its duration): "7:00-9:00pm",
// "11:30am-1:00pm", "all day", or a date span for multi-day all-day events.
func timeRange(e Event, loc *time.Location) string {
	if e.AllDay {
		if e.End.Sub(e.Start) > 24*time.Hour {
			return e.Start.In(loc).Format("Jan 2") + "-" + e.End.AddDate(0, 0, -1).In(loc).Format("Jan 2")
		}
		return "all day"
	}
	s, en := e.Start.In(loc), e.End.In(loc)
	if s.Year() != en.Year() || s.YearDay() != en.YearDay() { // crosses midnight
		return s.Format("3:04pm") + "-" + en.Format("Jan2 3:04pm")
	}
	start := s.Format("3:04")
	if strings.EqualFold(s.Format("PM"), en.Format("PM")) {
		return start + "-" + en.Format("3:04pm") // same half of day: drop the first am/pm
	}
	return start + s.Format("pm") + "-" + en.Format("3:04pm")
}

// styledRelday colors the "(today)"/"(tomorrow)" tag: for today the parens are
// light-green and the word is yellow; for any other day the parens are yellow and
// the word is plain green.
func styledRelday(relday string) string {
	inner := strings.Trim(relday, "()")
	parenC, textC := "1;93", "0;32" // others: yellow parens, regular-green word
	if inner == "today" {
		parenC, textC = "1;92", "1;93" // today: light-green parens, yellow word
	}
	return sgr(parenC, "(") + sgr(textC, inner) + sgr(parenC, ")")
}

// relativeDay returns " (today)" / " (tomorrow)" / " (this week)" for context.
func relativeDay(day, today time.Time) string {
	n := int(day.Sub(today).Hours() / 24)
	switch {
	case n <= 0:
		return "  (today)"
	case n == 1:
		return "  (tomorrow)"
	case n < 7:
		return "  (this week)"
	default:
		return ""
	}
}

func upcomingHints() []hint {
	return []hint{
		{"up/dn", "select"}, {"ENTER", "open day"}, {"M/W", "views"}, {"C", "cal"},
		{"T", "today"}, {"S", "settings"}, {"R", "refresh"}, {"Q", "quit"},
	}
}
