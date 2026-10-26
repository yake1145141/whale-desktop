//go:build !windows

package autostart

type State struct {
	Enabled    bool   `json:"enabled"`
	Exe        string `json:"exe"`
	Legacy     bool   `json:"legacy"`
	LegacyName string `json:"legacyName,omitempty"`
	Error      string `json:"error,omitempty"`
}

type Result struct {
	OK       bool   `json:"ok"`
	Enabled  bool   `json:"enabled"`
	Exe      string `json:"exe"`
	Error    string `json:"error,omitempty"`
	Migrated bool   `json:"migrated,omitempty"`
}

func ExecutablePath() string { return "" }
func Get() State {
	return State{Error: "开机自启仅支持 Windows"}
}
func Set(bool) Result {
	return Result{Error: "开机自启仅支持 Windows"}
}
func Sync() Result { return Set(false) }
