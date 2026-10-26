package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Paths contains every persistent location used by the application.
type Paths struct {
	Root      string
	Config    string
	Lines     string
	Peaks     string
	Usage     string
	Skins     string
	LogPath   string
	Window    string
	Secondary string
}

// NewPaths resolves the app data directory. Tests may override Paths.Root
// directly after construction.
func NewPaths() Paths {
	base := os.Getenv("APPDATA")
	if base == "" {
		if dir, err := os.UserConfigDir(); err == nil {
			base = dir
		} else {
			base = "."
		}
	}
	root := filepath.Join(base, "dsh-whale-desktop")
	return Paths{
		Root:      root,
		Config:    filepath.Join(root, "config.jsonc"),
		Lines:     filepath.Join(root, "lines.jsonc"),
		Peaks:     filepath.Join(root, "peaks.jsonc"),
		Usage:     filepath.Join(root, ".usage.json"),
		Skins:     filepath.Join(root, "skins"),
		LogPath:   filepath.Join(root, "whale.log"),
		Window:    filepath.Join(root, ".window.json"),
		Secondary: filepath.Join(root, ".window-secondary.json"),
	}
}

// EnsureLayout creates the data and skin directories.
func (p Paths) EnsureLayout() error {
	if err := os.MkdirAll(p.Root, 0o755); err != nil {
		return err
	}
	return os.MkdirAll(p.Skins, 0o755)
}

var logMu sync.Mutex

// Log appends a line to whale.log. Logging must never interrupt the app.
func (p Paths) Log(parts ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	line := time.Now().Format(time.RFC3339) + " "
	for i, part := range parts {
		if i > 0 {
			line += " "
		}
		switch value := part.(type) {
		case string:
			line += value
		case error:
			line += value.Error()
		default:
			line += fmt.Sprint(value)
		}
	}
	line += "\n"
	_ = os.MkdirAll(p.Root, 0o755)
	file, err := os.OpenFile(p.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		_, _ = file.WriteString(line)
		_ = file.Close()
	}
	if os.Getenv("WHALE_DEBUG") != "" {
		fmt.Print(line)
	}
}
