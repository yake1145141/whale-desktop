package ledger

import (
	"path/filepath"
	"testing"

	"whale-desktop/internal/config"
)

func TestRecordUsage(t *testing.T) {
	root := t.TempDir()
	paths := config.Paths{Usage: filepath.Join(root, ".usage.json")}
	first := Record(paths, 10, "CNY")
	if first.TodayUsage != 0 {
		t.Fatalf("first usage = %v", first.TodayUsage)
	}
	second := Record(paths, 7.5, "CNY")
	if second.TodayUsage != 2.5 {
		t.Fatalf("drop was not recorded: %v", second.TodayUsage)
	}
	third := Record(paths, 7.2, "USD")
	if third.TodayUsage != 2.5 {
		t.Fatalf("currency change should reset baseline only: %v", third.TodayUsage)
	}
}
