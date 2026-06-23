package main

import (
	"bufio"
	"bytes"
	"regexp"
	"testing"
	"time"
)

const sampleICS = "BEGIN:VCALENDAR\r\n" +
	"VERSION:2.0\r\n" +
	"BEGIN:VEVENT\r\nUID:1\r\nSUMMARY:All Day Thing\r\n" +
	"DTSTART;VALUE=DATE:20260115\r\nDTEND;VALUE=DATE:20260116\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:2\r\nSUMMARY:Timed UTC\r\n" +
	"DTSTART:20260115T150000Z\r\nDTEND:20260115T160000Z\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:3\r\nSUMMARY:Weekly Standup\r\n" +
	"DTSTART;TZID=America/New_York:20260105T090000\r\n" +
	"DTEND;TZID=America/New_York:20260105T093000\r\n" +
	"RRULE:FREQ=WEEKLY;BYDAY=MO;COUNT=4\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestParseICS(t *testing.T) {
	evs := parseICS([]byte(sampleICS), 0, time.UTC)
	if len(evs) != 3 {
		t.Fatalf("want 3 events, got %d", len(evs))
	}
	if !evs[0].AllDay || evs[0].Summary != "All Day Thing" {
		t.Errorf("event 0: %+v", evs[0])
	}
	if y, m, d := evs[0].Start.Date(); y != 2026 || m != time.January || d != 15 {
		t.Errorf("all-day start wrong: %v", evs[0].Start)
	}
	if evs[1].AllDay || evs[1].Start.Hour() != 15 { // 15:00Z
		t.Errorf("timed UTC wrong: %v", evs[1].Start)
	}
	if evs[2].rrule == "" || evs[2].Start.Hour() != 14 { // 09:00 EST -> 14:00Z
		t.Errorf("TZID conversion wrong: %v rrule=%q", evs[2].Start, evs[2].rrule)
	}
}

func TestRecurrenceExpansion(t *testing.T) {
	evs := parseICS([]byte(sampleICS), 0, time.UTC)
	var standup Event
	for _, e := range evs {
		if e.Summary == "Weekly Standup" {
			standup = e
		}
	}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	occ := expandRecurrence(standup, from, to)
	if len(occ) != 4 { // COUNT=4: Jan 5,12,19,26
		t.Fatalf("want 4 occurrences, got %d: %v", len(occ), occ)
	}
	wantDays := []int{5, 12, 19, 26}
	for i, e := range occ {
		if e.Start.Day() != wantDays[i] || e.Start.Weekday() != time.Monday {
			t.Errorf("occ %d on %v, want Jan %d (Mon)", i, e.Start, wantDays[i])
		}
	}
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func strip(b []byte) string { return ansiRE.ReplaceAllString(string(b), "") }

func renderToString(t *testing.T, fn func(*bufio.Writer)) string {
	t.Helper()
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	fn(w)
	w.Flush()
	return strip(buf.Bytes())
}

func TestRenderSmoke(t *testing.T) {
	cfg := config{
		Calendars: []Calendar{{Name: "BBS", URL: "x", Color: "cyan"}},
		WeekStart: time.Sunday, Loc: time.UTC, Title: "CalenDoor", Theme: defaultTheme(),
	}
	base := parseICS([]byte(sampleICS), 0, time.UTC)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	var events []Event
	for _, e := range base {
		events = append(events, expandRecurrence(e, from, to)...)
	}
	focus := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	today := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)

	month := renderToString(t, func(w *bufio.Writer) { renderMonth(w, 80, 25, cfg, events, focus, today) })
	if !contains(month, "January 2026") || !contains(month, "All Day") {
		t.Errorf("month view missing expected content:\n%s", month)
	}
	day := renderToString(t, func(w *bufio.Writer) { renderDay(w, 80, 25, cfg, events, focus, today) })
	if !contains(day, "All Day Thing") || !contains(day, "Timed UTC") {
		t.Errorf("day view missing events:\n%s", day)
	}
	up := renderToString(t, func(w *bufio.Writer) { renderUpcoming(w, 80, 25, cfg, events, today, 0) })
	if !contains(up, "Upcoming") || !contains(up, "Standup") {
		t.Errorf("upcoming view missing events:\n%s", up)
	}
	week := renderToString(t, func(w *bufio.Writer) { renderWeek(w, 80, 25, cfg, events, focus, today) })
	if !contains(week, "2026") {
		t.Errorf("week view missing header:\n%s", week)
	}
}

func contains(haystack, needle string) bool { return bytes.Contains([]byte(haystack), []byte(needle)) }

func TestPersonalCalendarMerge(t *testing.T) {
	a := &app{cfg: config{Loc: time.UTC}, sysCals: []Calendar{{Name: "BBS", Color: "cyan"}}}
	prefs := userPrefs{Cals: map[string][]userCal{
		"bob": {{Name: "Mine", URL: "https://x/y.ics", Color: "green"}},
	}}
	a.rebuildCalendars(prefs, "bob")
	if len(a.cfg.Calendars) != 2 {
		t.Fatalf("want 2 calendars, got %d", len(a.cfg.Calendars))
	}
	if !a.cfg.Calendars[1].Personal || a.cfg.Calendars[1].Name != "Mine" {
		t.Errorf("merged personal cal wrong: %+v", a.cfg.Calendars[1])
	}
	if a.personalCount() != 1 {
		t.Errorf("personalCount=%d want 1", a.personalCount())
	}
	a.rebuildCalendars(prefs, "alice") // a caller with no personal feeds
	if len(a.cfg.Calendars) != 1 || a.personalCount() != 0 {
		t.Errorf("alice: cals=%d personal=%d want 1/0", len(a.cfg.Calendars), a.personalCount())
	}
}

func TestWebcalURL(t *testing.T) {
	if got := webcalURL("https://calendar.google.com/x.ics"); got != "webcal://calendar.google.com/x.ics" {
		t.Errorf("webcalURL https = %q", got)
	}
	if got := webcalURL("webcal://already"); got != "webcal://already" {
		t.Errorf("webcalURL passthrough = %q", got)
	}
}

func TestTelnetTarget(t *testing.T) {
	ok := map[string]string{
		"telnet://bbs.example.com":      "bbs.example.com:23",
		"telnet://bbs.example.com:2323": "bbs.example.com:2323",
		"TELNET://x.com":                "x.com:23",
		"telnet://x.com/path":           "x.com:23",
	}
	for in, want := range ok {
		if got, good := telnetTarget(in); !good || got != want {
			t.Errorf("telnetTarget(%q)=%q,%v want %q", in, got, good, want)
		}
	}
	for _, bad := range []string{"https://x.com", "x.com", "", "ssh://x"} {
		if _, good := telnetTarget(bad); good {
			t.Errorf("telnetTarget(%q) should be not-ok", bad)
		}
	}
}

func TestJoinTargetFor(t *testing.T) {
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	live := Event{Start: now.Add(-10 * time.Minute), End: now.Add(10 * time.Minute), Location: "telnet://bbs.com", Summary: "Live"}
	if h, _, ok := joinTargetFor(live, now); !ok || h != "bbs.com:23" {
		t.Errorf("NOW telnet event should be joinable, got %q %v", h, ok)
	}
	future := Event{Start: now.Add(time.Hour), End: now.Add(2 * time.Hour), Location: "telnet://bbs.com"}
	if _, _, ok := joinTargetFor(future, now); ok {
		t.Error("future event should not be joinable")
	}
	noTel := Event{Start: now.Add(-time.Minute), End: now.Add(time.Minute), Location: "https://x"}
	if _, _, ok := joinTargetFor(noTel, now); ok {
		t.Error("non-telnet NOW event should not be joinable")
	}
}
