// Config: an ini next to the binary (calendar.ini) listing calendar sources and
// a few display settings. Sysops point the door at public iCalendar (.ics) feed
// URLs - e.g. a Google Calendar's "secret address in iCal format", an iCloud
// public-share webcal link, or any Outlook/ICS export - and give each a color.
package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// iniBool parses on/true/yes/1 as true (default true for an empty/odd value).
func iniBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off", "false", "no", "0":
		return false
	}
	return true
}

// Calendar is one configured ICS source.
type Calendar struct {
	Name     string
	URL      string
	Color    string // a palette name; auto-assigned by order if blank
	Personal bool   // true = a caller's own feed (from prefs), not a system calendar
}

type config struct {
	Calendars []Calendar
	WeekStart    time.Weekday   // first column of the month/week grid (default Sunday)
	Loc          *time.Location // display timezone (default system local)
	Title        string         // banner title (default "Calendar")
	Host         string         // this BBS's own telnet host[:port]; events pointing here show "HERE", not JOIN
	ClockFont    string         // TheDraw font name for the splash clock (default "computrx")
	RefreshEvery time.Duration  // auto re-fetch interval (0 = manual only; default 15m)
	TelnetGate   bool           // allow telnet:// hops on NOW events (default true)
	Theme        theme          // colors (defaults + [colors] overrides)
}

// iniPath looks for calendar.ini next to the binary (then the working dir),
// falling back to the legacy "calender" spelling so existing installs keep
// working without renaming their config.
func iniPath() string {
	dir := "."
	if exe, err := os.Executable(); err == nil {
		dir = filepath.Dir(exe)
	}
	for _, name := range []string{"calendar.ini", "calender.ini"} {
		if p := filepath.Join(dir, name); fileExists(p) {
			return p
		}
		if fileExists(name) {
			return name
		}
	}
	return filepath.Join(dir, "calendar.ini")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// loadConfig parses the ini. Repeated [calendar] sections each add a source.
// [settings] holds week_start / timezone / title. Missing file -> sensible
// defaults with no calendars (the UI shows a "configure me" hint).
func loadConfig(path string) config {
	cfg := config{WeekStart: time.Sunday, Loc: time.Local, Title: "CalenDoor", RefreshEvery: 15 * time.Minute, TelnetGate: true, ClockFont: "computrx", Theme: defaultTheme()}
	f, err := os.Open(path)
	if err != nil {
		logf("config: %v (using defaults, no calendars)", err)
		return cfg
	}
	defer f.Close()

	section := ""
	var cur *Calendar
	flush := func() {
		if cur != nil && cur.URL != "" {
			if cur.Name == "" {
				cur.Name = "Calendar"
			}
			cfg.Calendars = append(cfg.Calendars, *cur)
		}
		cur = nil
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			section = strings.ToLower(strings.Trim(line, "[]"))
			if section == "calendar" {
				cur = &Calendar{}
			}
			continue
		}
		k, v, ok := splitKV(line)
		if !ok {
			continue
		}
		switch section {
		case "calendar":
			if cur == nil {
				cur = &Calendar{}
			}
			switch k {
			case "name":
				cur.Name = v
			case "url":
				cur.URL = v
			case "color":
				cur.Color = strings.ToLower(v)
			}
		case "settings", "":
			switch k {
			case "week_start":
				if strings.HasPrefix(strings.ToLower(v), "mon") {
					cfg.WeekStart = time.Monday
				}
			case "timezone", "tz":
				if loc, err := time.LoadLocation(v); err == nil {
					cfg.Loc = loc
				} else {
					logf("config: bad timezone %q: %v", v, err)
				}
			case "title":
				cfg.Title = v
			case "host", "this_host":
				cfg.Host = v
			case "clock_font":
				cfg.ClockFont = strings.ToLower(strings.TrimSpace(v))
			case "refresh_minutes":
				if n, err := strconv.Atoi(v); err == nil {
					if n > 0 {
						cfg.RefreshEvery = time.Duration(n) * time.Minute
					} else {
						cfg.RefreshEvery = 0 // disable auto-refresh
					}
				}
			case "telnet_gate":
				cfg.TelnetGate = iniBool(v)
			}
		case "colors":
			switch k {
			case "weekday":
				cfg.Theme.weekday = themeColor(v, cfg.Theme.weekday)
			case "day":
				cfg.Theme.day = themeColor(v, cfg.Theme.day)
			case "dim":
				cfg.Theme.dim = themeColor(v, cfg.Theme.dim)
			case "title":
				cfg.Theme.title = themeColor(v, cfg.Theme.title)
			case "border":
				cfg.Theme.border = themeColor(v, cfg.Theme.border)
			case "today":
				cfg.Theme.today = "1;30;" + bgOf(themeColor(v, "33"))
			case "hint_key", "hintkey":
				cfg.Theme.hintKey = themeColor(v, cfg.Theme.hintKey)
			case "hint_text", "hinttext":
				cfg.Theme.hintText = themeColor(v, cfg.Theme.hintText)
			case "clock":
				cfg.Theme.clock = strings.ToLower(strings.TrimSpace(v))
			case "months", "month_colors":
				parts := strings.Split(v, ",")
				for i := 0; i < 12 && i < len(parts); i++ {
					if c := themeColor(parts[i], ""); c != "" {
						cfg.Theme.months[i] = c
					}
				}
			}
		}
	}
	flush()

	// Auto-assign palette colors to any calendar that didn't specify one.
	for i := range cfg.Calendars {
		if cfg.Calendars[i].Color == "" {
			cfg.Calendars[i].Color = paletteOrder[i%len(paletteOrder)]
		}
	}
	logf("config: %d calendar(s), tz=%s, weekStart=%v", len(cfg.Calendars), cfg.Loc, cfg.WeekStart)
	return cfg
}

func splitKV(line string) (k, v string, ok bool) {
	i := strings.IndexAny(line, "=:")
	if i < 0 {
		return "", "", false
	}
	return strings.ToLower(strings.TrimSpace(line[:i])), strings.TrimSpace(line[i+1:]), true
}
