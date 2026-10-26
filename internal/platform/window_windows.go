//go:build windows

package platform

import (
	"errors"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
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
	procGetWindowRect    = user32.NewProc("GetWindowRect")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procGetDpiForWindow  = user32.NewProc("GetDpiForWindow")
	procMonitorFromRect  = user32.NewProc("MonitorFromRect")
	procGetMonitorInfo   = user32.NewProc("GetMonitorInfoW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procFindWindow       = user32.NewProc("FindWindowW")
	procShowWindow       = user32.NewProc("ShowWindow")
	procSetForeground    = user32.NewProc("SetForegroundWindow")
	procSetWindowRgn     = user32.NewProc("SetWindowRgn")
	gdi32                = windows.NewLazySystemDLL("gdi32.dll")
	procCreateRectRgn    = gdi32.NewProc("CreateRectRgn")
	procCombineRgn       = gdi32.NewProc("CombineRgn")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
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

// ClipRect is a physical-pixel rectangle relative to the window.
type ClipRect struct {
	Left   int
	Top    int
	Right  int
	Bottom int
}

// SetWindowRegion clips hit testing to the visible interactive areas. Unlike
// WS_EX_TRANSPARENT, this leaves the WebView2 child untouched and clickable.
func (w *Window) SetWindowRegion(rects []ClipRect) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.hwnd == 0 {
		return
	}
	if len(rects) == 0 {
		procSetWindowRgn.Call(w.hwnd, 0, 1)
		return
	}
	combined, _, _ := procCreateRectRgn.Call(0, 0, 0, 0)
	if combined == 0 {
		return
	}
	for _, rect := range rects {
		if rect.Right <= rect.Left || rect.Bottom <= rect.Top {
			continue
		}
		part, _, _ := procCreateRectRgn.Call(
			uintptr(rect.Left),
			uintptr(rect.Top),
			uintptr(rect.Right),
			uintptr(rect.Bottom),
		)
		if part == 0 {
			continue
		}
		procCombineRgn.Call(combined, combined, part, 2) // RGN_OR
		procDeleteObject.Call(part)
	}
	if ok, _, _ := procSetWindowRgn.Call(w.hwnd, combined, 1); ok == 0 {
		procDeleteObject.Call(combined)
	}
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
