package usagelimit

import (
	"fmt"
	"testing"
	"time"
)

func TestHasError(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"You have exceeded your usage limit for today.", true},
		{"API usage limits — try again after 14:00 UTC", true},
		{"Rate limit exceeded, retry after 1 hour", true},
		{"You exceeded your current quota, please check your plan and billing details", true},
		{"insufficient_quota: you have run out of credits", true},
		{"Everything is fine, carry on.", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			if got := HasError(tc.text); got != tc.want {
				t.Errorf("HasError(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestExtractWaitSecs_UTCTimestamp(t *testing.T) {
	// Build a UTC timestamp 2 hours in the future so it's reliably in the future.
	future := time.Now().UTC().Add(2 * time.Hour)
	text := fmt.Sprintf("try again after %02d:%02d UTC", future.Hour(), future.Minute())

	got := ExtractWaitSecs(text)
	// Should be approximately 2 hours (7200s) ± 60s for test execution time.
	if got < 7140 || got > 7260 {
		t.Errorf("ExtractWaitSecs(UTC) = %d, want ~7200", got)
	}
}

func TestExtractWaitSecs_RelativeDuration(t *testing.T) {
	cases := []struct {
		text    string
		wantMin int
		wantMax int
	}{
		{"in 1 hours 30 minutes", 5400, 5400},
		{"in 2 hours", 7200, 7200},
		{"in 0 hours 45 minutes", 3600, 3600}, // 0 hours → fallback (no hours match)
		{"no duration here", 3600, 3600},       // default fallback
		{"rate limit exceeded, retry in 30 seconds", 30, 30},
		{"retry in 1m30s", 90, 90},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			got := ExtractWaitSecs(tc.text)
			if got < tc.wantMin || got > tc.wantMax {
				t.Errorf("ExtractWaitSecs(%q) = %d, want [%d, %d]", tc.text, got, tc.wantMin, tc.wantMax)
			}
		})
	}
}

func TestExtractWaitSecs_Fallback(t *testing.T) {
	got := ExtractWaitSecs("exceeded your usage limit")
	if got != 3600 {
		t.Errorf("ExtractWaitSecs fallback = %d, want 3600", got)
	}
}
