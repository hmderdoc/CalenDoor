// The normalized calendar event and the per-calendar color palette.
package main

import (
	"sort"
	"time"
)

// Event is one calendar entry, normalized from an ICS VEVENT. Times are in the
// door's configured location; End is always after Start (the parser fills a
// sensible default when the source omits it).
type Event struct {
	Start, End time.Time
	AllDay     bool
	Summary    string
	Location   string
	Desc       string
	UID        string
	CalIdx     int    // index into cfg.Calendars (drives the color)
	rrule      string // raw RRULE, if any (expanded into occurrences at load)
}

// occursOn reports whether the event overlaps the given calendar day.
func (e Event) occursOn(day time.Time) bool {
	d0 := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location())
	d1 := d0.AddDate(0, 0, 1)
	return e.Start.Before(d1) && e.End.After(d0)
}

// colorName palette: friendly names -> ANSI bright-foreground SGR codes. Used
// both for explicit "color =" config and for auto-assignment by calendar order.
var paletteOrder = []string{"cyan", "green", "yellow", "magenta", "blue", "red", "white"}

var colorSGR = map[string]string{
	"red":     "91",
	"green":   "92",
	"yellow":  "93",
	"blue":    "94",
	"magenta": "95",
	"cyan":    "96",
	"white":   "97",
	"gray":    "90",
	"grey":    "90",
}

// sgrFor returns the SGR foreground code for a color name, defaulting to white.
func sgrFor(name string) string {
	if c, ok := colorSGR[name]; ok {
		return c
	}
	return "97"
}

// sortEvents orders events chronologically by start. All-day events start at
// local midnight, so they naturally precede timed events on the same day; the
// AllDay tiebreak only matters when two starts are exactly equal. (Sorting
// all-day first *globally* would wrongly float later all-day events above
// earlier timed ones in cross-day lists like Upcoming.)
func sortEvents(evs []Event) {
	sort.SliceStable(evs, func(i, j int) bool {
		if !evs[i].Start.Equal(evs[j].Start) {
			return evs[i].Start.Before(evs[j].Start)
		}
		return evs[i].AllDay && !evs[j].AllDay
	})
}

// eventsOn returns the events overlapping a day, sorted for display.
func eventsOn(evs []Event, day time.Time) []Event {
	var out []Event
	for _, e := range evs {
		if e.occursOn(day) {
			out = append(out, e)
		}
	}
	sortEvents(out)
	return out
}
