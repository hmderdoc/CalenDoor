// A pragmatic RRULE expander. Not a full RFC 5545 implementation - it covers the
// cases real calendars overwhelmingly use (FREQ DAILY/WEEKLY/MONTHLY/YEARLY with
// INTERVAL, COUNT, UNTIL, and weekly BYDAY) and clamps occurrence generation to
// the visible window so a "forever" rule can't run away.
package main

import (
	"strconv"
	"strings"
	"time"
)

type rrule struct {
	freq     string
	interval int
	count    int       // 0 = unbounded
	until    time.Time // zero = none
	byday    []time.Weekday
}

func parseRRULE(s string, loc *time.Location) rrule {
	r := rrule{interval: 1}
	for _, part := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch strings.ToUpper(k) {
		case "FREQ":
			r.freq = strings.ToUpper(v)
		case "INTERVAL":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				r.interval = n
			}
		case "COUNT":
			r.count, _ = strconv.Atoi(v)
		case "UNTIL":
			if t, _, ok := parseICSTime(v, nil, loc); ok {
				r.until = t
			}
		case "BYDAY":
			for _, d := range strings.Split(v, ",") {
				if wd, ok := weekdayCode(d); ok {
					r.byday = append(r.byday, wd)
				}
			}
		}
	}
	return r
}

// weekdayCode maps an ICS day code (optionally prefixed with an ordinal, e.g.
// "2MO") to a weekday. We ignore the ordinal (good enough for weekly rules).
func weekdayCode(s string) (time.Weekday, bool) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) < 2 {
		return 0, false
	}
	switch s[len(s)-2:] {
	case "SU":
		return time.Sunday, true
	case "MO":
		return time.Monday, true
	case "TU":
		return time.Tuesday, true
	case "WE":
		return time.Wednesday, true
	case "TH":
		return time.Thursday, true
	case "FR":
		return time.Friday, true
	case "SA":
		return time.Saturday, true
	}
	return 0, false
}

// expandRecurrence returns the event's occurrences overlapping [from,to). A
// non-recurring event yields itself (if it overlaps); a recurring one is walked
// from its DTSTART until the window/UNTIL/COUNT bound.
func expandRecurrence(base Event, from, to time.Time) []Event {
	if base.rrule == "" {
		if base.End.After(from) && base.Start.Before(to) {
			return []Event{base}
		}
		return nil
	}
	r := parseRRULE(base.rrule, base.Start.Location())
	if r.freq == "" {
		if base.End.After(from) && base.Start.Before(to) {
			return []Event{base}
		}
		return nil
	}
	dur := base.End.Sub(base.Start)
	var out []Event
	count := 0
	cur := base.Start
	for guard := 0; cur.Before(to) && guard < 5000; guard++ {
		occs := []time.Time{cur}
		if r.freq == "WEEKLY" && len(r.byday) > 0 {
			occs = weekOccurrences(cur, base.Start, r.byday)
		}
		for _, oc := range occs {
			if oc.Before(base.Start) {
				continue // BYDAY can list days before DTSTART in the first week
			}
			if !r.until.IsZero() && oc.After(r.until) {
				return out
			}
			if r.count > 0 && count >= r.count {
				return out
			}
			count++
			if oc.Add(dur).After(from) && oc.Before(to) {
				e := base
				e.Start, e.End, e.rrule = oc, oc.Add(dur), ""
				out = append(out, e)
			}
		}
		switch r.freq {
		case "DAILY":
			cur = cur.AddDate(0, 0, r.interval)
		case "WEEKLY":
			cur = cur.AddDate(0, 0, 7*r.interval)
		case "MONTHLY":
			cur = cur.AddDate(0, r.interval, 0)
		case "YEARLY":
			cur = cur.AddDate(r.interval, 0, 0)
		default:
			return out
		}
	}
	return out
}

// weekOccurrences returns, for the week containing cur, the days matching byday,
// each carrying the start event's time-of-day.
func weekOccurrences(cur, start time.Time, byday []time.Weekday) []time.Time {
	sun := cur.AddDate(0, 0, -int(cur.Weekday())) // back up to Sunday of cur's week
	var out []time.Time
	for i := 0; i < 7; i++ {
		day := sun.AddDate(0, 0, i)
		for _, wd := range byday {
			if day.Weekday() == wd {
				out = append(out, time.Date(day.Year(), day.Month(), day.Day(),
					start.Hour(), start.Minute(), start.Second(), 0, start.Location()))
			}
		}
	}
	return out
}
