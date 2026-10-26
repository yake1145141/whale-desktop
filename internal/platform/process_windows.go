//go:build windows

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

var (
	shell32                 = syscall.NewLazyDLL("shell32.dll")
	ole32                   = syscall.NewLazyDLL("ole32.dll")
	procShellExecute        = shell32.NewProc("ShellExecuteW")
	procSHParseDisplayName  = shell32.NewProc("SHParseDisplayName")
	procOpenFolderAndSelect = shell32.NewProc("SHOpenFolderAndSelectItems")
	procCoTaskMemFree       = ole32.NewProc("CoTaskMemFree")
)

// OpenPath opens a file or folder with its registered Windows shell handler.
func OpenPath(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	result, _, _ := procShellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(target)),
		0,
		0,
		1, // SW_SHOWNORMAL
	)
	if result <= 32 {
		return fmt.Errorf("ShellExecuteW open failed (code %d)", result)
	}
	return nil
}

// RevealPath opens the parent folder with the file selected.
func RevealPath(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	target, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	var itemIDList uintptr
	var attributes uint32
	result, _, _ := procSHParseDisplayName.Call(
		uintptr(unsafe.Pointer(target)),
		0,
		uintptr(unsafe.Pointer(&itemIDList)),
		0,
		uintptr(unsafe.Pointer(&attributes)),
	)
	if int32(result) < 0 || itemIDList == 0 {
		return fmt.Errorf("SHParseDisplayName failed (0x%08X)", uint32(result))
	}
	defer procCoTaskMemFree.Call(itemIDList)
	result, _, _ = procOpenFolderAndSelect.Call(itemIDList, 0, 0, 0)
	if int32(result) < 0 {
		return fmt.Errorf("SHOpenFolderAndSelectItems failed (0x%08X)", uint32(result))
	}
	return nil
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
