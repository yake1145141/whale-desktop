package lines

import (
	"errors"
	"math/rand"
	"strconv"

	"whale-desktop/internal/config"
	"whale-desktop/internal/jsonc"
)

// Line is one of the three fixed dialogue rows.
type Line struct {
	Text  string `json:"t"`
	Style string `json:"s"`
	Color string `json:"c"`
	Wrap  bool   `json:"w"`
}

// Variant is one weighted set of dialogue rows.
type Variant struct {
	Weight       float64 `json:"w"`
	SingleWeight float64 `json:"singleWeight,omitempty"`
	Lines        []*Line `json:"lines"`
}

// Group is one weighted dialogue group.
type Group struct {
	Weight   float64   `json:"w"`
	Builtin  string    `json:"builtin,omitempty"`
	GIF      bool      `json:"gif,omitempty"`
	Variants []Variant `json:"variants,omitempty"`
	Lines    []*Line   `json:"lines,omitempty"`
}

// Selection is the result of a weighted draw.
type Selection struct {
	Builtin string  `json:"builtin,omitempty"`
	GIF     bool    `json:"gif,omitempty"`
	Lines   []*Line `json:"lines,omitempty"`
}

// Result is a loaded dialogue file.
type Result struct {
	File    string  `json:"file"`
	Groups  []Group `json:"groups"`
	Error   string  `json:"error,omitempty"`
	Created bool    `json:"created"`
}

// Default is written on first run. The README field is intentional because the
// file is user-editable.
var Default = map[string]any{
	"_README": []any{
		"随机台词配置：点击鲸鱼气泡后显示的台词从这里加权抽取。",
		"groups[].w / weight 是该组的相对权重；组内 variants 每个条目等概率。",
		"只想改几句时，也可以直接写顶层 lines: [{ text, weight, style }]，不必用 groups。",
		"variants 里的对象可用 w / weight 设置变体权重，并用 lines 覆盖整组台词。",
		"行格式: { t / text, s / style, c / color, w / wrap }；A=小标题 B=大字 C=灰色提示 P=中号时段。",
		"文本支持占位符: {today} 今日已用 {balance} 余额 {currency} 币种 {period} 时段。",
		"builtin: \"period\" 表示内置的「当前时间段 + 今日已用」动态内容。",
		"gif: true 表示显示皮肤包里的 rua.gif 动图。",
	},
	"groups": []any{
		map[string]any{"w": 45, "builtin": "period"},
		map[string]any{"w": 7, "variants": []any{"好模型... ↓", "好女孩...↓"}, "style": "B"},
		map[string]any{
			"w": 7, "style": "A", "wrap": true,
			"variants": []any{
				"不知道用户有什么用，先赶走吧~",
				"我...我...我也要挣钱吗？",
				"我去吃饭啦，测完叫我",
				"压力一只蓝色大肥鱼？！",
				"DeepSleep...",
				"坏了...用户彻底怒了！",
			},
		},
		map[string]any{"w": 10, "gif": true},
		map[string]any{
			"w": 3, "style": "A", "wrap": true,
			"variants": []any{
				"你目录里的dsh是什么...大烧货吗...?",
				"恭喜你实现token自由！token全跑了！",
				"真当我是便宜货啊...",
			},
		},
		map[string]any{"w": 1, "variants": []any{"哦鲸鲸... "}, "style": "B"},
	},
}

// Load reads and normalizes the configured dialogue file.
func Load(paths config.Paths, cfg config.Config) Result {
	file := cfg.ResolveLinesFile(paths)
	res := jsonc.Read(file)
	if res.Missing {
		err := jsonc.WriteAtomic(file, Default)
		return Result{File: file, Groups: Normalize(Default), Created: err == nil}
	}
	if res.Error != nil {
		return Result{
			File:   file,
			Groups: Normalize(Default),
			Error:  "台词文件语法错误: " + res.Error.Error(),
		}
	}
	return Result{File: file, Groups: Normalize(res.Value)}
}

// Normalize converts flexible user JSON into the renderer's stable shape.
func Normalize(raw map[string]any) []Group {
	groupsIn, ok := raw["groups"].([]any)
	if !ok {
		if flat, exists := raw["lines"].([]any); exists {
			groupsIn = []any{map[string]any{"w": 1, "variants": flat}}
		} else {
			groupsIn = Default["groups"].([]any)
		}
	}
	groups := make([]Group, 0, len(groupsIn))
	for _, item := range groupsIn {
		g, ok := item.(map[string]any)
		if !ok || boolean(g["enabled"], true) == false {
			continue
		}
		weight := nonNegativeNumber(first(g, "w", "weight"), 1)
		defaults := lineDefaults{
			Style: stringValue(first(g, "style", "s")),
			Color: stringValue(first(g, "color", "c")),
			Wrap:  boolean(g["wrap"], false),
		}
		group := Group{Weight: weight}
		if builtin, exists := g["builtin"]; exists {
			group.Builtin = stringValue(builtin)
			group.Lines = normalizeLineArray(g["lines"], defaults)
			groups = append(groups, group)
			continue
		}
		if boolean(g["gif"], false) || group.Builtin == "gif" {
			group.GIF = true
			groups = append(groups, group)
			continue
		}
		variantsIn, ok := g["variants"].([]any)
		if !ok {
			variantsIn, ok = g["lines"].([]any)
		}
		if !ok {
			continue
		}
		variants := make([]Variant, 0, len(variantsIn))
		for _, rawVariant := range variantsIn {
			if vm, ok := rawVariant.(map[string]any); ok {
				if lineArray, ok := vm["lines"].([]any); ok {
					n := normalizeLineArray(lineArray, defaults)
					if len(n) > 0 {
						variants = append(variants, Variant{
							Weight: nonNegativeNumber(first(vm, "w", "weight"), 1),
							Lines:  n,
						})
					}
					continue
				}
			}
			line := normalizeLine(rawVariant, defaults)
			if line == nil {
				continue
			}
			singleWeight := 1.0
			if vm, ok := rawVariant.(map[string]any); ok {
				singleWeight = nonNegativeNumber(first(vm, "w", "weight"), 1)
			}
			variants = append(variants, Variant{
				Weight:       1,
				SingleWeight: singleWeight,
				Lines:        []*Line{nil, line, nil},
			})
		}
		if len(variants) > 0 {
			group.Variants = variants
			groups = append(groups, group)
		}
	}
	if len(groups) == 0 {
		// The built-in file has groups and cannot recurse indefinitely.
		return Normalize(map[string]any{"groups": Default["groups"]})
	}
	return groups
}

type lineDefaults struct {
	Style string
	Color string
	Wrap  bool
}

func normalizeLineArray(raw any, defaults lineDefaults) []*Line {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]*Line, 0, len(items))
	hasLine := false
	for _, item := range items {
		line := normalizeLine(item, defaults)
		out = append(out, line)
		hasLine = hasLine || line != nil
	}
	if !hasLine {
		return nil
	}
	return out
}

func normalizeLine(raw any, defaults lineDefaults) *Line {
	if text, ok := raw.(string); ok {
		if text == "" {
			return nil
		}
		return &Line{Text: text, Style: validStyle(defaults.Style, "A"), Color: defaults.Color, Wrap: defaults.Wrap}
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	text := stringValue(first(obj, "t", "text"))
	if text == "" {
		return nil
	}
	wrapRaw := obj["w"]
	if wrapRaw == nil {
		wrapRaw = obj["wrap"]
	}
	return &Line{
		Text:  text,
		Style: validStyle(stringValue(first(obj, "s", "style")), validStyle(defaults.Style, "A")),
		Color: firstString(defaults.Color, obj["c"], obj["color"]),
		Wrap:  booleanDefault(wrapRaw, defaults.Wrap),
	}
}

func validStyle(value, fallback string) string {
	switch value {
	case "A", "B", "C", "P":
		return value
	default:
		return fallback
	}
}

// Pick draws one group and variant using relative weights.
func Pick(groups []Group) Selection {
	if len(groups) == 0 {
		return Selection{Builtin: "period"}
	}
	weighted := make([]weightedItem[Group], 0, len(groups))
	for _, group := range groups {
		weighted = append(weighted, weightedItem[Group]{Value: group, Weight: group.Weight})
	}
	group, ok := pickWeighted(weighted)
	if !ok {
		return Selection{Builtin: "period"}
	}
	if group.Builtin != "" {
		if group.Builtin == "gif" {
			return Selection{GIF: true}
		}
		return Selection{Builtin: group.Builtin, Lines: group.Lines}
	}
	if group.GIF {
		return Selection{GIF: true}
	}
	variantWeights := make([]weightedItem[Variant], 0, len(group.Variants))
	for _, variant := range group.Variants {
		weight := variant.Weight
		if variant.SingleWeight != 0 {
			weight = variant.SingleWeight
		}
		variantWeights = append(variantWeights, weightedItem[Variant]{Value: variant, Weight: weight})
	}
	variant, ok := pickWeighted(variantWeights)
	if !ok {
		return Selection{Builtin: "period"}
	}
	return Selection{Lines: variant.Lines}
}

type weightedItem[T any] struct {
	Value  T
	Weight float64
}

func pickWeighted[T any](items []weightedItem[T]) (T, bool) {
	var zero T
	total := 0.0
	for _, item := range items {
		if item.Weight > 0 {
			total += item.Weight
		}
	}
	if total <= 0 {
		return zero, false
	}
	draw := rand.Float64() * total
	for _, item := range items {
		if item.Weight <= 0 {
			continue
		}
		draw -= item.Weight
		if draw < 0 {
			return item.Value, true
		}
	}
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Weight > 0 {
			return items[i].Value, true
		}
	}
	return zero, false
}

func first(obj map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := obj[key]; ok {
			return value
		}
	}
	return nil
}

func firstString(fallback string, values ...any) string {
	for _, value := range values {
		if text := stringValue(value); text != "" {
			return text
		}
	}
	return fallback
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return ""
	}
}

func boolean(value any, fallback bool) bool {
	return booleanDefault(value, fallback)
}

func booleanDefault(value any, fallback bool) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(typed)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func nonNegativeNumber(value any, fallback float64) float64 {
	var number float64
	switch typed := value.(type) {
	case float64:
		number = typed
	case int:
		number = float64(typed)
	case int64:
		number = float64(typed)
	case jsonNumber:
		parsed, err := strconv.ParseFloat(string(typed), 64)
		if err == nil {
			number = parsed
		}
	case interface{ Float64() (float64, error) }:
		parsed, err := typed.Float64()
		if err == nil {
			number = parsed
		}
	}
	if number < 0 {
		return fallback
	}
	return number
}

// json.Number is accepted through its Float64 method, but this local type keeps
// the switch readable and testable.
type jsonNumber string

// ValidationError is returned by callers that need a typed error.
var ValidationError = errors.New("invalid lines configuration")
