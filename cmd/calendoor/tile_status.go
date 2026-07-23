package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Intentionally omits summaries, locations, and URLs. The shell needs only
// event intervals, and a caller's private feed details never belong in a tile
// cache that another process can inspect.
type tileStatusEvent struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

type tileStatus struct {
	GeneratedAt int64             `json:"generated_at"`
	Month       string            `json:"month"`
	Day         int               `json:"day"`
	DateUntil   int64             `json:"date_until"`
	Events      []tileStatusEvent `json:"events"`
	Errors      int               `json:"errors,omitempty"`
}

// writeTileStatus fetches the same shared + per-caller feeds as the door and
// atomically publishes a small status document for fshell's dynamic root tile.
func writeTileStatus(path, handle string) error {
	cfg := loadConfig(iniPath())
	prefs := loadPrefs()
	if strings.TrimSpace(handle) == "" {
		handle = "guest"
	}
	a := &app{cfg: cfg, sysCals: append([]Calendar(nil), cfg.Calendars...), calFilter: -1}
	a.rebuildCalendars(prefs, handle)
	if zone, ok := prefs.TZ[handle]; ok {
		a.applyTZ(zone)
	}
	now := time.Now().In(a.cfg.Loc)
	from := dayOf(now, a.cfg.Loc).AddDate(0, 0, -1)
	to := from.AddDate(0, 14, 0)
	events, errs := loadCalendars(a.cfg, from, to)
	date := dayOf(now, a.cfg.Loc)
	nextMidnight := date.AddDate(0, 0, 1)
	status := tileStatus{
		GeneratedAt: time.Now().Unix(),
		Month:       strings.ToUpper(now.Format("Jan")),
		Day:         now.Day(),
		DateUntil:   nextMidnight.Unix(),
		Events:      make([]tileStatusEvent, 0, len(events)),
		Errors:      len(errs),
	}
	for _, event := range events {
		if !event.End.After(now.Add(-24 * time.Hour)) {
			continue
		}
		status.Events = append(status.Events, tileStatusEvent{Start: event.Start.Unix(), End: event.End.Unix()})
		if len(status.Events) >= 512 {
			break
		}
	}
	return writeTileStatusAtomic(path, status)
}

func writeTileStatusAtomic(path string, status tileStatus) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tile-status-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		tmp.Close()
		if !ok {
			os.Remove(tmpPath)
		}
	}()
	enc := json.NewEncoder(tmp)
	if err := enc.Encode(status); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename status: %w", err)
	}
	ok = true
	return nil
}
