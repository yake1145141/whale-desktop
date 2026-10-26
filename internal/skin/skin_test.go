package skin

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"whale-desktop/internal/config"
	"whale-desktop/internal/jsonc"
)

func TestLoadCustomSkinUsesDefaultGeometry(t *testing.T) {
	skinsDir := t.TempDir()
	dir := filepath.Join(skinsDir, "custom")
	if err := os.MkdirAll(filepath.Join(dir, "sounds"), 0o755); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "whale.png"), encoded.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sounds", "press.mp3"), []byte("mp3"), 0o644); err != nil {
		t.Fatal(err)
	}
	meta := map[string]any{
		"displayName": "测试皮肤",
		"image":       "whale.png",
		"sounds": map[string]any{
			"test": map[string]any{"label": "测试", "press": "sounds/press.mp3"},
		},
	}
	if err := jsonc.WriteAtomic(filepath.Join(dir, "skin.jsonc"), meta); err != nil {
		t.Fatal(err)
	}
	loaded := LoadDir(config.Paths{Skins: skinsDir}, "custom")
	if loaded == nil {
		t.Fatal("skin did not load")
	}
	if loaded.Geometry.Bubble.VBox.W != 1026 || loaded.Geometry.WindowPadding != 16 {
		t.Fatalf("geometry = %#v", loaded.Geometry)
	}
	payload := ToPayload(loaded)
	if len(payload.ImageURL) < 20 || len(payload.Sounds["test"].Press) < 20 {
		t.Fatal("assets were not converted to data URLs")
	}
}
