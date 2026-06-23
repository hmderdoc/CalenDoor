// Calendar - a BBS door (Synchronet/DOOR32) that renders shared iCalendar feeds:
// monthly / weekly / daily / upcoming views, color-coded per calendar, read from
// public .ics URLs (Google, iCloud, Outlook, ...). Read-only: sysops create
// events in their calendar service; the door subscribes and displays them.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
	_ "time/tzdata" // embed the IANA zone database so LoadLocation works anywhere
)

// version is stamped at release time via -ldflags "-X main.version=vX.Y.Z";
// "dev" for local/source builds.
var version = "dev"

type viewMode int

const (
	viewMonth viewMode = iota
	viewWeek
	viewDay
	viewUpcoming
)

type app struct {
	cfg      config
	sysCals  []Calendar // system calendars (from the ini); cfg.Calendars = these + the caller's personal feeds
	events   []Event
	loadErrs []string
	view     viewMode
	focus    time.Time // selected day
	today    time.Time
	winFrom  time.Time
	winTo    time.Time
	upSel    int  // selected row in the upcoming list
	calFilter int // -1 = show all calendars; else index into cfg.Calendars (cycled with C)
	gen      int  // bumped on every (re)load; async results from an older gen are discarded
	loading  bool // a background refresh is in flight
}

// visibleEvents applies the calendar filter (C cycles through them): all events
// when calFilter is -1, otherwise just the selected calendar's.
func (a *app) visibleEvents() []Event {
	if a.calFilter < 0 || a.calFilter >= len(a.cfg.Calendars) {
		return a.events
	}
	out := make([]Event, 0, len(a.events))
	for _, e := range a.events {
		if e.CalIdx == a.calFilter {
			out = append(out, e)
		}
	}
	return out
}

// cycleCalFilter steps the filter All -> cal 0 -> cal 1 -> ... -> All. No-op
// (stays on All) when there's at most one calendar to filter.
func (a *app) cycleCalFilter() {
	a.upSel = 0
	if len(a.cfg.Calendars) <= 1 {
		a.calFilter = -1
		return
	}
	a.calFilter++
	if a.calFilter >= len(a.cfg.Calendars) {
		a.calFilter = -1
	}
}

// drawFilterChip shows the active calendar filter at the top-left (a colored
// swatch + name, or "All calendars"). Hidden when there's nothing to filter.
func (a *app) drawFilterChip(w *bufio.Writer) {
	if len(a.cfg.Calendars) <= 1 {
		return
	}
	if a.calFilter < 0 || a.calFilter >= len(a.cfg.Calendars) {
		at(w, 1, 2, sgr("0;37", sqr+" All calendars"))
		return
	}
	c := a.cfg.Calendars[a.calFilter]
	at(w, 1, 2, sgr(sgrFor(c.Color), sqr+" ")+sgr("1;37", truncate(c.Name, 18)))
}

// rebuildCalendars merges the system calendars with the caller's personal feeds
// (from prefs) into cfg.Calendars, so loading + color-coding treat them uniformly.
func (a *app) rebuildCalendars(prefs userPrefs, handle string) {
	cals := append([]Calendar(nil), a.sysCals...)
	for _, uc := range prefs.Cals[handle] {
		color := uc.Color
		if color == "" {
			color = paletteOrder[len(cals)%len(paletteOrder)]
		}
		cals = append(cals, Calendar{Name: uc.Name, URL: uc.URL, Color: color, Personal: true})
	}
	a.cfg.Calendars = cals
}

// personalCount is how many of the merged calendars are the caller's own.
func (a *app) personalCount() int {
	n := 0
	for _, c := range a.cfg.Calendars {
		if c.Personal {
			n++
		}
	}
	return n
}

func main() {
	logf("=== calendar door start: args=%v pid=%d ===", os.Args, os.Getpid())

	dropfile := os.Getenv("CALENDAR_DROPFILE")
	if dropfile == "" {
		dropfile = "DOOR32.SYS"
	}
	for i := 1; i < len(os.Args); i++ {
		a := os.Args[i]
		switch {
		case a == "-dropfile" && i+1 < len(os.Args):
			i++
			dropfile = os.Args[i]
		case strings.HasPrefix(a, "-dropfile="):
			dropfile = a[len("-dropfile="):]
		}
	}

	term, restore, err := openTerm(dropfile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "calendar: cannot open terminal:", err)
		os.Exit(1)
	}
	defer term.Close()

	cols, rows := 80, 25
	if c, r, ok := localTermSize(); ok {
		cols, rows = c, r
	} else if c, r, ok := probeSize(term, 600*time.Millisecond); ok {
		cols, rows = c, r
	}
	if cols < 40 {
		cols = 80
	}
	if rows < 16 {
		rows = 24
	}
	logf("terminal %dx%d", cols, rows)

	restore()
	defer restore()
	w := bufio.NewWriterSize(term, 32*1024)
	hideCursor(w)
	w.WriteString("\x1b[?7l") // autowrap off: a full-width bottom row can't scroll the screen
	defer func() { showCursor(w); w.WriteString("\x1b[?7h"); resetAttr(w); w.Flush() }()

	cfg := loadConfig(iniPath())
	in := newInput(term)

	runSplash(w, in, cols, rows, cfg) // TheDraw-font title splash (random font each launch)

	handle := door32Handle(dropfile)
	if handle == "" {
		handle = "guest"
	}
	prefs := loadPrefs()

	a := &app{cfg: cfg, view: viewUpcoming, calFilter: -1, sysCals: append([]Calendar(nil), cfg.Calendars...)}
	a.today = dayOf(time.Now(), cfg.Loc)
	a.focus = a.today
	a.rebuildCalendars(prefs, handle) // fold in the caller's personal feeds

	// Per-caller timezone: returning callers get their saved zone; first-timers
	// pick one (events then render in their local time). The choice persists.
	if zone, ok := prefs.TZ[handle]; ok {
		a.applyTZ(zone)
	} else {
		zone := runTZPicker(w, in, cols, rows, a.cfg.Loc.String())
		if zone == "" {
			zone = a.cfg.Loc.String() // skipped: keep the server default, but remember it so we don't re-prompt
		}
		if a.applyTZ(zone) {
			prefs.TZ[handle] = zone
			prefs.save()
		}
	}

	// Initial load with a "please wait" splash (the fetch blocks on the network).
	a.splash(w, cols, rows, "Loading calendars...")
	a.reload()
	if e, ok := localNowEvent(a.events, a.nowT(), a.cfg); ok { // an event hosted here is live: greet them
		runWelcome(w, in, cols, rows, a.cfg, e)
	}
	a.render(w, cols, rows)

	// Background refresh: a worker fetches on demand and posts results back to the
	// loop, so the network never freezes input. gen discards a stale result that
	// lands after a newer (re)load.
	type loadResult struct {
		gen    int
		events []Event
		errs   []string
	}
	loadCh := make(chan loadResult, 1)
	startRefresh := func() {
		if a.loading {
			return
		}
		a.loading = true
		a.gen++
		g := a.gen
		a.today = dayOf(time.Now(), a.cfg.Loc)
		a.winFrom = a.today.AddDate(0, -2, 0)
		a.winTo = a.today.AddDate(0, 14, 0)
		cfg, from, to := a.cfg, a.winFrom, a.winTo
		go func() {
			evs, errs := loadCalendars(cfg, from, to)
			loadCh <- loadResult{g, evs, errs}
		}()
	}

	// uiTick keeps the "starts in" countdown (and the today highlight) current
	// without a keypress, repainting Upcoming in place (no clear = no flicker).
	uiTick := time.NewTicker(30 * time.Second)
	defer uiTick.Stop()
	// resizeTick asks the terminal for its size; the reader parses the reply and
	// posts it on resizeCh, so the calendar re-lays-out live when the window changes.
	resizeTick := time.NewTicker(1500 * time.Millisecond)
	defer resizeTick.Stop()
	var refreshC <-chan time.Time
	if a.cfg.RefreshEvery > 0 {
		rt := time.NewTicker(a.cfg.RefreshEvery)
		defer rt.Stop()
		refreshC = rt.C
	}

	for {
		select {
		case <-in.quitCh:
			logf("quit: connection closed")
			return
		case res := <-loadCh:
			a.loading = false
			if res.gen == a.gen { // still the latest request
				a.events, a.loadErrs = res.events, res.errs
			}
			a.render(w, cols, rows)
		case <-refreshC:
			startRefresh()
			a.render(w, cols, rows) // show the refreshing indicator
		case <-resizeTick.C:
			w.WriteString("\x1b[18t") // ask the terminal for its size (telnet-safe)
			w.Flush()
		case sz := <-in.resizeCh:
			if nc, nr := sz[0], sz[1]; nc >= 20 && nr >= 8 && (nc != cols || nr != rows) {
				cols, rows = nc, nr
				logf("resize: %dx%d", cols, rows)
				a.render(w, cols, rows)
			}
		case <-uiTick.C:
			a.today = dayOf(time.Now(), a.cfg.Loc)
			if a.view == viewUpcoming {
				renderUpcoming(w, cols, rows, a.cfg, a.visibleEvents(), a.nowT(), a.upSel)
				a.drawFilterChip(w)
				if a.loading {
					drawRefreshing(w, cols)
				}
				w.Flush()
			}
		case k := <-in.events:
			if k > 0 && toLower(rune(k)) == 's' { // settings screen
				runSettings(w, in, cols, rows, a, handle, prefs)
				a.render(w, cols, rows)
				continue
			}
			if k > 0 && toLower(rune(k)) == 'r' { // manual refresh (background)
				startRefresh()
				a.render(w, cols, rows)
				continue
			}
			if k == keyEnter && a.view == viewUpcoming { // hop to a BBS that's live NOW
				if host, label, ok := a.joinTarget(a.nowT()); ok {
					runTelnetGate(w, in, term, cols, rows, host, label)
					a.render(w, cols, rows)
					continue
				}
			}
			if a.handleKey(k, &cols, &rows, term) {
				logf("quit: user")
				return
			}
			a.render(w, cols, rows)
		}
	}
}

// applyTZ switches the display timezone, re-anchoring today/focus to it. The
// caller persists the choice and triggers a reload (all-day parsing is zone
// dependent, so events must be re-read for the new zone).
func (a *app) applyTZ(zone string) bool {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		logf("timezone %q: %v", zone, err)
		return false
	}
	a.cfg.Loc = loc
	a.today = dayOf(time.Now(), loc)
	a.focus = a.today
	return true
}

// reload synchronously (re)fetches all calendars into a wide window around today.
// Used on entry and after a config change (add/remove calendar, timezone). Bumps
// gen so any in-flight background refresh is discarded when it returns.
func (a *app) reload() {
	a.gen++
	a.winFrom = a.today.AddDate(0, -2, 0)
	a.winTo = a.today.AddDate(0, 14, 0)
	a.events, a.loadErrs = loadCalendars(a.cfg, a.winFrom, a.winTo)
}

func (a *app) splash(w *bufio.Writer, cols, rows int, msg string) {
	cls(w)
	at(w, rows/2, (cols-len(msg))/2+1, sgr("1;36", msg))
	w.Flush()
}

func (a *app) render(w *bufio.Writer, cols, rows int) {
	cls(w)
	evs := a.visibleEvents()
	switch a.view {
	case viewMonth:
		renderMonth(w, cols, rows, a.cfg, evs, a.focus, a.today)
	case viewWeek:
		renderWeek(w, cols, rows, a.cfg, evs, a.focus, a.today)
	case viewDay:
		renderDay(w, cols, rows, a.cfg, evs, a.focus, a.today)
	case viewUpcoming:
		renderUpcoming(w, cols, rows, a.cfg, evs, a.nowT(), a.upSel)
	}
	a.drawFilterChip(w)
	if len(a.loadErrs) > 0 { // a non-fatal banner if a feed failed
		msg := truncate("! "+strings.Join(a.loadErrs, "; "), cols-2)
		at(w, rows-1, 2, sgr("1;31", msg))
	}
	if a.loading {
		drawRefreshing(w, cols)
	}
	w.Flush()
}

// drawRefreshing overlays a small indicator on the title bar during a background refresh.
func drawRefreshing(w *bufio.Writer, cols int) {
	at(w, 1, cols-11, sgr("1;93;46", " refresh "))
}

// handleKey applies a keypress; returns true to quit.
func (a *app) handleKey(k key, cols, rows *int, term Term) bool {
	// Letter shortcuts (case-insensitive).
	if k > 0 {
		switch toLower(rune(k)) {
		case 'q':
			return true
		case 'm':
			a.view = viewMonth
			return false
		case 'w':
			a.view = viewWeek
			return false
		case 'd':
			a.view = viewDay
			return false
		case 'u':
			a.view = viewUpcoming
			a.upSel = 0
			return false
		case 't':
			a.focus = a.today
			return false
		case 'c':
			a.cycleCalFilter()
			return false
		// 'r' (refresh) is handled in the main loop so it can run in the background
		}
	}

	switch a.view {
	case viewUpcoming:
		list := upcomingEvents(a.visibleEvents(), a.nowT())
		switch k {
		case keyUp:
			if a.upSel > 0 {
				a.upSel--
			}
		case keyDown:
			if a.upSel < len(list)-1 {
				a.upSel++
			}
		case keyEnter:
			if a.upSel < len(list) {
				a.focus = dayOf(list[a.upSel].Start, a.cfg.Loc)
				a.view = viewDay
			}
		case keyEsc, keyBack:
			a.view = viewMonth
		}
	case viewDay:
		switch k {
		case keyLeft, keyUp, keyPgUp:
			a.focus = a.focus.AddDate(0, 0, -1)
		case keyRight, keyDown, keyPgDn:
			a.focus = a.focus.AddDate(0, 0, 1)
		case keyEsc, keyBack:
			a.view = viewMonth
		}
	case viewWeek:
		switch k {
		case keyLeft:
			a.focus = a.focus.AddDate(0, 0, -1)
		case keyRight:
			a.focus = a.focus.AddDate(0, 0, 1)
		case keyUp, keyPgUp:
			a.focus = a.focus.AddDate(0, 0, -7)
		case keyDown, keyPgDn:
			a.focus = a.focus.AddDate(0, 0, 7)
		case keyEnter:
			a.view = viewDay
		case keyEsc, keyBack:
			a.view = viewMonth
		}
	default: // month
		switch k {
		case keyLeft:
			a.focus = a.focus.AddDate(0, 0, -1)
		case keyRight:
			a.focus = a.focus.AddDate(0, 0, 1)
		case keyUp:
			a.focus = a.focus.AddDate(0, 0, -7)
		case keyDown:
			a.focus = a.focus.AddDate(0, 0, 7)
		case keyPgUp:
			a.focus = a.focus.AddDate(0, -1, 0)
		case keyPgDn:
			a.focus = a.focus.AddDate(0, 1, 0)
		case keyEnter:
			a.view = viewDay
		case keyEsc:
			return true
		}
	}
	return false
}

func toLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	return r
}

// nowT is the current instant in the caller's display timezone.
func (a *app) nowT() time.Time { return time.Now().In(a.cfg.Loc) }

// joinTarget returns the telnet dial address + label if the currently-selected
// Upcoming event is happening NOW and has a telnet:// location (and the gateway
// is enabled).
func (a *app) joinTarget(now time.Time) (host, label string, ok bool) {
	if !a.cfg.TelnetGate {
		return "", "", false
	}
	list := upcomingEvents(a.visibleEvents(), now)
	if a.upSel < 0 || a.upSel >= len(list) {
		return "", "", false
	}
	h, l, ok := joinTargetFor(list[a.upSel], now)
	if !ok || isLocalHost(h, a.cfg) { // never "hop" to the board we're already on
		return "", "", false
	}
	return h, l, true
}

func dayOf(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}
