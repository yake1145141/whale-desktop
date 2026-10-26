package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"whale-desktop/internal/jsonc"
)

// Anchor describes the initial launch corner.
type Anchor struct {
	H     string  `json:"h"`
	V     string  `json:"v"`
	HDist float64 `json:"hDist"`
	VDist float64 `json:"vDist"`
}

// Config is the fully-resolved application configuration.
type Config struct {
	Skin              string  `json:"skin"`
	Scale             float64 `json:"scale"`
	Anchor            Anchor  `json:"anchor"`
	AlwaysOnTop       bool    `json:"alwaysOnTop"`
	LinesFile         string  `json:"linesFile"`
	RandomLines       bool    `json:"randomLines"`
	BubbleOn          bool    `json:"bubbleOn"`
	BubbleMs          int     `json:"bubbleMs"`
	DisplayMode       string  `json:"displayMode"`
	KeyboardClickMode string  `json:"keyboardClickMode"`
	UsageMode         string  `json:"usageMode"`
	APIKey            string  `json:"apiKey"`
	PlatformToken     string  `json:"platformToken"`
	RefreshMs         int     `json:"refreshMs"`
	Currency          string  `json:"currency"`
	PeakPreset        string  `json:"peakPreset"`
	Sound             bool    `json:"sound"`
	Volume            float64 `json:"volume"`
	SoundSet          string  `json:"soundSet"`
	Draggable         bool    `json:"draggable"`
	Passthrough       bool    `json:"passthrough"`
	ShowMenuButton    bool    `json:"showMenuButton"`
}

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		Skin:              "default",
		Scale:             1.4,
		Anchor:            Anchor{H: "right", V: "bottom", HDist: 40, VDist: 60},
		AlwaysOnTop:       true,
		LinesFile:         "lines.jsonc",
		RandomLines:       true,
		BubbleOn:          true,
		BubbleMs:          5000,
		DisplayMode:       "balance",
		KeyboardClickMode: "balance",
		UsageMode:         "ledger",
		RefreshMs:         60000,
		PeakPreset:        "default",
		Sound:             true,
		Volume:            0.9,
		SoundSet:          "duck",
		Draggable:         true,
		Passthrough:       true,
		ShowMenuButton:    true,
	}
}

// DefaultMap is the canonical map persisted on first run.
func DefaultMap() map[string]any {
	value, _ := jsonc.MarshalMap(Default())
	return value
}

// Result is a loaded configuration and the sparse user-authored portion.
type Result struct {
	Config Config
	User   map[string]any
	Error  string
}

// Load reads config.jsonc. Missing or malformed files fall back to defaults.
func Load(paths Paths) Result {
	res := jsonc.Read(paths.Config)
	defaults := DefaultMap()
	if res.Missing {
		cfg := Default()
		return Result{Config: cfg, User: map[string]any{}}
	}
	if res.Error != nil {
		return Result{
			Config: Default(),
			User:   map[string]any{},
			Error:  "config.jsonc 语法错误: " + res.Error.Error(),
		}
	}
	user := jsonc.Clone(res.Value)
	if _, exists := user["peakPreset"]; !exists {
		if mode, ok := user["peakMode"].(string); ok {
			if mode == "custom" {
				user["peakPreset"] = "legacy-custom"
			} else {
				user["peakPreset"] = mode
			}
		}
	}
	merged := jsonc.DeepMerge(defaults, user)
	var cfg Config
	if err := jsonc.Decode(merged, &cfg); err != nil {
		return Result{Config: Default(), User: user, Error: "config.jsonc 字段错误: " + err.Error()}
	}
	normalize(&cfg)
	return Result{Config: cfg, User: user}
}

func normalize(cfg *Config) {
	if cfg.Scale < 0.6 || cfg.Scale > 2.5 {
		cfg.Scale = 1.4
	}
	if cfg.BubbleMs < 600 {
		cfg.BubbleMs = 5000
	}
	if cfg.RefreshMs < 10000 {
		cfg.RefreshMs = 60000
	}
	if cfg.Anchor.H != "left" {
		cfg.Anchor.H = "right"
	}
	if cfg.Anchor.V != "top" {
		cfg.Anchor.V = "bottom"
	}
	if cfg.DisplayMode != "time" && cfg.DisplayMode != "keyboard" {
		cfg.DisplayMode = "balance"
	}
	if cfg.KeyboardClickMode != "time" {
		cfg.KeyboardClickMode = "balance"
	}
	if cfg.UsageMode != "token" {
		cfg.UsageMode = "ledger"
	}
	cfg.Volume = max(0, min(1, cfg.Volume))
}

// ResolveLinesFile resolves the configured dialogue file relative to app data.
func (cfg Config) ResolveLinesFile(paths Paths) string {
	home, _ := os.UserHomeDir()
	file := jsonc.ExpandHome(strings.TrimSpace(cfg.LinesFile), home)
	if file == "" {
		file = "lines.jsonc"
	}
	if !filepath.IsAbs(file) {
		file = filepath.Join(paths.Root, file)
	}
	return file
}

// Apply merges a sparse patch into the user configuration and persists it.
func Apply(paths Paths, current Result, patch map[string]any) (Result, error) {
	user := jsonc.DeepMerge(current.User, patch)
	merged := jsonc.DeepMerge(DefaultMap(), user)
	var cfg Config
	if err := jsonc.Decode(merged, &cfg); err != nil {
		return current, err
	}
	normalize(&cfg)
	if err := WriteUser(paths, user); err != nil {
		return Result{Config: cfg, User: user}, err
	}
	return Result{Config: cfg, User: user}, nil
}

const configHeader = `// 小鲸鱼桌面挂件配置
// 这里只写入你改过的项；未出现的项使用程序内置默认值。
// 保存后点托盘菜单「重载配置与台词」或挂件菜单「重载配置」即可生效。
// 完整默认值与说明见 README.md 的「主配置」一节。
// 支持 // 注释与尾逗号。

`

// WriteUser atomically writes only the user's sparse configuration.
func WriteUser(paths Paths, user map[string]any) error {
	value, err := jsonc.MarshalMap(user)
	if err != nil {
		return err
	}
	data := value
	body, err := marshalIndentedJSON(data)
	if err != nil {
		return err
	}
	return jsonc.WriteTextAtomic(paths.Config, configHeader+body+"\n")
}

func marshalIndentedJSON(value any) (string, error) {
	// Keep this file's config output identical to the other JSONC files.
	data, err := jsonc.MarshalIndented(value)
	if err != nil {
		return "", fmt.Errorf("encode config: %w", err)
	}
	return data, nil
}
