//go:build !windows

package platform

type Rect struct {
	X, Y, Width, Height int
}

type ClipRect struct {
	Left, Top, Right, Bottom int
}

type Window struct{}

func (w *Window) AttachByClass(string) bool         { return false }
func (w *Window) MakeToolWindow()                   {}
func AcquireSingleInstance(string) bool             { return true }
func ShowWindowByClass(string) bool                 { return false }
func (w *Window) SetWindowRegion([]ClipRect)        {}
func (w *Window) DPI() float64                      { return 1 }
func (w *Window) CursorScreen() (int, int, bool)    { return 0, 0, false }
func (w *Window) Bounds() Rect                      { return Rect{} }
func (w *Window) Move(Rect) bool                    { return false }
func WorkArea(Rect) Rect                            { return Rect{} }
