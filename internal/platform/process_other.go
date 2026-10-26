//go:build !windows

package platform

import (
	"os/exec"
	"runtime"
)

func OpenPath(path string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}

func RevealPath(path string) error { return OpenPath(path) }

func LaunchDetached(executable string, args ...string) (*exec.Cmd, error) {
	cmd := exec.Command(executable, args...)
	return cmd, cmd.Start()
}
