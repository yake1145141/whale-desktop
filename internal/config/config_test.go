package config

import (
	"os"
	"path/filepath"
	"testing"

	"whale-desktop/internal/jsonc"
)

func TestLoadDefaultsAndApplySparsePatch(t *testing.T) {
	root := t.TempDir()
	paths := Paths{Root: root, Config: filepath.Join(root, "config.jsonc")}
	if err := WriteUser(paths, map[string]any{"scale": 1.8, "displayMode": "time"}); err != nil {
		t.Fatal(err)
	}
	loaded := Load(paths)
	if loaded.Error != "" {
		t.Fatal(loaded.Error)
	}
	if loaded.Config.Scale != 1.8 || loaded.Config.DisplayMode != "time" {
		t.Fatalf("loaded config = %#v", loaded.Config)
	}
	if loaded.Config.Anchor.H != "right" || loaded.Config.Anchor.V != "bottom" {
		t.Fatalf("defaults were not merged: %#v", loaded.Config.Anchor)
	}
	updated, err := Apply(paths, loaded, map[string]any{"soundSet": "fx1"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Config.SoundSet != "fx1" {
		t.Fatalf("sound set = %q", updated.Config.SoundSet)
	}
	if _, exists := updated.User["refreshMs"]; exists {
		t.Fatal("default fields must not be written into the user config")
	}
	text, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jsonc.Parse(string(text))
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := parsed["scale"]; !exists {
		t.Fatal("patch was not persisted")
	}
}
