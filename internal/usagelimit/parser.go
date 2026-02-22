package usagelimit

import (
	"regexp"
	"strconv"
	"time"
)

var (
	errorRe = regexp.MustCompile(
		`(?i)(exceeded your usage limit|usage limits.{0,60}try again after|rate limit.{0,60}retry after)`,
	)
	utcTimeRe = regexp.MustCompile(`(?i)after (\d+):(\d+) UTC`)
	hoursRe   = regexp.MustCompile(`(?i)in (\d+) hours?`)
	minsRe    = regexp.MustCompile(`(?i)(\d+) minutes?`)
)

// HasError reports whether text contains an API usage-limit message.
func HasError(text string) bool {
	return errorRe.MatchString(text)
}

// ExtractWaitSecs parses the wait duration from error text and returns seconds.
// Priority: UTC timestamp → "in X hours Y minutes" → 3600 fallback.
func ExtractWaitSecs(text string) int {
	// Primary: "after HH:MM UTC" — compute delta from now to that wall-clock time (UTC)
	if m := utcTimeRe.FindStringSubmatch(text); len(m) == 3 {
		h, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])

		now := time.Now().UTC()
		target := time.Date(now.Year(), now.Month(), now.Day(), h, min, 0, 0, time.UTC)
		if !target.After(now) {
			target = target.Add(24 * time.Hour) // already passed → next day
		}
		secs := int(target.Sub(now).Seconds())
		if secs > 0 {
			return secs
		}
	}

	// Fallback: "in X hours Y minutes"
	hours, mins := 0, 0
	if m := hoursRe.FindStringSubmatch(text); len(m) == 2 {
		hours, _ = strconv.Atoi(m[1])
	}
	if hours > 0 {
		if m := minsRe.FindStringSubmatch(text); len(m) == 2 {
			mins, _ = strconv.Atoi(m[1])
		}
	}
	if hours > 0 || mins > 0 {
		return hours*3600 + mins*60
	}

	return 3600 // default: 1 hour
}
