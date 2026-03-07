package usagelimit

import (
	"regexp"
	"strconv"
	"time"
)

var (
	errorRe = regexp.MustCompile(
		`(?i)(exceeded your usage limit|exceeded your current quota|insufficient.{0,10}quota|usage limits.{0,60}try again after|rate limit.{0,60}retry after)`,
	)
	utcTimeRe  = regexp.MustCompile(`(?i)after (\d+):(\d+) UTC`)
	hoursRe    = regexp.MustCompile(`(?i)in (\d+) hours?`)
	minsRe     = regexp.MustCompile(`(?i)(\d+) minutes?`)
	secsRe     = regexp.MustCompile(`(?i)in (\d+) seconds?`)
	shortRe    = regexp.MustCompile(`(?i)in (\d+)m(\d+)s`)
	warn5hRe   = regexp.MustCompile(`(?i)(\d+)%\s+of\s+(?:your\s+)?5.?hour`)
	warnWeekRe = regexp.MustCompile(`(?i)(\d+)%\s+of\s+(?:your\s+)?week`)
)

// HasError reports whether text contains an API usage-limit message.
func HasError(text string) bool {
	return errorRe.MatchString(text)
}

// HasWarning reports whether text contains a usage percentage warning (approaching limit).
func HasWarning(text string) bool {
	return warn5hRe.MatchString(text) || warnWeekRe.MatchString(text)
}

// ExtractWarningLabel returns a short display label like "⚠️ 65%/5h 89%/wk" from warning text.
// Returns empty string if no warning is found.
func ExtractWarningLabel(text string) string {
	label := ""
	if m := warn5hRe.FindStringSubmatch(text); len(m) == 2 {
		label += m[1] + "%/5h "
	}
	if m := warnWeekRe.FindStringSubmatch(text); len(m) == 2 {
		label += m[1] + "%/wk"
	}
	if label == "" {
		return ""
	}
	return "⚠️ " + label
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

	// OpenAI short format: "in 1m30s"
	if m := shortRe.FindStringSubmatch(text); len(m) == 3 {
		mins, _ := strconv.Atoi(m[1])
		secs, _ := strconv.Atoi(m[2])
		return mins*60 + secs
	}

	// OpenAI seconds format: "in X seconds"
	if m := secsRe.FindStringSubmatch(text); len(m) == 2 {
		if secs, _ := strconv.Atoi(m[1]); secs > 0 {
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
