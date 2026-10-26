//go:build windows

package keyboard

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	whKeyboardLL = 13
	hcAction     = 0
	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105
	wmQuit       = 0x0012
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procSetWindowsHookEx    = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetMessage          = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessage     = user32.NewProc("DispatchMessageW")
	procPostThreadMessage   = user32.NewProc("PostThreadMessageW")
	keyboardCallback        = windows.NewCallback(keyboardProc)
	activeHook              *Hook
	activeHookMu            sync.Mutex
)

type keyboardEvent struct {
	VKCode   uint32
	ScanCode uint32
	Flags    uint32
	Time     uint32
}

type message struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Point   struct{ X, Y int32 }
	Private uint32
}

// Hook observes key presses without consuming them.
type Hook struct {
	mu       sync.Mutex
	handler  func(string)
	running  atomic.Bool
	threadID uint32
	hook     uintptr
	done     chan struct{}
	pressed  map[uint32]struct{}
	order    []uint32
}

// Start installs a low-level keyboard hook on a locked OS thread.
func (h *Hook) Start(handler func(string)) error {
	h.mu.Lock()
	if h.running.Load() {
		h.handler = handler
		h.mu.Unlock()
		return nil
	}
	h.handler = handler
	h.done = make(chan struct{})
	h.pressed = map[uint32]struct{}{}
	h.order = nil
	h.running.Store(true)
	activeHookMu.Lock()
	activeHook = h
	activeHookMu.Unlock()
	ready := make(chan error, 1)
	h.mu.Unlock()

	go h.run(ready)
	err := <-ready
	if err != nil {
		h.running.Store(false)
		activeHookMu.Lock()
		if activeHook == h {
			activeHook = nil
		}
		activeHookMu.Unlock()
	}
	return err
}

func (h *Hook) run(ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	threadID := windows.GetCurrentThreadId()
	h.mu.Lock()
	h.threadID = threadID
	h.mu.Unlock()

	hook, _, callErr := procSetWindowsHookEx.Call(whKeyboardLL, keyboardCallback, 0, 0)
	if hook == 0 {
		if callErr == windows.ERROR_SUCCESS {
			callErr = errors.New("SetWindowsHookExW failed")
		}
		ready <- fmt.Errorf("键盘钩子启动失败: %w", callErr)
		close(h.done)
		return
	}
	h.mu.Lock()
	h.hook = hook
	h.mu.Unlock()
	ready <- nil

	for {
		var msg message
		result, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}

	h.mu.Lock()
	if h.hook != 0 {
		procUnhookWindowsHookEx.Call(h.hook)
		h.hook = 0
	}
	h.handler = nil
	h.pressed = map[uint32]struct{}{}
	h.order = nil
	h.mu.Unlock()
	h.running.Store(false)
	activeHookMu.Lock()
	if activeHook == h {
		activeHook = nil
	}
	activeHookMu.Unlock()
	close(h.done)
}

// Stop ends the hook and its dedicated message loop.
func (h *Hook) Stop() {
	if !h.running.Load() {
		return
	}
	h.mu.Lock()
	threadID, done := h.threadID, h.done
	h.mu.Unlock()
	if threadID != 0 {
		procPostThreadMessage.Call(uintptr(threadID), wmQuit, 0, 0)
	}
	if done != nil {
		<-done
	}
}

func (h *Hook) Running() bool {
	return h.running.Load()
}

func keyboardProc(code, wParam, lParam uintptr) uintptr {
	activeHookMu.Lock()
	hook := activeHook
	activeHookMu.Unlock()
	if code == hcAction && hook != nil && hook.running.Load() {
		event := (*keyboardEvent)(unsafe.Pointer(lParam))
		switch uint32(wParam) {
		case wmKeyDown, wmSysKeyDown:
			hook.handle(event.VKCode, true)
		case wmKeyUp, wmSysKeyUp:
			hook.handle(event.VKCode, false)
		}
	}
	result, _, _ := procCallNextHookEx.Call(0, code, wParam, lParam)
	return result
}

func (h *Hook) handle(code uint32, down bool) {
	h.mu.Lock()
	identity := keyLabel(code)
	if down {
		if _, exists := h.pressed[code]; !exists {
			h.pressed[code] = struct{}{}
			h.order = append(h.order, code)
		}
	} else {
		for held := range h.pressed {
			if keyLabel(held) == identity {
				delete(h.pressed, held)
			}
		}
		for i := 0; i < len(h.order); {
			if _, exists := h.pressed[h.order[i]]; !exists {
				h.order = append(h.order[:i], h.order[i+1:]...)
			} else {
				i++
			}
		}
	}
	label := h.comboLabel()
	handler := h.handler
	h.mu.Unlock()
	if down && label != "" && handler != nil {
		defer func() { _ = recover() }()
		handler(label)
	}
}

func (h *Hook) comboLabel() string {
	shiftHeld := false
	for code := range h.pressed {
		if code == 0xA0 || code == 0xA1 {
			shiftHeld = true
		}
	}
	shiftedNumber := false
	if shiftHeld {
		for code := range h.pressed {
			if shiftSymbol(code) != "" {
				shiftedNumber = true
				break
			}
		}
	}
	labels := make([]string, 0, 3)
	for _, code := range h.order {
		if _, exists := h.pressed[code]; !exists {
			continue
		}
		if shiftedNumber && (code == 0xA0 || code == 0xA1) {
			continue
		}
		label := ""
		if shiftHeld {
			label = shiftSymbol(code)
		}
		if label == "" {
			label = keyLabel(code)
		}
		duplicate := false
		for _, existing := range labels {
			duplicate = duplicate || existing == label
		}
		if !duplicate {
			labels = append(labels, label)
		}
		if len(labels) == 3 {
			break
		}
	}
	return strings.Join(labels, " + ")
}

var namedKeys = map[uint32]string{
	0x08: "Backspace", 0x09: "Tab", 0x0D: "Enter", 0x14: "CapsLock",
	0x1B: "Esc", 0x20: "Space", 0x21: "PgUp", 0x22: "PgDn", 0x23: "End",
	0x24: "Home", 0x25: "←", 0x26: "↑", 0x27: "→", 0x28: "↓", 0x2D: "Insert",
	0x2E: "Delete", 0x5B: "Win", 0x5C: "Win", 0x5D: "Menu", 0x90: "NumLock",
	0x91: "ScrollLock", 0x2C: "PrintScreen", 0xA0: "Shift", 0xA1: "Shift",
	0xA2: "Ctrl", 0xA3: "Ctrl", 0xA4: "Alt", 0xA5: "Alt",
	0x6A: "Num *", 0x6B: "Num +", 0x6D: "Num -", 0x6E: "Num .", 0x6F: "Num /",
	0xBA: ";", 0xBB: "=", 0xBC: ",", 0xBD: "-", 0xBE: ".", 0xBF: "/",
	0xC0: "`", 0xDB: "[", 0xDC: `\`, 0xDD: "]", 0xDE: "'",
}

func keyLabel(code uint32) string {
	if value, ok := namedKeys[code]; ok {
		return value
	}
	if code >= 0x30 && code <= 0x39 {
		return string(rune(code))
	}
	if code >= 0x41 && code <= 0x5A {
		return string(rune(code))
	}
	if code >= 0x60 && code <= 0x69 {
		return "Num " + string(rune('0'+code-0x60))
	}
	if code >= 0x70 && code <= 0x87 {
		return fmt.Sprintf("F%d", code-0x6F)
	}
	return fmt.Sprintf("Key %d", code)
}

func shiftSymbol(code uint32) string {
	symbols := map[uint32]string{
		0x31: "!", 0x32: "@", 0x33: "#", 0x34: "$", 0x35: "%",
		0x36: "^", 0x37: "&", 0x38: "*", 0x39: "(", 0x30: ")",
	}
	return symbols[code]
}
