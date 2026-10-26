//go:build windows

package platform

import (
	"errors"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wsExLayered     = 0x00080000
	wsExTransparent = 0x00000020

	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	monitorDefaultToNearest = 2
	smCXScreen              = 0
	smCYScreen              = 1
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procGetWindowLongPtr = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtr = user32.NewProc("SetWindowLongPtrW")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procSetCursorPos     = user32.NewProc("SetCursorPos")
	procScreenToClient   = user32.NewProc("ScreenToClient")
	procGetWindowRect    = user32.NewProc("GetWindowRect")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procGetDpiForWindow  = user32.NewProc("GetDpiForWindow")
	procMonitorFromRect  = user32.NewProc("MonitorFromRect")
	procGetMonitorInfo   = user32.NewProc("GetMonitorInfoW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procFindWindow       = user32.NewProc("FindWindowW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procSetForeground    = user32.NewProc("SetForegroundWindow")
	instanceMutex        windows.Handle
)

const wsExToolWindow = 0x00000080

type point struct {
	X int32
	Y int32
}

type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

// Window is a small Win32 adapter around the HWND created by Wails.
type Window struct {
	mu   sync.Mutex
	hwnd uintptr
}

func (w *Window) attach(hwnd uintptr) {
	w.mu.Lock()
	w.hwnd = hwnd
	w.mu.Unlock()
}

// AttachByClass finds the top-level Wails window by its configured class.
func (w *Window) AttachByClass(className string) bool {
	name, err := windows.UTF16PtrFromString(className)
	if err != nil {
		return false
	}
	hwnd, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(name)), 0)
	if hwnd == 0 {
		return false
	}
	w.attach(hwnd)
	return true
}

// AcquireSingleInstance creates a per-user named kernel mutex. The handle stays
// open until process exit, which is what keeps the name owned.
func AcquireSingleInstance(name string) bool {
	namePtr, err := windows.UTF16PtrFromString("Local\\" + name)
	if err != nil {
		return true
	}
	handle, err := windows.CreateMutex(nil, false, namePtr)
	if err != nil {
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			return false
		}
		return true
	}
	instanceMutex = handle
	return true
}

// ShowWindowByClass brings an existing instance's frameless window forward.
func ShowWindowByClass(className string) bool {
	namePtr, err := windows.UTF16PtrFromString(className)
	if err != nil {
		return false
	}
	hwnd, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(namePtr)), 0)
	if hwnd == 0 {
		return false
	}
	procShowWindow.Call(hwnd, 8) // SW_SHOWNA
	procSetForeground.Call(hwnd)
	return true
}

// MakeToolWindow hides the app from the taskbar while retaining activation.
func (w *Window) MakeToolWindow() {
	hwnd := w.handle()
	if hwnd == 0 {
		return
	}
	index := int32(-20)
	style, _, _ := procGetWindowLongPtr.Call(w.hwnd, uintptr(index))
	style |= wsExToolWindow
	procSetWindowLongPtr.Call(hwnd, uintptr(index), style)
}

func (w *Window) handle() uintptr {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hwnd
}

// SetPassthrough toggles transparent mouse hit testing on the top-level window.
func (w *Window) SetPassthrough(enabled bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.hwnd == 0 {
		return
	}
	index := int32(-20) // GWL_EXSTYLE; keeps this code valid on 32/64-bit.
	style, _, _ := procGetWindowLongPtr.Call(w.hwnd, uintptr(index))
	current := style&wsExTransparent != 0
	if current == enabled {
		return
	}
	if enabled {
		style |= wsExTransparent | wsExLayered
	} else {
		style &^= wsExTransparent
	}
	procSetWindowLongPtr.Call(w.hwnd, uintptr(index), style)
}

// IsPassthrough reports the current Win32 click-through style.
func (w *Window) IsPassthrough() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.hwnd == 0 {
		return false
	}
	index := int32(-20)
	style, _, _ := procGetWindowLongPtr.Call(w.hwnd, uintptr(index))
	return style&wsExTransparent != 0
}

// SetCursorScreen moves the global cursor. Used only by the built-in smoke
// verifier.
func SetCursorScreen(x, y int) bool {
	ok, _, _ := procSetCursorPos.Call(uintptr(x), uintptr(y))
	return ok != 0
}

// DPI returns the current monitor scaling as a ratio (96 DPI = 1.0).
func (w *Window) DPI() float64 {
	hwnd := w.handle()
	if hwnd == 0 {
		return 1
	}
	dpi, _, _ := procGetDpiForWindow.Call(hwnd)
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96
}

// CursorClientDIP returns the cursor position in the same CSS-pixel coordinate
// space used by the renderer.
func (w *Window) CursorClientDIP() (int, int, bool) {
	hwnd := w.handle()
	if hwnd == 0 {
		return 0, 0, false
	}
	var p point
	if ok, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p))); ok == 0 {
		return 0, 0, false
	}
	if ok, _, _ := procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&p))); ok == 0 {
		return 0, 0, false
	}
	scale := w.DPI()
	return int(float64(p.X) / scale), int(float64(p.Y) / scale), true
}

// CursorScreen returns the physical screen cursor position.
func (w *Window) CursorScreen() (int, int, bool) {
	var p point
	if ok, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p))); ok == 0 {
		return 0, 0, false
	}
	return int(p.X), int(p.Y), true
}

// Rect is a physical-pixel window rectangle.
type Rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

func (w *Window) Bounds() Rect {
	hwnd := w.handle()
	if hwnd == 0 {
		return Rect{}
	}
	var r rect
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return Rect{}
	}
	return Rect{X: int(r.Left), Y: int(r.Top), Width: int(r.Right - r.Left), Height: int(r.Bottom - r.Top)}
}

// Move places the physical window rectangle without activating it.
func (w *Window) Move(bounds Rect) bool {
	hwnd := w.handle()
	if hwnd == 0 || bounds.Width <= 0 || bounds.Height <= 0 {
		return false
	}
	ok, _, _ := procSetWindowPos.Call(
		hwnd, 0,
		uintptr(bounds.X), uintptr(bounds.Y),
		uintptr(bounds.Width), uintptr(bounds.Height),
		swpNoZOrder|swpNoActivate,
	)
	return ok != 0
}

// WorkArea returns the physical work area of the monitor containing bounds.
func WorkArea(bounds Rect) Rect {
	r := rect{
		Left: int32(bounds.X), Top: int32(bounds.Y),
		Right: int32(bounds.X + bounds.Width), Bottom: int32(bounds.Y + bounds.Height),
	}
	monitor, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), monitorDefaultToNearest)
	if monitor == 0 {
		width, _, _ := procGetSystemMetrics.Call(smCXScreen)
		height, _, _ := procGetSystemMetrics.Call(smCYScreen)
		return Rect{Width: int(width), Height: int(height)}
	}
	info := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if ok, _, _ := procGetMonitorInfo.Call(monitor, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return Rect{}
	}
	return Rect{
		X: int(info.Work.Left), Y: int(info.Work.Top),
		Width:  int(info.Work.Right - info.Work.Left),
		Height: int(info.Work.Bottom - info.Work.Top),
	}
}

// Validate verifies that a handle is attached.
func (w *Window) Validate() error {
	if w.handle() == 0 {
		return errors.New("window handle is not ready")
	}
	return nil
}
