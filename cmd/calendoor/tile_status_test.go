package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteTileStatusAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "tile.json")
	want := tileStatus{
		GeneratedAt: 100,
		Month:       "JUL",
		Day:         12,
		DateUntil:   200,
		Events:      []tileStatusEvent{{Start: 110, End: 120}},
	}
	if err := writeTileStatusAtomic(path, want); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got tileStatus
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Month != "JUL" || got.Day != 12 || len(got.Events) != 1 || got.Events[0].Start != 110 {
		t.Fatalf("unexpected status: %+v", got)
	}
}
