//go:build windows

package autostart

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	runKey     = `Software\Microsoft\Windows\CurrentVersion\Run`
	valueName  = "H1kaRU.WhaleDesktop"
	legacyName = "MeteorNOX.WhaleDesktop"
)

// State describes the current per-user startup registration.
type State struct {
	Enabled    bool   `json:"enabled"`
	Exe        string `json:"exe"`
	Legacy     bool   `json:"legacy"`
	LegacyName string `json:"legacyName,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Result reports a startup registration change.
type Result struct {
	OK       bool   `json:"ok"`
	Enabled  bool   `json:"enabled"`
	Exe      string `json:"exe"`
	Error    string `json:"error,omitempty"`
	Migrated bool   `json:"migrated,omitempty"`
}

func ExecutablePath() string {
	path, err := os.Executable()
	if err != nil || !strings.EqualFold(filepathExt(path), ".exe") {
		return ""
	}
	// `go run` should never register a disposable binary for login.
	if strings.Contains(strings.ToLower(path), `\go-build`) && os.Getenv("WHALE_ALLOW_DEV_AUTOSTART") == "" {
		return ""
	}
	return path
}

func filepathExt(path string) string {
	index := strings.LastIndex(path, ".")
	if index < 0 {
		return ""
	}
	return path[index:]
}

func BuildRunValue(executable string) (string, error) {
	if executable == "" {
		return "", errors.New("未找到已编译的 WhaleDesktop.exe")
	}
	return `"` + strings.ReplaceAll(executable, `"`, `""`) + `" --autostart`, nil
}

func Get() State {
	exe := ExecutablePath()
	key, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return State{Exe: exe, Error: err.Error()}
	}
	defer key.Close()
	if _, _, err := key.GetStringValue(valueName); err == nil {
		return State{Enabled: true, Exe: exe}
	}
	if _, _, err := key.GetStringValue(legacyName); err == nil {
		return State{Enabled: true, Exe: exe, Legacy: true, LegacyName: legacyName}
	}
	return State{Exe: exe}
}

func Set(enabled bool) Result {
	exe := ExecutablePath()
	if enabled && exe == "" {
		return Result{Error: "当前是开发运行；请使用已编译的 WhaleDesktop.exe 设置开机自启。"}
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return Result{Exe: exe, Error: err.Error()}
	}
	defer key.Close()
	if enabled {
		command, buildErr := BuildRunValue(exe)
		if buildErr != nil {
			return Result{Exe: exe, Error: buildErr.Error()}
		}
		if err := key.SetStringValue(valueName, command); err != nil {
			return Result{Exe: exe, Error: err.Error()}
		}
	} else if err := key.DeleteValue(valueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return Result{Exe: exe, Error: err.Error()}
	}
	if err := key.DeleteValue(legacyName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return Result{Exe: exe, Enabled: enabled, Error: err.Error()}
	}
	return Result{OK: true, Enabled: enabled, Exe: exe}
}

// Sync repairs an existing registration after the executable moves.
func Sync() Result {
	state := Get()
	if !state.Enabled {
		return Result{OK: state.Error == "", Enabled: false, Exe: state.Exe, Error: state.Error}
	}
	result := Set(true)
	result.Migrated = state.Legacy
	return result
}

func ErrorText(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}
