package retryafter

import (
	"net/http"
	"testing"
	"time"
)

func TestParseResponseClockSkew(t *testing.T) {
	server := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	date := func(t time.Time) string { return t.Format(http.TimeFormat) }
	tests := []struct {
		name, raw, serverDate string
		now                   time.Time
		want                  time.Duration
	}{
		{"local clock ahead", date(server.Add(3 * time.Minute)), date(server), server.Add(2 * time.Minute), 3 * time.Minute},
		{"local clock behind", date(server.Add(3 * time.Minute)), date(server), server.Add(-2 * time.Minute), 5 * time.Minute},
		{"delta seconds", "180", date(server), server.Add(2 * time.Minute), 3 * time.Minute},
		{"invalid server date", date(server.Add(3 * time.Minute)), "invalid", server.Add(2 * time.Minute), time.Minute},
		{"bounded provider delay", date(server.Add(48 * time.Hour)), date(server), server.Add(47 * time.Hour), MaxDelay},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseResponse(tc.raw, tc.serverDate, tc.now); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
