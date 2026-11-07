package parser

import (
	"database/sql"
	"testing"
	"time"
)

func TestIssuesClassificationAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		code, want string
	}{
		{"roles_pending", "roles_pending"}, {"stage-failed", "stage-failed"},
		{"", "не указана"}, {"private/trace", "unknown"},
		{"CI_PRIVATE_KEY", "unknown"},
	} {
		if got := issueReason(tc.code); got != tc.want {
			t.Fatalf("reason %q: %q != %q", tc.code, got, tc.want)
		}
	}
	const now = int64(1794000000000)
	success := sql.NullString{String: time.UnixMilli(now+1).UTC().Format(time.RFC3339Nano), Valid: true}
	before := sql.NullString{String: time.UnixMilli(now-1).UTC().Format(time.RFC3339Nano), Valid: true}
	for _, tc := range []struct {
		kind string
		at sql.NullString
		want bool
	}{
		{"scan_source", success, true},
		{"scan_source", before, false},
		{"extract_vacancy", success, false},
		{"scan_source", sql.NullString{String: "invalid", Valid: true}, false},
		{"scan_source", sql.NullString{}, false},
	} {
		if got := issueRecovered(tc.kind, now, tc.at); got != tc.want {
			t.Fatalf("recovery %q/%q: %v != %v", tc.kind, tc.at.String, got, tc.want)
		}
	}
	if !expectedIssueWait["apify_running"] || expectedIssueWait["extraction"] {
		t.Fatal("routine waits must not become P2 incidents")
	}
}
