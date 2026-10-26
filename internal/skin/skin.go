package skin

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"whale-desktop/internal/config"
	"whale-desktop/internal/jsonc"
)

// Box is a rectangle expressed as ratios of the bubble width.
type Box struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
}

type VBox struct {
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type Bubble struct {
	Width  float64 `json:"width"`
	VBox   VBox    `json:"vbox"`
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Aspect float64 `json:"aspect,omitempty"`
}

type Text struct {
	LeftPct float64 `json:"leftPct"`
	TopPct  float64 `json:"topPct"`
	Anchor  string  `json:"anchor"`
}

type Font struct {
	Label  float64 `json:"label"`
	Amount float64 `json:"amount"`
	Period float64 `json:"period"`
	Hint   float64 `json:"hint"`
}

type GIF struct {
	LeftPct float64 `json:"leftPct"`
	TopPct  float64 `json:"topPct"`
	MaxW    float64 `json:"maxW"`
	MaxH    float64 `json:"maxH"`
}

type MenuButton struct {
	TopRatio float64 `json:"topRatio"`
	Right    float64 `json:"right"`
	Size     float64 `json:"size"`
}

type LockButton struct {
	Size    float64 `json:"size"`
	OffsetX float64 `json:"offsetX"`
	OffsetY float64 `json:"offsetY"`
}

// Geometry controls the complete composition of a skin.
type Geometry struct {
	Img           Box        `json:"img"`
	Bubble        Bubble     `json:"bubble"`
	Text          Text       `json:"text"`
	Font          Font       `json:"font"`
	WrapMax       float64    `json:"wrapMax"`
	GIF           GIF        `json:"gif"`
	MenuBtn       MenuButton `json:"menuBtn"`
	LockBtn       LockButton `json:"lockBtn"`
	WindowPadding float64    `json:"windowPadding"`
}

// Sound is a press/release pair inside a skin.
type Sound struct {
	Label   string `json:"label"`
	Press   string `json:"press"`
	Release string `json:"release"`
}

// Skin is one loadable skin pack.
type Skin struct {
	Name        string           `json:"name"`
	Dir         string           `json:"-"`
	DisplayName string           `json:"displayName"`
	Description string           `json:"description"`
	Author      string           `json:"author"`
	ImagePath   string           `json:"-"`
	GIFPath     string           `json:"-"`
	Sounds      map[string]Sound `json:"sounds"`
	Geometry    Geometry         `json:"geometry"`
}

// Summary is the compact skin list sent to menus.
type Summary struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

// Payload is the renderer representation. Assets are embedded as data URLs so
// custom skins work without a privileged custom URL scheme.
type Payload struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"displayName"`
	Description string            `json:"description"`
	Geometry    Geometry          `json:"geometry"`
	ImageURL    string            `json:"imageUrl"`
	GIFURL      string            `json:"gifUrl"`
	Sounds      map[string]Sound  `json:"sounds"`
	ImageMime   string            `json:"imageMime"`
	ImagePath   string            `json:"-"`
	GIFPath     string            `json:"-"`
	Extra       map[string]string `json:"-"`
}

var baseGeometry = map[string]any{
	"img": map[string]any{"width": 0.5968, "height": 0.5968, "right": 0, "bottom": 0},
	"bubble": map[string]any{
		"width": 0.7143, "vbox": map[string]any{"w": 1026, "h": 700}, "left": 0.02, "top": -0.46,
	},
	"text":          map[string]any{"leftPct": 0.4425, "topPct": 0.35, "anchor": "center"},
	"font":          map[string]any{"label": 0.0643, "amount": 0.1248, "period": 0.1014, "hint": 0.0546},
	"wrapMax":       0.5458,
	"gif":           map[string]any{"leftPct": 0.4425, "topPct": 0.44, "maxW": 0.5458, "maxH": 0.3899},
	"menuBtn":       map[string]any{"topRatio": 0.4055, "right": 4, "size": 30},
	"lockBtn":       map[string]any{"size": 28, "offsetX": 0, "offsetY": 0},
	"windowPadding": 16,
}

var baseMeta = map[string]any{
	"name":        "default",
	"displayName": "默认 · 小鲸鱼",
	"description": "原版 DSH 插件的小鲸鱼与气泡几何",
	"author":      "",
	"sounds": map[string]any{
		"duck": map[string]any{
			"label": "小黄鸭", "press": "sounds/Ya1.mp3", "release": "sounds/Ya2.mp3",
		},
		"fx1": map[string]any{
			"label": "音效1", "press": "sounds/D1.mp3", "release": "sounds/D2.mp3",
		},
	},
}

// InstallDefault materializes the bundled skin once without overwriting user
// edits on later launches.
func InstallDefault(paths config.Paths, assets fs.FS) error {
	dir := filepath.Join(paths.Skins, "default")
	if err := os.MkdirAll(filepath.Join(dir, "sounds"), 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"DSniang1.png": "whale.png",
		"rua.gif":      "rua.gif",
	}
	for source, destination := range files {
		if err := copyIfMissing(assets, source, filepath.Join(dir, destination)); err != nil {
			return err
		}
	}
	for _, name := range []string{"Ya1.mp3", "Ya2.mp3", "D1.mp3", "D2.mp3"} {
		if err := copyIfMissing(assets, name, filepath.Join(dir, "sounds", name)); err != nil {
			return err
		}
	}
	metaFile := filepath.Join(dir, "skin.jsonc")
	if _, err := os.Stat(metaFile); err == nil {
		return nil
	}
	meta := map[string]any{
		"_README": []any{
			"皮肤包定义。本文件只描述「这个皮肤是什么」；geometry 不写时继承 Go 程序内置默认几何。",
			"所有比例都是「占气泡宽度的比例」；bubble.left 控制气泡相对立绘的横向位置。",
			"文字默认 anchor=center，leftPct/topPct 是文字块中心在气泡内的百分比位置。",
		},
		"name":        "default",
		"displayName": "默认 · 小鲸鱼",
		"description": "原版 DSH 插件的小鲸鱼与气泡构图",
		"author":      "",
		"image":       "whale.png",
		"gif":         "rua.gif",
		"sounds": map[string]any{
			"duck": map[string]any{"label": "小黄鸭", "press": "sounds/Ya1.mp3", "release": "sounds/Ya2.mp3"},
			"fx1":  map[string]any{"label": "音效1", "press": "sounds/D1.mp3", "release": "sounds/D2.mp3"},
		},
	}
	return jsonc.WriteAtomic(metaFile, meta)
}

func copyIfMissing(assets fs.FS, source, destination string) error {
	if _, err := os.Stat(destination); err == nil {
		return nil
	}
	data, err := fs.ReadFile(assets, source)
	if err != nil {
		return fmt.Errorf("embedded asset %s: %w", source, err)
	}
	return os.WriteFile(destination, data, 0o644)
}

// List returns every valid skin, default first.
func List(paths config.Paths) []Skin {
	entries, err := os.ReadDir(paths.Skins)
	if err != nil {
		return nil
	}
	var out []Skin
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if loaded := LoadDir(paths, entry.Name()); loaded != nil {
			out = append(out, *loaded)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == "default" {
			return true
		}
		if out[j].Name == "default" {
			return false
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// Load returns the named skin, falling back safely.
func Load(paths config.Paths, name string) *Skin {
	direct := LoadDir(paths, name)
	if direct != nil {
		return direct
	}
	fallback := LoadDir(paths, "default")
	if fallback != nil {
		return fallback
	}
	all := List(paths)
	if len(all) > 0 {
		return &all[0]
	}
	return nil
}

// LoadDir parses one directory.
func LoadDir(paths config.Paths, name string) *Skin {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return nil
	}
	dir := filepath.Join(paths.Skins, name)
	res := jsonc.Read(filepath.Join(dir, "skin.jsonc"))
	if res.Missing || res.Error != nil {
		paths.Log("skin.jsonc parse error", name, res.Error)
		return nil
	}
	raw := res.Value
	metaMap := jsonc.DeepMerge(baseMeta, raw)
	if geometry, ok := raw["geometry"].(map[string]any); ok {
		metaMap["geometry"] = jsonc.DeepMerge(baseGeometry, geometry)
	} else {
		metaMap["geometry"] = baseGeometry
	}
	var geometry Geometry
	if err := jsonc.Decode(metaMap["geometry"], &geometry); err != nil {
		paths.Log("skin geometry error", name, err)
		return nil
	}
	imagePath, err := resolveAsset(dir, text(raw["image"]),
		[]string{"whale.png", "whale.gif", "DSniang1.png", "DSniang02.png"})
	if err != nil || imagePath == "" {
		paths.Log("skin has no usable image", name)
		return nil
	}
	gifPath, _ := resolveAsset(dir, text(raw["gif"]), []string{"rua.gif"})
	sounds := map[string]Sound{}
	if rawSounds, ok := metaMap["sounds"].(map[string]any); ok {
		for key, rawSound := range rawSounds {
			obj, ok := rawSound.(map[string]any)
			if !ok {
				continue
			}
			press := resolveOptional(dir, text(obj["press"]))
			if press == "" {
				continue
			}
			sounds[key] = Sound{
				Label: firstNonEmpty(text(obj["label"]), key),
				Press: press, Release: resolveOptional(dir, text(obj["release"])),
			}
		}
	}
	return &Skin{
		Name: name, Dir: dir,
		DisplayName: firstNonEmpty(text(metaMap["displayName"]), name),
		Description: text(metaMap["description"]),
		Author:      text(metaMap["author"]),
		ImagePath:   imagePath, GIFPath: gifPath,
		Sounds: sounds, Geometry: geometry,
	}
}

func resolveAsset(dir, configured string, candidates []string) (string, error) {
	if configured != "" {
		return resolveOptional(dir, configured), nil
	}
	for _, name := range candidates {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", errors.New("asset not found")
}

func resolveOptional(dir, configured string) string {
	if configured == "" {
		return ""
	}
	path := filepath.Clean(filepath.Join(dir, filepath.FromSlash(configured)))
	rel, err := filepath.Rel(dir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	return path
}

// Summaries returns menu-facing metadata.
func Summaries(items []Skin) []Summary {
	out := make([]Summary, 0, len(items))
	for _, item := range items {
		out = append(out, Summary{Name: item.Name, DisplayName: item.DisplayName, Description: item.Description})
	}
	return out
}

// ToPayload loads all assets as browser-compatible data URLs.
func ToPayload(item *Skin) *Payload {
	if item == nil {
		return nil
	}
	payload := &Payload{
		Name: item.Name, DisplayName: item.DisplayName,
		Description: item.Description, Geometry: item.Geometry,
		Sounds: item.Sounds, ImagePath: item.ImagePath, GIFPath: item.GIFPath,
	}
	payload.ImageURL, payload.ImageMime = fileDataURL(item.ImagePath)
	payload.GIFURL, _ = fileDataURL(item.GIFPath)
	if payload.Sounds == nil {
		payload.Sounds = map[string]Sound{}
	}
	for key, sound := range payload.Sounds {
		sound.Press, _ = fileDataURL(sound.Press)
		sound.Release, _ = fileDataURL(sound.Release)
		payload.Sounds[key] = sound
	}
	return payload
}

func fileDataURL(path string) (string, string) {
	if path == "" {
		return "", ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	mimeType := mime.TypeByExtension(filepath.Ext(path))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), mimeType
}

func text(value any) string {
	if value == nil {
		return ""
	}
	if stringValue, ok := value.(string); ok {
		return stringValue
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
