package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/energye/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"whale-desktop/internal/autostart"
	"whale-desktop/internal/balance"
	"whale-desktop/internal/config"
	"whale-desktop/internal/credentials"
	"whale-desktop/internal/jsonc"
	"whale-desktop/internal/keyboard"
	"whale-desktop/internal/lines"
	"whale-desktop/internal/peaks"
	"whale-desktop/internal/platform"
	"whale-desktop/internal/skin"
)

const (
	windowClassName = "WhaleDesktopWindow"
	balanceTTL      = 25 * time.Second
)

type savedRect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type inset struct {
	Left   int `json:"left"`
	Top    int `json:"top"`
	Right  int `json:"right"`
	Bottom int `json:"bottom"`
}

type windowStore struct {
	Rect           *savedRect `json:"rect,omitempty"`
	Inset          inset      `json:"inset"`
	HAnchor        string     `json:"hAnchor,omitempty"`
	VAnchor        string     `json:"vAnchor,omitempty"`
	FlipHorizontal bool       `json:"flipHorizontal,omitempty"`
	SoundOn        *bool      `json:"soundOn,omitempty"`
	Volume         *float64   `json:"volume,omitempty"`
	SoundSet       string     `json:"soundSet,omitempty"`
	Skin           string     `json:"skin,omitempty"`
}

// HitRegion is one interactive rectangle in renderer CSS pixels.
type HitRegion struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type hitPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type resizeRequest struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Inset  inset   `json:"inset"`
}

type soundPatch struct {
	SoundOn  *bool    `json:"soundOn"`
	Volume   *float64 `json:"volume"`
	SoundSet *string  `json:"soundSet"`
}

// StatePayload is the complete renderer bootstrap and hot-reload payload.
type StatePayload struct {
	Config             config.Config  `json:"config"`
	ConfigError        string         `json:"configError"`
	ConfigFile         string         `json:"configFile"`
	LinesFile          string         `json:"linesFile"`
	LinesError         string         `json:"linesError"`
	Groups             []lines.Group  `json:"groups"`
	PeaksFile          string         `json:"peaksFile"`
	PeaksError         string         `json:"peaksError"`
	PeakPresets        []peaks.Preset `json:"peakPresets"`
	SoundOn            bool           `json:"soundOn"`
	Volume             float64        `json:"volume"`
	SoundSet           string         `json:"soundSet"`
	SecondaryPet       bool           `json:"secondaryPet"`
	DualPet            bool           `json:"dualPet"`
	FlipHorizontal     bool           `json:"flipHorizontal"`
	Skin               *skin.Payload  `json:"skin"`
	Skins              []skin.Summary `json:"skins"`
	HasAPIKey          bool           `json:"hasApiKey"`
	KeySource          string         `json:"keySource"`
	HasPlatformToken   bool           `json:"hasPlatformToken"`
	IsPeak             bool           `json:"isPeak"`
	KeyboardError      string         `json:"keyboardError"`
	KeyboardHookActive bool           `json:"keyboardHookRunning"`
}

type configResult struct {
	Config config.Config `json:"config"`
	Skin   *skin.Payload `json:"skin,omitempty"`
	Groups []lines.Group `json:"groups,omitempty"`
}

type skinResult struct {
	Skin   *skin.Payload `json:"skin"`
	Config config.Config `json:"config"`
}

type dualResult struct {
	Enabled bool `json:"enabled"`
}

type deleteResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// App owns all application state and is bound to the Wails frontend.
type App struct {
	ctx    context.Context
	assets fs.FS
	paths  config.Paths

	mu          sync.RWMutex
	cfg         config.Result
	lines       lines.Result
	peaks       peaks.Result
	allSkins    []skin.Skin
	current     *skin.Skin
	store       windowStore
	keyboardErr string

	window     platform.Window
	regions    []HitRegion
	hitLogged  bool
	second     bool
	secondary  *os.Process
	quitting   atomic.Bool
	visible    atomic.Bool
	dragging   atomic.Bool
	dragMu     sync.Mutex
	dragCancel context.CancelFunc
	dragDone   chan struct{}

	balanceMu     sync.Mutex
	balanceCache  *balance.Payload
	balanceCached time.Time
	lastGood      *balance.Payload

	keyboard keyboard.Hook
	refresh  chan struct{}

	trayMu      sync.Mutex
	trayReady   bool
	trayStarted bool
	autoEnabled bool
}

// NewApp loads all user files before Wails starts.
func NewApp(assets fs.FS, secondPet bool) *App {
	app := &App{
		assets:  assets,
		paths:   config.NewPaths(),
		second:  secondPet,
		refresh: make(chan struct{}, 1),
	}
	_ = app.paths.EnsureLayout()
	if _, err := os.Stat(app.paths.Config); os.IsNotExist(err) {
		_ = config.WriteUser(app.paths, config.DefaultMap())
	}
	if err := skin.InstallDefault(app.paths, assets); err != nil {
		app.paths.Log("installDefaultSkin failed:", err)
	}
	app.cfg = config.Load(app.paths)
	app.lines = lines.Load(app.paths, app.cfg.Config)
	app.peaks = peaks.Load(app.paths, app.mergedConfig())
	app.store = app.loadWindowStore()
	app.current = skin.Load(app.paths, app.windowSkinName())
	app.allSkins = skin.List(app.paths)
	return app
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.visible.Store(true)
	go a.attachWindow()
	a.startConfigWatcher(ctx)
	a.startBalanceLoop(ctx)
	if !a.second {
		delay := time.Duration(0)
		if hasArg("--autostart") {
			delay = 1500 * time.Millisecond
		}
		go func() {
			if delay > 0 {
				time.Sleep(delay)
			}
			a.startTray()
			go a.refreshAutostart()
		}()
	}
	go func() {
		if hasArg("--autostart") {
			time.Sleep(1800 * time.Millisecond)
		} else {
			time.Sleep(900 * time.Millisecond)
		}
		a.applyDisplayMode()
		if a.wantsBalanceDisplay() {
			if payload := a.balance(true); payload.OK {
				a.emit("whale:balance", payload)
			}
		}
	}()
	a.runRequestedVerification()
}

func (a *App) runRequestedVerification() {
	script := ""
	switch {
	case hasArg("--verify-bubble"):
		script = "window.__whaleDebug && window.__whaleDebug.open()"
	case hasArg("--verify-menu"):
		script = "window.__whaleDebug && window.__whaleDebug.openMenu()"
	case hasArg("--verify-lock"):
		script = "window.__whaleDebug && window.__whaleDebug.lock()"
	case hasArg("--verify-open-config"):
		go func() {
			time.Sleep(2500 * time.Millisecond)
			a.paths.Log("verify open config:", a.OpenPath("config"))
		}()
		return
	case hasArg("--verify-open-config-js"):
		go func() {
			time.Sleep(3000 * time.Millisecond)
			runtime.WindowExecJS(a.ctx, "openPath('config')")
		}()
		return
	default:
		return
	}
	go func() {
		time.Sleep(2500 * time.Millisecond)
		runtime.WindowExecJS(a.ctx, script)
	}()
}

func (a *App) attachWindow() {
	for i := 0; i < 30; i++ {
		if a.window.AttachByClass(windowClassName) {
			a.window.MakeToolWindow()
			a.restoreWindowPosition()
			a.reapplyWindowRegion()
			runtime.WindowSetAlwaysOnTop(a.ctx, a.configSnapshot().AlwaysOnTop)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	a.paths.Log("could not attach to Wails window")
}

func (a *App) shutdown(context.Context) {
	a.quitting.Store(true)
	a.endDrag(false)
	a.keyboard.Stop()
	if a.secondary != nil {
		_ = a.secondary.Kill()
	}
	if a.trayStarted {
		systray.Quit()
	}
}

func (a *App) beforeClose(context.Context) bool {
	if a.quitting.Load() {
		return false
	}
	runtime.WindowHide(a.ctx)
	return true
}

// GetState returns a fresh snapshot used by renderer bootstrap and reload.
func (a *App) GetState() StatePayload {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.stateLocked()
}

func (a *App) stateLocked() StatePayload {
	cfg := a.cfg.Config
	key := credentials.APIClientKey(cfg.APIKey)
	payload := StatePayload{
		Config:             cfg,
		ConfigError:        a.cfg.Error,
		ConfigFile:         a.paths.Config,
		LinesFile:          a.lines.File,
		LinesError:         a.lines.Error,
		Groups:             a.lines.Groups,
		PeaksFile:          a.peaks.File,
		PeaksError:         a.peaks.Error,
		PeakPresets:        a.peaks.Presets,
		SecondaryPet:       a.second,
		DualPet:            a.second || a.secondary != nil,
		FlipHorizontal:     a.store.FlipHorizontal,
		Skin:               skin.ToPayload(a.current),
		Skins:              skin.Summaries(a.allSkins),
		HasAPIKey:          key.Value != "",
		KeySource:          key.Source,
		HasPlatformToken:   credentials.PlatformToken(cfg.PlatformToken).Value != "",
		IsPeak:             balance.IsPeakTime(time.Now().Unix()),
		KeyboardError:      a.keyboardErr,
		KeyboardHookActive: a.keyboard.Running(),
	}
	payload.SoundOn, payload.Volume, payload.SoundSet = a.soundLocked()
	return payload
}

func (a *App) emit(name string, payload any) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, name, payload)
	}
}

func (a *App) emitState() {
	a.emit("whale:state", a.GetState())
}

// ClientLog records renderer-side action failures in the same diagnostic log as
// the Go process.
func (a *App) ClientLog(message string) bool {
	a.paths.Log("renderer:", message)
	return true
}

// GetBalance returns current balance and usage. Manual requests bypass cache.
func (a *App) GetBalance(manual bool) balance.Payload {
	payload := a.balance(manual)
	if !a.second || manual {
		a.emit("whale:balance", payload)
	}
	return payload
}

func (a *App) balance(manual bool) balance.Payload {
	a.balanceMu.Lock()
	defer a.balanceMu.Unlock()
	now := time.Now()
	if !manual && a.balanceCache != nil && now.Sub(a.balanceCached) < balanceTTL {
		payload := *a.balanceCache
		payload.Cached = true
		payload.IsPeak = balance.IsPeakTime(now.Unix())
		return payload
	}
	cfg := a.configSnapshot()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	payload := balance.Fetch(ctx, cfg)
	if !payload.OK {
		if payload.Transient && a.lastGood != nil {
			fallback := *a.lastGood
			fallback.Stale = true
			fallback.Warning = payload.Error
			fallback.IsPeak = balance.IsPeakTime(now.Unix())
			return fallback
		}
		return payload
	}
	payload = balance.NewLedgerPayload(a.paths, ctx, cfg, payload)
	copy := payload
	a.lastGood = &copy
	a.balanceCache = &copy
	a.balanceCached = now
	return payload
}

func (a *App) startBalanceLoop(ctx context.Context) {
	go func() {
		for {
			delay := time.Duration(a.configSnapshot().RefreshMs) * time.Millisecond
			if delay < 10*time.Second {
				delay = 60 * time.Second
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-a.refresh:
				timer.Stop()
			case <-timer.C:
				if a.wantsBalanceDisplay() {
					a.emit("whale:balance", a.balance(false))
				}
			}
		}
	}()
}

func (a *App) resetBalanceLoop() {
	select {
	case a.refresh <- struct{}{}:
	default:
	}
}

func (a *App) wantsBalanceDisplay() bool {
	cfg := a.configSnapshot()
	return cfg.DisplayMode == "balance" ||
		(cfg.DisplayMode == "keyboard" && cfg.KeyboardClickMode != "time")
}

// PickLines performs one weighted dialogue draw.
func (a *App) PickLines() lines.Selection {
	a.mu.RLock()
	groups := a.lines.Groups
	a.mu.RUnlock()
	return lines.Pick(groups)
}

// SetConfig merges, persists, and hot-reloads a sparse config patch.
func (a *App) SetConfig(patch map[string]any) configResult {
	a.mu.Lock()
	before := a.cfg.Config
	updated, err := config.Apply(a.paths, a.cfg, patch)
	if err != nil {
		a.cfg.Error = "配置写入失败: " + err.Error()
	} else {
		a.cfg = updated
	}
	needsSkin := hasKey(patch, "skin") && a.cfg.Config.Skin != before.Skin
	needsLines := hasKey(patch, "linesFile")
	needsPeaks := hasKey(patch, "peakPreset")
	if needsSkin {
		a.current = skin.Load(a.paths, a.windowSkinName())
	}
	if needsLines {
		a.lines = lines.Load(a.paths, a.cfg.Config)
	}
	if needsPeaks {
		a.peaks = peaks.Load(a.paths, a.mergedConfig())
	}
	cfg := a.cfg.Config
	result := configResult{Config: cfg}
	if needsSkin {
		result.Skin = skin.ToPayload(a.current)
	}
	if needsLines {
		result.Groups = a.lines.Groups
	}
	a.mu.Unlock()

	if hasAny(patch, "apiKey", "platformToken", "currency", "usageMode") {
		a.balanceMu.Lock()
		a.balanceCache = nil
		a.balanceCached = time.Time{}
		a.balanceMu.Unlock()
	}
	if hasKey(patch, "alwaysOnTop") && a.ctx != nil {
		runtime.WindowSetAlwaysOnTop(a.ctx, cfg.AlwaysOnTop)
	}
	if hasKey(patch, "passthrough") {
		a.reapplyWindowRegion()
	}
	if hasAny(patch, "displayMode", "keyboardClickMode", "refreshMs") {
		a.applyDisplayMode()
		a.resetBalanceLoop()
		if a.wantsBalanceDisplay() {
			go func() { a.emit("whale:balance", a.balance(false)) }()
		}
	}
	a.rebuildTray()
	if needsSkin || needsLines || needsPeaks || hasAny(patch, "displayMode", "keyboardClickMode") {
		a.emitState()
	}
	return result
}

// Reload reloads every user-editable source and emits a complete state.
func (a *App) Reload() StatePayload {
	a.reloadAll()
	a.emitState()
	return a.GetState()
}

func (a *App) reloadAll() {
	a.mu.Lock()
	a.cfg = config.Load(a.paths)
	a.lines = lines.Load(a.paths, a.cfg.Config)
	a.peaks = peaks.Load(a.paths, a.mergedConfig())
	a.current = skin.Load(a.paths, a.windowSkinName())
	a.allSkins = skin.List(a.paths)
	a.mu.Unlock()
	if a.ctx != nil {
		runtime.WindowSetAlwaysOnTop(a.ctx, a.configSnapshot().AlwaysOnTop)
	}
	a.applyDisplayMode()
	a.resetBalanceLoop()
	a.rebuildTray()
	a.reapplyWindowRegion()
}

func (a *App) startConfigWatcher(ctx context.Context) {
	last := fileModTime(a.paths.Config)
	go func() {
		ticker := time.NewTicker(1500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				next := fileModTime(a.paths.Config)
				if next.IsZero() || next.Equal(last) {
					continue
				}
				last = next
				a.reloadAll()
				a.emitState()
			}
		}
	}()
}

// UpdateHitRegions applies the renderer's current interactive areas as a native
// window region. Areas outside the pet, bubble and menu pass clicks through to
// the desktop without changing the WebView2 child's mouse behavior.
func (a *App) UpdateHitRegions(regions []HitRegion) bool {
	a.mu.Lock()
	a.regions = append(a.regions[:0], regions...)
	enabled := a.cfg.Config.Passthrough
	if !a.hitLogged {
		a.hitLogged = true
		a.paths.Log("hit regions initialized:", len(regions))
	}
	a.mu.Unlock()
	a.applyWindowRegion(regions, enabled)
	return true
}

func (a *App) applyWindowRegion(regions []HitRegion, enabled bool) {
	if !enabled || a.dragging.Load() {
		a.window.SetWindowRegion(nil)
		return
	}
	scale := a.window.DPI()
	if scale <= 0 {
		scale = 1
	}
	clips := make([]platform.ClipRect, 0, len(regions))
	for _, region := range regions {
		if region.Width <= 0 || region.Height <= 0 {
			continue
		}
		clips = append(clips, platform.ClipRect{
			Left:   int(math.Round(region.Left * scale)),
			Top:    int(math.Round(region.Top * scale)),
			Right:  int(math.Round((region.Left + region.Width) * scale)),
			Bottom: int(math.Round((region.Top + region.Height) * scale)),
		})
	}
	a.window.SetWindowRegion(clips)
}

func (a *App) reapplyWindowRegion() {
	a.mu.RLock()
	regions := append([]HitRegion(nil), a.regions...)
	enabled := a.cfg.Config.Passthrough
	a.mu.RUnlock()
	a.applyWindowRegion(regions, enabled)
}

// Resize keeps the anchored edge fixed while the pet changes scale or skin.
func (a *App) Resize(request resizeRequest) bool {
	if request.Width < 80 || request.Height < 80 {
		return false
	}
	scale := a.window.DPI()
	if scale <= 0 {
		scale = 1
	}
	current := a.window.Bounds()
	if current.Width == 0 {
		return false
	}
	newW := int(math.Ceil(request.Width * scale))
	newH := int(math.Ceil(request.Height * scale))
	a.mu.Lock()
	store := a.store
	x, y := current.X, current.Y
	if store.Rect != nil && store.Rect.W > 0 && store.Rect.H > 0 {
		if store.HAnchor == "right" {
			x = current.X + current.Width - newW
		}
		if store.VAnchor == "bottom" {
			y = current.Y + current.Height - newH
		}
	} else {
		cfg := a.cfg.Config
		area := platform.WorkArea(platform.Rect{Width: newW, Height: newH})
		x = anchoredX(area, newW, cfg.Anchor, scale)
		y = anchoredY(area, newH, cfg.Anchor, scale)
	}
	store.Rect = &savedRect{X: x, Y: y, W: newW, H: newH}
	store.Inset = request.Inset
	a.store = store
	a.saveWindowStoreLocked()
	a.mu.Unlock()
	a.window.Move(platform.Rect{X: x, Y: y, Width: newW, Height: newH})
	return true
}

func anchoredX(area platform.Rect, width int, anchor config.Anchor, scale float64) int {
	distance := int(math.Round(anchor.HDist * scale))
	if anchor.H == "left" {
		return area.X + distance
	}
	return area.X + area.Width - width - distance
}

func anchoredY(area platform.Rect, height int, anchor config.Anchor, scale float64) int {
	distance := int(math.Round(anchor.VDist * scale))
	if anchor.V == "top" {
		return area.Y + distance
	}
	return area.Y + area.Height - height - distance
}

func (a *App) restoreWindowPosition() {
	bounds := a.window.Bounds()
	if bounds.Width == 0 {
		return
	}
	a.mu.RLock()
	store := a.store
	cfg := a.cfg.Config
	a.mu.RUnlock()
	if store.Rect != nil {
		bounds.X, bounds.Y = store.Rect.X, store.Rect.Y
	} else {
		area := platform.WorkArea(bounds)
		dpi := a.window.DPI()
		bounds.X = anchoredX(area, bounds.Width, cfg.Anchor, dpi)
		bounds.Y = anchoredY(area, bounds.Height, cfg.Anchor, dpi)
		if a.second {
			bounds.X -= int(math.Round(220 * dpi))
			bounds.Y -= int(math.Round(140 * dpi))
		}
	}
	area := platform.WorkArea(bounds)
	insetPx := insetPhysical(store.Inset, a.window.DPI())
	bounds.X = clamp(bounds.X, area.X-insetPx.Left, area.X+area.Width-bounds.Width+insetPx.Right)
	bounds.Y = clamp(bounds.Y, area.Y-insetPx.Top, area.Y+area.Height-bounds.Height+insetPx.Bottom)
	a.window.Move(bounds)
	a.rememberBoundsLocked()
}

func insetPhysical(value inset, scale float64) inset {
	return inset{
		Left:   int(math.Round(float64(value.Left) * scale)),
		Top:    int(math.Round(float64(value.Top) * scale)),
		Right:  int(math.Round(float64(value.Right) * scale)),
		Bottom: int(math.Round(float64(value.Bottom) * scale)),
	}
}

// DragBegin starts a cursor-follow loop. The offset is in renderer CSS pixels.
func (a *App) DragBegin(_ hitPoint) bool {
	cfg := a.configSnapshot()
	if !cfg.Draggable {
		return false
	}
	cursorX, cursorY, ok := a.window.CursorScreen()
	if !ok {
		return false
	}
	a.dragMu.Lock()
	if a.dragDone != nil {
		a.dragMu.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(a.ctx)
	done := make(chan struct{})
	a.dragCancel = cancel
	a.dragDone = done
	a.dragging.Store(true)
	a.dragMu.Unlock()

	a.window.SetWindowRegion(nil)
	go a.dragLoop(ctx, done, cursorX, cursorY)
	return true
}

func (a *App) dragLoop(ctx context.Context, done chan struct{}, startCursorX, startCursorY int) {
	defer func() {
		a.dragMu.Lock()
		a.dragCancel = nil
		a.dragDone = nil
		a.dragging.Store(false)
		a.dragMu.Unlock()
		close(done)
	}()
	startBounds := a.window.Bounds()
	lastX, lastY := startCursorX, startCursorY
	lastMoved := time.Now()
	ticker := time.NewTicker(12 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentX, currentY, ok := a.window.CursorScreen()
			if !ok {
				return
			}
			if currentX != lastX || currentY != lastY {
				lastX, lastY = currentX, currentY
				lastMoved = time.Now()
				x := startBounds.X + currentX - startCursorX
				y := startBounds.Y + currentY - startCursorY
				a.window.Move(platform.Rect{X: x, Y: y, Width: startBounds.Width, Height: startBounds.Height})
			} else if time.Since(lastMoved) > 10*time.Second {
				a.endDragFromLoop()
				return
			}
		}
	}
}

// DragEnd settles the window into the nearest work area and saves its position.
func (a *App) DragEnd() bool {
	a.endDrag(true)
	return true
}

func (a *App) endDrag(snap bool) {
	a.dragMu.Lock()
	cancel, done := a.dragCancel, a.dragDone
	a.dragMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
	if snap {
		a.dropSnap()
	} else {
		a.rememberBounds()
	}
	a.reapplyWindowRegion()
}

func (a *App) endDragFromLoop() {
	a.dragMu.Lock()
	cancel := a.dragCancel
	a.dragMu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.dropSnap()
	a.reapplyWindowRegion()
}

func (a *App) dropSnap() {
	bounds := a.window.Bounds()
	area := platform.WorkArea(bounds)
	a.mu.RLock()
	content := insetPhysical(a.store.Inset, a.window.DPI())
	a.mu.RUnlock()
	scale := a.window.DPI()
	threshold := int(140 * scale)
	x, y := bounds.X, bounds.Y
	dLeft := bounds.X - area.X
	dRight := area.X + area.Width - (bounds.X + bounds.Width)
	dTop := bounds.Y - area.Y
	dBottom := area.Y + area.Height - (bounds.Y + bounds.Height)
	hAnchor, vAnchor := "right", "bottom"
	if dLeft <= dRight {
		hAnchor = "left"
	}
	if dTop <= dBottom {
		vAnchor = "top"
	}
	if min(dLeft, dRight) < threshold {
		if hAnchor == "left" {
			x = area.X - content.Left
		} else {
			x = area.X + area.Width - bounds.Width + content.Right
		}
	}
	if min(dTop, dBottom) < threshold {
		if vAnchor == "top" {
			y = area.Y - content.Top
		} else {
			y = area.Y + area.Height - bounds.Height + content.Bottom
		}
	}
	x = clamp(x, area.X-content.Left, area.X+area.Width-bounds.Width+content.Right)
	y = clamp(y, area.Y-content.Top, area.Y+area.Height-bounds.Height+content.Bottom)
	a.window.Move(platform.Rect{X: x, Y: y, Width: bounds.Width, Height: bounds.Height})
	a.mu.Lock()
	a.store.HAnchor = hAnchor
	a.store.VAnchor = vAnchor
	a.rememberBoundsLocked()
	a.mu.Unlock()
}

func (a *App) rememberBounds() {
	a.mu.Lock()
	a.rememberBoundsLocked()
	a.mu.Unlock()
}

func (a *App) rememberBoundsLocked() {
	bounds := a.window.Bounds()
	if bounds.Width == 0 {
		return
	}
	a.store.Rect = &savedRect{X: bounds.X, Y: bounds.Y, W: bounds.Width, H: bounds.Height}
	a.saveWindowStoreLocked()
}

// SetFlip stores the per-pet horizontal orientation.
func (a *App) SetFlip(value bool) bool {
	a.mu.Lock()
	a.store.FlipHorizontal = value
	a.saveWindowStoreLocked()
	a.mu.Unlock()
	a.rebuildTray()
	a.emitState()
	return value
}

// SetSkin switches only the current pet, preserving a second pet's choice.
func (a *App) SetSkin(name string) skinResult {
	a.mu.Lock()
	a.store.Skin = name
	a.current = skin.Load(a.paths, name)
	a.saveWindowStoreLocked()
	result := skinResult{Skin: skin.ToPayload(a.current), Config: a.cfg.Config}
	a.mu.Unlock()
	a.rebuildTray()
	a.emitState()
	return result
}

// SetSound updates the current pet's independent sound settings.
func (a *App) SetSound(patch soundPatch) map[string]any {
	a.mu.Lock()
	if patch.SoundOn != nil {
		a.store.SoundOn = patch.SoundOn
	}
	if patch.Volume != nil {
		value := max(0.0, min(1.0, *patch.Volume))
		a.store.Volume = &value
	}
	if patch.SoundSet != nil {
		a.store.SoundSet = *patch.SoundSet
	}
	a.saveWindowStoreLocked()
	on, volume, set := a.soundLocked()
	a.mu.Unlock()
	a.emitState()
	return map[string]any{"soundOn": on, "volume": volume, "soundSet": set}
}

func (a *App) soundLocked() (bool, float64, string) {
	on := a.cfg.Config.Sound
	if a.store.SoundOn != nil {
		on = *a.store.SoundOn
	}
	volume := a.cfg.Config.Volume
	if a.store.Volume != nil {
		volume = *a.store.Volume
	}
	set := firstNonEmpty(a.store.SoundSet, a.cfg.Config.SoundSet, "duck")
	return on, max(0, min(1, volume)), set
}

// ToggleDualPet starts or stops a separate second instance.
func (a *App) ToggleDualPet(enabled bool) dualResult {
	if a.second {
		if !enabled {
			a.Quit()
		}
		return dualResult{}
	}
	if enabled {
		a.startSecondary()
	} else {
		a.stopSecondary()
	}
	return dualResult{Enabled: a.hasSecondary()}
}

func (a *App) startSecondary() {
	a.mu.Lock()
	if a.secondary != nil {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	executable, err := os.Executable()
	if err != nil {
		a.paths.Log("secondary executable:", err)
		return
	}
	cmd, err := platform.LaunchDetached(executable, "--second-pet")
	if err != nil {
		a.paths.Log("secondary pet spawn failed:", err)
		return
	}
	a.mu.Lock()
	a.secondary = cmd.Process
	a.mu.Unlock()
	go func(process *os.Process) {
		_, _ = process.Wait()
		a.mu.Lock()
		if a.secondary == process {
			a.secondary = nil
		}
		a.mu.Unlock()
		a.rebuildTray()
		a.emitState()
	}(cmd.Process)
	a.rebuildTray()
	a.emitState()
}

func (a *App) stopSecondary() {
	a.mu.Lock()
	process := a.secondary
	a.secondary = nil
	a.mu.Unlock()
	if process != nil {
		_ = process.Kill()
	}
	a.rebuildTray()
	a.emitState()
}

func (a *App) hasSecondary() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.second || a.secondary != nil
}

// OpenPath opens a user-editable file or folder in Explorer.
func (a *App) OpenPath(which string) bool {
	target := a.paths.Root
	switch which {
	case "config":
		target = a.paths.Config
	case "lines":
		target = a.currentLinesFile()
	case "peaks":
		target = a.paths.Peaks
	case "skins":
		target = a.paths.Skins
	case "skin":
		a.mu.RLock()
		if a.current != nil {
			target = a.current.Dir
		}
		a.mu.RUnlock()
	}
	var err error
	if which == "config" || which == "lines" || which == "peaks" {
		err = platform.RevealPath(target)
	} else {
		err = platform.OpenPath(target)
	}
	if err != nil {
		a.paths.Log("open path failed:", which, target, err)
		return false
	}
	a.paths.Log("open path succeeded:", which, target)
	return true
}

// ReadFile returns one editable file.
func (a *App) ReadFile(which string) string {
	target := a.fileFor(which)
	if target == "" {
		return ""
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return ""
	}
	return string(data)
}

// WriteFile writes one editable file and reloads all configuration.
func (a *App) WriteFile(which, text string) deleteResult {
	target := a.fileFor(which)
	if target == "" {
		return deleteResult{Error: "unknown file"}
	}
	if err := jsonc.WriteTextAtomic(target, text); err != nil {
		return deleteResult{Error: err.Error()}
	}
	a.Reload()
	return deleteResult{OK: true}
}

func (a *App) fileFor(which string) string {
	switch which {
	case "config":
		return a.paths.Config
	case "lines":
		return a.currentLinesFile()
	case "peaks":
		return a.paths.Peaks
	default:
		return ""
	}
}

func (a *App) currentLinesFile() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.Config.ResolveLinesFile(a.paths)
}

// Hide hides the pet without exiting the tray process.
func (a *App) Hide() bool {
	if a.ctx != nil {
		runtime.WindowHide(a.ctx)
		a.visible.Store(false)
	}
	return true
}

// Quit exits the entire application.
func (a *App) Quit() bool {
	a.quitting.Store(true)
	if a.trayStarted {
		systray.Quit()
	}
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
	return true
}

func (a *App) toggleWindow() {
	if a.ctx == nil {
		return
	}
	if a.visible.Load() {
		runtime.WindowHide(a.ctx)
		a.visible.Store(false)
	} else {
		runtime.WindowShow(a.ctx)
		a.visible.Store(true)
		runtime.WindowSetAlwaysOnTop(a.ctx, a.configSnapshot().AlwaysOnTop)
	}
}

func (a *App) applyDisplayMode() {
	cfg := a.configSnapshot()
	if cfg.DisplayMode != "keyboard" {
		a.keyboard.Stop()
		a.mu.Lock()
		a.keyboardErr = ""
		a.mu.Unlock()
		return
	}
	err := a.keyboard.Start(func(label string) {
		if a.configSnapshot().DisplayMode == "keyboard" {
			a.emit("whale:key", map[string]any{"label": label, "ts": time.Now().UnixMilli()})
		}
	})
	a.mu.Lock()
	if err != nil {
		a.keyboardErr = err.Error()
	} else {
		a.keyboardErr = ""
	}
	a.mu.Unlock()
	if err != nil {
		a.paths.Log("keyboard hook failed:", err)
		a.emit("whale:key", map[string]any{"error": err.Error()})
	}
}

func (a *App) mergedConfig() map[string]any {
	a.mu.RLock()
	user := a.cfg.User
	a.mu.RUnlock()
	return jsonc.DeepMerge(config.DefaultMap(), user)
}

func (a *App) configSnapshot() config.Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.cfg.Config
}

func (a *App) windowSkinName() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.store.Skin != "" {
		return a.store.Skin
	}
	return a.cfg.Config.Skin
}

func (a *App) loadWindowStore() windowStore {
	file := a.paths.Window
	if a.second {
		file = a.paths.Secondary
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return windowStore{HAnchor: "right", VAnchor: "bottom"}
	}
	var store windowStore
	if json.Unmarshal(data, &store) != nil {
		return windowStore{HAnchor: "right", VAnchor: "bottom"}
	}
	if store.HAnchor == "" {
		store.HAnchor = "right"
	}
	if store.VAnchor == "" {
		store.VAnchor = "bottom"
	}
	return store
}

func (a *App) saveWindowStoreLocked() {
	file := a.paths.Window
	if a.second {
		file = a.paths.Secondary
	}
	if err := jsonc.WriteAtomic(file, a.store); err != nil {
		a.paths.Log("save window state:", err)
	}
}

func (a *App) startTray() {
	a.trayMu.Lock()
	if a.trayStarted {
		a.trayMu.Unlock()
		return
	}
	a.trayStarted = true
	a.trayMu.Unlock()
	systray.Run(a.trayOnReady, func() {})
}

func (a *App) trayOnReady() {
	a.trayMu.Lock()
	a.trayReady = true
	a.trayMu.Unlock()
	icon, _ := fs.ReadFile(a.assets, "icon.ico")
	if len(icon) > 0 {
		systray.SetIcon(icon)
	}
	systray.SetTooltip("WhaleDesktop · 小鲸鱼余额挂件")
	systray.SetOnClick(func(systray.IMenu) { a.toggleWindow() })
	systray.SetOnRClick(func(menu systray.IMenu) { _ = menu.ShowMenu() })
	a.buildTrayMenu()
}

func (a *App) rebuildTray() {
	a.trayMu.Lock()
	ready := a.trayReady
	a.trayMu.Unlock()
	if !ready {
		return
	}
	a.buildTrayMenu()
}

func (a *App) buildTrayMenu() {
	a.trayMu.Lock()
	if !a.trayReady {
		a.trayMu.Unlock()
		return
	}
	a.trayMu.Unlock()
	systray.ResetMenu()
	show := systray.AddMenuItem("显示/隐藏小鲸鱼", "左键托盘图标也可以切换")
	show.Click(func() { a.toggleWindow() })
	systray.AddSeparator()

	a.mu.RLock()
	skins := append([]skin.Skin(nil), a.allSkins...)
	current := ""
	if a.current != nil {
		current = a.current.Name
	}
	flip := a.store.FlipHorizontal
	dual := a.secondary != nil || a.second
	a.mu.RUnlock()
	skinMenu := systray.AddMenuItem("皮肤", "切换当前桌宠的皮肤")
	for _, item := range skins {
		label := item.DisplayName
		if item.Name == current {
			label += " ✓"
		}
		menuItem := skinMenu.AddSubMenuItem(label, item.Description)
		name := item.Name
		menuItem.Click(func() { a.SetSkin(name) })
	}

	refresh := systray.AddMenuItem("刷新余额", "立即拉取 DeepSeek 余额")
	refresh.Click(func() {
		go func() { a.emit("whale:balance", a.balance(true)) }()
	})
	systray.AddSeparator()

	dualItem := systray.AddMenuItemCheckbox("双开桌宠", "启动第二个独立桌宠", dual)
	dualItem.Click(func() { a.ToggleDualPet(dualItem.Checked()) })
	flipItem := systray.AddMenuItemCheckbox("水平翻转", "只翻转当前桌宠", flip)
	flipItem.Click(func() { a.SetFlip(flipItem.Checked()) })
	systray.AddSeparator()

	openMenu := systray.AddMenuItem("打开配置", "打开用户配置文件")
	openRoot := openMenu.AddSubMenuItem("打开配置文件夹", "")
	openRoot.Click(func() { a.OpenPath("root") })
	openLines := openMenu.AddSubMenuItem("打开台词文件", "")
	openLines.Click(func() { a.OpenPath("lines") })
	openPeaks := openMenu.AddSubMenuItem("打开峰谷文件", "")
	openPeaks.Click(func() { a.OpenPath("peaks") })
	openSkins := systray.AddMenuItem("打开皮肤文件夹", "")
	openSkins.Click(func() { a.OpenPath("skins") })
	reload := systray.AddMenuItem("重载配置与台词", "")
	reload.Click(func() { a.Reload() })
	systray.AddSeparator()

	auto := systray.AddMenuItemCheckbox("开机自启", "当前 Windows 用户登录时自动启动", a.autoEnabled)
	auto.Click(func() {
		want := auto.Checked()
		go func() {
			result := autostart.Set(want)
			if !result.OK {
				auto.Uncheck()
				_, _ = runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
					Type:    runtime.ErrorDialog,
					Title:   "开机自启设置失败",
					Message: result.Error,
				})
			} else {
				if want {
					auto.Check()
				}
			}
			a.refreshAutostart()
		}()
	})
	systray.AddSeparator()
	quit := systray.AddMenuItem("退出", "")
	quit.Click(func() { a.Quit() })
}

func (a *App) refreshAutostart() {
	state := autostart.Get()
	if state.Enabled && !a.second {
		if result := autostart.Sync(); !result.OK {
			a.paths.Log("autostart sync failed:", result.Error)
		}
	}
	a.autoEnabled = state.Enabled
	a.rebuildTray()
}

func fileModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

func hasArg(arg string) bool {
	for _, value := range os.Args[1:] {
		if value == arg {
			return true
		}
	}
	return false
}

func hasKey(values map[string]any, key string) bool {
	_, ok := values[key]
	return ok
}

func hasAny(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if hasKey(values, key) {
			return true
		}
	}
	return false
}

func clamp(value, low, high int) int {
	if high < low {
		high = low
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
