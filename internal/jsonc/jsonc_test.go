package jsonc

import "testing"

func TestParseJSONC(t *testing.T) {
	got, err := Parse(`{
		// comment
		"url": "https://example.com/a//b",
		"items": [1, 2,],
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if got["url"] != "https://example.com/a//b" {
		t.Fatalf("url changed: %#v", got["url"])
	}
	if len(got["items"].([]any)) != 2 {
		t.Fatalf("items = %#v", got["items"])
	}
}

func TestDeepMerge(t *testing.T) {
	base := map[string]any{"a": map[string]any{"x": 1, "y": 2}, "list": []any{1}}
	patch := map[string]any{"a": map[string]any{"y": 3}, "list": []any{2}}
	got := DeepMerge(base, patch)
	if got["a"].(map[string]any)["x"] != 1 || got["a"].(map[string]any)["y"] != 3 {
		t.Fatalf("nested merge = %#v", got["a"])
	}
	if len(got["list"].([]any)) != 1 || got["list"].([]any)[0] != 2 {
		t.Fatalf("array should be replaced: %#v", got["list"])
	}
}
