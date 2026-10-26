package peaks

import (
	"fmt"
	"regexp"
	"strings"

	"whale-desktop/internal/config"
	"whale-desktop/internal/jsonc"
)

// Preset is one selectable peak/off-peak wording pair.
type Preset struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Peak    string `json:"peak"`
	OffPeak string `json:"offPeak"`
}

// Result is a loaded peaks file.
type Result struct {
	File    string   `json:"file"`
	Presets []Preset `json:"presets"`
	Error   string   `json:"error,omitempty"`
	Created bool     `json:"created"`
}

// Default is written on first run.
var Default = map[string]any{
	"_README": []any{
		"峰谷文案配置。每个 presets 项会在桌宠菜单里显示为一项。",
		"name = 菜单显示名，peak = 高峰时段文案，offPeak = 空闲时段文案。",
		"可以添加任意多项；id 可省略，省略时会根据 name 自动生成。",
	},
	"presets": []any{
		map[string]any{"id": "default", "name": "默认", "peak": "高峰时段", "offPeak": "空闲时段"},
		map[string]any{"id": "liangwen", "name": "梁文峰谷", "peak": "梁文峰", "offPeak": "梁文谷"},
		map[string]any{"id": "qiangqiang", "name": "!?强强?!", "peak": "!?峰峰?!", "offPeak": "!?谷谷?!"},
	},
}

var nonID = regexp.MustCompile(`[^\w.-]+`)

// Load reads peaks.jsonc and preserves a legacy custom preset.
func Load(paths config.Paths, merged map[string]any) Result {
	res := jsonc.Read(paths.Peaks)
	if res.Missing {
		err := jsonc.WriteAtomic(paths.Peaks, Default)
		return Result{File: paths.Peaks, Presets: normalize(Default), Created: err == nil}
	}
	if res.Error != nil {
		return Result{
			File: paths.Peaks, Presets: normalize(Default),
			Error: "峰谷文案文件语法错误: " + res.Error.Error(),
		}
	}
	presets := normalize(res.Value)
	if mode, _ := merged["peakMode"].(string); mode == "custom" {
		labels, _ := merged["peakLabels"].(map[string]any)
		if labels != nil && !contains(presets, "legacy-custom") {
			presets = append(presets, Preset{
				ID: "legacy-custom", Name: "自定义（旧配置）",
				Peak:    firstNonEmpty(text(labels["peak"]), "高峰时段"),
				OffPeak: firstNonEmpty(text(labels["offPeak"]), "空闲时段"),
			})
		}
	}
	return Result{File: paths.Peaks, Presets: presets}
}

func normalize(raw map[string]any) []Preset {
	items, ok := raw["presets"].([]any)
	if !ok {
		items = Default["presets"].([]any)
	}
	used := map[string]bool{}
	out := make([]Preset, 0, len(items))
	for i, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := firstNonEmpty(text(obj["name"]), text(obj["label"]), "方案 "+itoa(i+1))
		if name == "" {
			continue
		}
		id := makeID(firstNonEmpty(text(obj["id"]), text(obj["key"]), name), i, used)
		out = append(out, Preset{
			ID: id, Name: name,
			Peak:    firstNonEmpty(text(obj["peak"]), text(obj["peakText"]), "高峰时段"),
			OffPeak: firstNonEmpty(text(obj["offPeak"]), text(obj["offpeak"]), text(obj["valley"]), "空闲时段"),
		})
	}
	if len(out) == 0 {
		return normalize(Default)
	}
	return out
}

func makeID(value string, index int, used map[string]bool) string {
	base := strings.Trim(nonID.ReplaceAllString(strings.ReplaceAll(strings.TrimSpace(value), " ", "-"), "-"), "-.")
	if base == "" {
		base = "preset-" + itoa(index+1)
	}
	id := base
	for suffix := 2; used[id]; suffix++ {
		id = base + "-" + itoa(suffix)
	}
	used[id] = true
	return id
}

func contains(items []Preset, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func Find(items []Preset, id string) *Preset {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	if len(items) > 0 {
		return &items[0]
	}
	return nil
}

func text(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func itoa(value int) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	var out [20]byte
	i := len(out)
	for value > 0 {
		i--
		out[i] = digits[value%10]
		value /= 10
	}
	return string(out[i:])
}
