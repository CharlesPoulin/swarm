package usagelimit_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/cpoulin/claude-swarm/internal/usagelimit"
)

func TestHasError(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"You have exceeded your usage limit for today.", true},
		{"API usage limits — try again after 14:00 UTC", true},
		{"Rate limit exceeded, retry after 1 hour", true},
		{"insufficient_quota", true},
		{"ok", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.text, func(t *testing.T) {
			if got := usagelimit.HasError(tc.text); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtractWaitSecs(t *testing.T) {
	t.Run("UTCTimestamp", func(t *testing.T) {
		future := time.Now().UTC().Add(2 * time.Hour)
		text := fmt.Sprintf("after %02d:%02d UTC", future.Hour(), future.Minute())

		got := usagelimit.ExtractWaitSecs(text)
		if got < 7140 || got > 7260 {
			t.Errorf("got %d, want ~7200", got)
		}
	})

	t.Run("Durations", func(t *testing.T) {
		cases := []struct {
			text string
			want int
		}{
			{"in 1 hours 30 minutes", 5400},
			{"in 2 hours", 7200},
			{"retry in 30 seconds", 30},
			{"in 1m30s", 90},
		}
		for _, tc := range cases {
			t.Run(tc.text, func(t *testing.T) {
				if got := usagelimit.ExtractWaitSecs(tc.text); got != tc.want {
					t.Errorf("got %d, want %d", got, tc.want)
				}
			})
		}
	})

	t.Run("Fallback", func(t *testing.T) {
		got := usagelimit.ExtractWaitSecs("none")
		if got != 3600 {
			t.Errorf("got %d, want 3600", got)
		}
	})
}
