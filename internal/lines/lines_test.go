package lines

import "testing"

func TestNormalizeFlatLines(t *testing.T) {
	groups := Normalize(map[string]any{"lines": []any{
		map[string]any{"text": "余额 {balance}", "weight": 9, "style": "B"},
		"simple",
	}})
	if len(groups) != 1 || len(groups[0].Variants) != 2 {
		t.Fatalf("groups = %#v", groups)
	}
	if groups[0].Variants[0].SingleWeight != 9 {
		t.Fatalf("weight = %v", groups[0].Variants[0].SingleWeight)
	}
	if groups[0].Variants[1].Lines[1].Style != "A" {
		t.Fatalf("line = %#v", groups[0].Variants[1].Lines[1])
	}
}

func TestPickDisablesZeroWeight(t *testing.T) {
	groups := []Group{
		{Weight: 0, Variants: []Variant{{Weight: 1, Lines: []*Line{{Text: "no"}}}}},
		{Weight: 1, Variants: []Variant{{Weight: 1, Lines: []*Line{{Text: "yes"}}}}},
	}
	for i := 0; i < 50; i++ {
		got := Pick(groups)
		if len(got.Lines) == 0 || got.Lines[0].Text != "yes" {
			t.Fatalf("unexpected selection: %#v", got)
		}
	}
}
