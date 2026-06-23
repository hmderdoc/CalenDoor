// Startup welcome: if the caller logs in while an event hosted on THIS board is
// happening now, greet them with it instead of burying it in the list (and there
// is nothing to "hop" to - they're already here).
package main

import (
	"bufio"
	"fmt"
	"time"
)

// localNowEvent returns the first event happening now whose telnet location is
// this BBS itself.
func localNowEvent(events []Event, now time.Time, cfg config) (Event, bool) {
	if cfg.Host == "" {
		return Event{}, false
	}
	for _, e := range upcomingEvents(events, now) {
		if host, _, ok := joinTargetFor(e, now); ok && isLocalHost(host, cfg) {
			return e, true
		}
	}
	return Event{}, false
}

// runWelcome shows a one-screen greeting for an event happening here now.
func runWelcome(w *bufio.Writer, in *input, cols, rows int, cfg config, e Event) {
	now := time.Now().In(cfg.Loc)
	cls(w)
	row := rows/2 - 4
	at(w, row, 1, sgr("1;96", center("Welcome to "+cfg.Title, cols)))
	row += 2
	at(w, row, 1, sgr("1;30;42", center(" HAPPENING RIGHT NOW, HERE ON THIS BOARD ", cols)))
	row += 2
	at(w, row, 1, sgr("1;97", center(e.Summary, cols)))
	row++
	at(w, row, 1, sgr("0;36", center(timeRange(e, cfg.Loc), cols)))
	row++
	if rt := remainingText(e, now); rt != "" {
		at(w, row, 1, sgr("1;91", center(rt, cols))) // light red: how long is left
	}
	if e.Desc != "" {
		row += 2
		for _, ln := range wrapText(e.Desc, cols-8) {
			if row >= rows-2 {
				break
			}
			at(w, row, 1, sgr("1;93", center(ln, cols))) // bright yellow, prominent
			row++
		}
	}
	at(w, rows-1, 1, sgr("0;37", center("Press any key to open the calendar.", cols)))
	w.Flush()
	waitKey(in)
}

// remainingText is the light-red "time left" line for a live event.
func remainingText(e Event, now time.Time) string {
	d := e.End.Sub(now)
	if d <= 0 {
		return ""
	}
	if e.AllDay {
		return "happening all day today"
	}
	mins := int(d.Minutes())
	switch {
	case mins < 1:
		return "less than a minute remains for the event"
	case mins == 1:
		return "1 minute remains for the event"
	case mins < 60:
		return fmt.Sprintf("%d minutes remain for the event", mins)
	}
	h, m := mins/60, mins%60
	if m == 0 {
		return fmt.Sprintf("%d hours remain for the event", h)
	}
	return fmt.Sprintf("%dh %dm remain for the event", h, m)
}
