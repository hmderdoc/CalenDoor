// iCalendar (RFC 5545) fetch + parse. We read public .ics feeds over HTTP and
// turn VEVENTs into normalized Events: line unfolding, DATE vs DATE-TIME,
// UTC/TZID/floating times, DURATION, TEXT unescaping, and a pragmatic RRULE
// expander for the common recurring cases (DAILY/WEEKLY/MONTHLY/YEARLY with
// INTERVAL/COUNT/UNTIL/BYDAY). Read-only: we never write back to the source.
package main

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// loadCalendars fetches and parses every configured calendar, expanding
// recurrences into the [from,to) window. Returns the merged events plus any
// per-calendar fetch/parse errors (so the UI can surface "couldn't load X").
func loadCalendars(cfg config, from, to time.Time) ([]Event, []string) {
	var all []Event
	var errs []string
	client := &http.Client{Timeout: 20 * time.Second}
	for i, cal := range cfg.Calendars {
		data, err := fetchICS(client, cal.URL)
		if err != nil {
			logf("calendar %q fetch failed: %v", cal.Name, err)
			errs = append(errs, cal.Name+": "+err.Error())
			continue
		}
		base := parseICS(data, i, cfg.Loc)
		var n int
		for _, e := range base {
			occ := expandRecurrence(e, from, to)
			all = append(all, occ...)
			n += len(occ)
		}
		logf("calendar %q: %d raw events -> %d occurrences in window", cal.Name, len(base), n)
	}
	sortEvents(all)
	return all, errs
}

func fetchICS(client *http.Client, url string) ([]byte, error) {
	// webcal:// is just ICS over HTTP(S); normalize it so net/http accepts it.
	if strings.HasPrefix(url, "webcal://") {
		url = "https://" + strings.TrimPrefix(url, "webcal://")
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "BBS-Calendar/0.1 (+door)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, &httpError{resp.StatusCode}
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16MB cap
}

type httpError struct{ code int }

func (e *httpError) Error() string { return "HTTP " + strconv.Itoa(e.code) }

// parseICS unfolds and walks the feed, emitting one Event per VEVENT (the
// master instance; recurrences are expanded later).
func parseICS(data []byte, calIdx int, loc *time.Location) []Event {
	lines := unfold(string(data))
	var out []Event
	var cur *Event
	var dtEnd time.Time
	var hasEnd bool
	var dur time.Duration
	var hasDur bool
	for _, ln := range lines {
		name, params, val := splitProp(ln)
		switch name {
		case "BEGIN":
			if strings.EqualFold(val, "VEVENT") {
				cur = &Event{CalIdx: calIdx}
				dtEnd, hasEnd, dur, hasDur = time.Time{}, false, 0, false
			}
		case "END":
			if strings.EqualFold(val, "VEVENT") && cur != nil {
				finishEvent(cur, dtEnd, hasEnd, dur, hasDur)
				if !cur.Start.IsZero() {
					out = append(out, *cur)
				}
				cur = nil
			}
		default:
			if cur == nil {
				continue
			}
			switch name {
			case "DTSTART":
				t, allDay, ok := parseICSTime(val, params, loc)
				if ok {
					cur.Start, cur.AllDay = t, allDay
				}
			case "DTEND":
				if t, _, ok := parseICSTime(val, params, loc); ok {
					dtEnd, hasEnd = t, true
				}
			case "DURATION":
				if d, ok := parseICSDuration(val); ok {
					dur, hasDur = d, true
				}
			case "SUMMARY":
				cur.Summary = toCP437Line(unescapeText(val))
			case "LOCATION":
				cur.Location = toCP437Line(unescapeText(val))
			case "DESCRIPTION":
				cur.Desc = toCP437(unescapeText(val))
			case "UID":
				cur.UID = val
			case "RRULE":
				cur.rrule = val
			}
		}
	}
	return out
}

// finishEvent fills End from DTEND/DURATION/defaults so End is always > Start.
func finishEvent(e *Event, dtEnd time.Time, hasEnd bool, dur time.Duration, hasDur bool) {
	switch {
	case hasEnd:
		e.End = dtEnd
	case hasDur:
		e.End = e.Start.Add(dur)
	case e.AllDay:
		e.End = e.Start.AddDate(0, 0, 1)
	default:
		e.End = e.Start.Add(time.Hour)
	}
	if !e.End.After(e.Start) {
		if e.AllDay {
			e.End = e.Start.AddDate(0, 0, 1)
		} else {
			e.End = e.Start.Add(time.Hour)
		}
	}
}

// unfold splits on line breaks and joins RFC5545 continuation lines (a line
// starting with a space or tab continues the previous one).
func unfold(s string) []string {
	raw := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	var out []string
	for _, ln := range raw {
		ln = strings.TrimRight(ln, "\r")
		if ln == "" {
			continue
		}
		if (ln[0] == ' ' || ln[0] == '\t') && len(out) > 0 {
			out[len(out)-1] += ln[1:]
			continue
		}
		out = append(out, ln)
	}
	return out
}

// splitProp parses NAME(;PARAM=VAL)*:VALUE into (name, params, value).
func splitProp(line string) (name string, params map[string]string, value string) {
	colon := -1
	inQuote := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQuote = !inQuote
		case ':':
			if !inQuote {
				colon = i
			}
		}
		if colon >= 0 {
			break
		}
	}
	if colon < 0 {
		return strings.ToUpper(line), nil, ""
	}
	left, value := line[:colon], line[colon+1:]
	parts := strings.Split(left, ";")
	name = strings.ToUpper(parts[0])
	if len(parts) > 1 {
		params = map[string]string{}
		for _, p := range parts[1:] {
			if k, v, ok := strings.Cut(p, "="); ok {
				params[strings.ToUpper(k)] = strings.Trim(v, "\"")
			}
		}
	}
	return name, params, value
}

// parseICSTime parses a DATE or DATE-TIME value honoring VALUE/TZID params and a
// trailing Z (UTC). A floating time (no Z, no TZID) is interpreted in loc.
func parseICSTime(val string, params map[string]string, loc *time.Location) (time.Time, bool, bool) {
	val = strings.TrimSpace(val)
	if params["VALUE"] == "DATE" || (len(val) == 8 && !strings.Contains(val, "T")) {
		if t, err := time.ParseInLocation("20060102", val, loc); err == nil {
			return t, true, true
		}
		return time.Time{}, false, false
	}
	if strings.HasSuffix(val, "Z") {
		if t, err := time.ParseInLocation("20060102T150405Z", val, time.UTC); err == nil {
			return t.In(loc), false, true
		}
	}
	zone := loc
	if tz := params["TZID"]; tz != "" {
		if z, err := time.LoadLocation(tz); err == nil {
			zone = z
		}
	}
	if t, err := time.ParseInLocation("20060102T150405", val, zone); err == nil {
		return t.In(loc), false, true
	}
	return time.Time{}, false, false
}

// parseICSDuration handles the common ISO-8601 forms: PnDTnHnMnS / PTnHnMnS / PnW.
func parseICSDuration(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(strings.TrimPrefix(s, "-"), "+")
	if !strings.HasPrefix(s, "P") {
		return 0, false
	}
	s = s[1:]
	var d time.Duration
	inTime := false
	num := ""
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			num += string(r)
		case r == 'T':
			inTime = true
			num = ""
		default:
			n, _ := strconv.Atoi(num)
			num = ""
			switch r {
			case 'W':
				d += time.Duration(n) * 7 * 24 * time.Hour
			case 'D':
				d += time.Duration(n) * 24 * time.Hour
			case 'H':
				d += time.Duration(n) * time.Hour
			case 'M':
				if inTime {
					d += time.Duration(n) * time.Minute
				}
			case 'S':
				d += time.Duration(n) * time.Second
			}
		}
	}
	if neg {
		d = -d
	}
	return d, d != 0
}

func unescapeText(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`)
	return r.Replace(s)
}
