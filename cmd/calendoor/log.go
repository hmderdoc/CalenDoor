// Minimal logging to calendar.log next to the binary (the door has no console).
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	logMu   sync.Mutex
	logFile *os.File
	logOnce sync.Once
)

func logf(format string, a ...any) {
	logOnce.Do(func() {
		dir := "."
		if exe, err := os.Executable(); err == nil {
			dir = filepath.Dir(exe)
		}
		logFile, _ = os.OpenFile(filepath.Join(dir, "calendar.log"),
			os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	})
	if logFile == nil {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	fmt.Fprintf(logFile, "%s "+format+"\n",
		append([]any{time.Now().Format("15:04:05")}, a...)...)
}
