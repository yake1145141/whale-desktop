package balance

import (
	"testing"
	"time"
)

func TestIsPeakTime(t *testing.T) {
	// 2026-10-26 is a Monday. 10:00 Beijing is peak, 13:00 is off-peak.
	peak := time.Date(2026, 10, 26, 10, 0, 0, 0, shanghai).Unix()
	offPeak := time.Date(2026, 10, 26, 13, 0, 0, 0, shanghai).Unix()
	if !IsPeakTime(peak) {
		t.Fatal("10:00 should be peak")
	}
	if IsPeakTime(offPeak) {
		t.Fatal("13:00 should be off-peak")
	}
}
