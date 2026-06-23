// Per-caller preferences (currently just the display timezone), keyed by the
// BBS handle from DOOR32.SYS and persisted in data/prefs.json next to the binary.
package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// door32Handle reads the caller's handle/alias from DOOR32.SYS. The standard
// layout: line 6 = real name, line 7 = handle/alias (1-based).
func door32Handle(dropfile string) string {
	f, err := os.Open(dropfile)
	if err != nil {
		return ""
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, strings.TrimSpace(sc.Text()))
	}
	if len(lines) >= 7 && lines[6] != "" {
		return lines[6]
	}
	if len(lines) >= 6 && lines[5] != "" {
		return lines[5]
	}
	return ""
}

// userCal is a caller's own calendar feed (their private iCal link), shown only
// to them, overlaid on the shared system calendars.
type userCal struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Color string `json:"color"`
}

// userPrefs is the per-caller store: timezone + personal calendar feeds, each
// keyed by handle.
type userPrefs struct {
	TZ   map[string]string    `json:"tz"`
	Cals map[string][]userCal `json:"cals"`
}

func prefsPath() string {
	dir := "."
	if exe, err := os.Executable(); err == nil {
		dir = filepath.Dir(exe)
	}
	return filepath.Join(dir, "data", "prefs.json")
}

func loadPrefs() userPrefs {
	p := userPrefs{}
	if data, err := os.ReadFile(prefsPath()); err == nil {
		json.Unmarshal(data, &p)
	}
	if p.TZ == nil {
		p.TZ = map[string]string{}
	}
	if p.Cals == nil {
		p.Cals = map[string][]userCal{}
	}
	return p
}

func (p userPrefs) save() {
	path := prefsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logf("prefs: mkdir: %v", err)
		return
	}
	if data, err := json.MarshalIndent(p, "", "  "); err == nil {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			logf("prefs: write: %v", err)
		}
	}
}
