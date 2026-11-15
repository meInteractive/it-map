// Package retryafter bounds provider-controlled HTTP retry hints.
package retryafter

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const MinDelay = time.Minute
const MaxDelay = 24 * time.Hour

// Parse accepts delta-seconds or an HTTP date and never returns an unbounded wait.
func Parse(raw string, now time.Time) time.Duration {
	raw = strings.TrimSpace(raw)
	var d time.Duration
	if seconds, err := strconv.ParseUint(raw, 10, 64); err == nil {
		if seconds > uint64(MaxDelay/time.Second) {
			return MaxDelay
		}
		d = time.Duration(seconds) * time.Second
	} else if date, err := http.ParseTime(raw); err == nil {
		d = date.Sub(now)
	} else {
		if raw != "" && strings.Trim(raw, "0123456789") == "" {
			return MaxDelay
		}
		return MinDelay
	}
	if d < MinDelay {
		return MinDelay
	}
	if d > MaxDelay {
		return MaxDelay
	}
	return d
}

// ParseResponse accounts for clock skew using the provider's Date header.
// The longer delay is conservative and remains bounded by MaxDelay.
func ParseResponse(raw, serverDate string, receivedAt time.Time) time.Duration {
	delay := Parse(raw, receivedAt)
	if date, err := http.ParseTime(serverDate); err == nil {
		if providerDelay := Parse(raw, date); providerDelay > delay {
			return providerDelay
		}
	}
	return delay
}
