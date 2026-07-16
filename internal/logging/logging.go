// Package logging provides a best-effort, file-based debug log for a
// project's AgentOrchestra workspace. Logging failures never break the
// calling operation — if the log file can't be opened, callers silently
// get a logger that discards output.
package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

const (
	logDirName  = "logs"
	logFileName = "ao.log"
	maxLogBytes = 5 * 1024 * 1024 // rotate once the log exceeds 5MB
)

// Open returns a structured (JSON lines) logger that appends to
// <workspaceDir>/.ao/logs/ao.log, and a close function the caller must
// invoke when done logging. On any error opening the file, it returns a
// logger that discards everything, so operations never fail because of
// logging problems.
func Open(workspaceDir string) (*slog.Logger, func()) {
	dir := filepath.Join(workspaceDir, ".ao", logDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return discard()
	}

	path := filepath.Join(dir, logFileName)
	rotateIfLarge(path)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return discard()
	}
	logger := slog.New(slog.NewJSONHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return logger, func() { f.Close() }
}

// rotateIfLarge renames path to path+".1" (overwriting any previous backup)
// once it grows past maxLogBytes, so ao.log never grows unbounded.
func rotateIfLarge(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxLogBytes {
		return
	}
	backup := path + ".1"
	os.Remove(backup)
	os.Rename(path, backup)
}

func discard() (*slog.Logger, func()) {
	return slog.New(slog.NewTextHandler(io.Discard, nil)), func() {}
}
