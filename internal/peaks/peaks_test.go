package peaks

import "testing"

func TestNormalizeGeneratesUniqueIDs(t *testing.T) {
	got := normalize(map[string]any{
		"presets": []any{
			map[string]any{"name": "work time", "peak": "忙", "offPeak": "闲"},
			map[string]any{"name": "work time", "peak": "峰", "offPeak": "谷"},
		},
	})
	if len(got) != 2 {
		t.Fatalf("presets = %#v", got)
	}
	if got[0].ID == got[1].ID || got[1].ID != got[0].ID+"-2" {
		t.Fatalf("ids = %q, %q", got[0].ID, got[1].ID)
	}
}
