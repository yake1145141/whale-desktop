//go:build windows

package platform

import (
	"os/exec"
	"syscall"
)

// OpenPath opens a file or folder in the desktop shell.
func OpenPath(path string) error {
	cmd := exec.Command("explorer.exe", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

// RevealPath opens Explorer with the file selected.
func RevealPath(path string) error {
	cmd := exec.Command("explorer.exe", "/select,"+path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

// LaunchDetached starts another instance without holding it as a child
// dependency beyond process monitoring.
func LaunchDetached(executable string, args ...string) (*exec.Cmd, error) {
	cmd := exec.Command(executable, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}
