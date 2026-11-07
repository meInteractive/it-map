package apifyruns

import (
	"strings"
	"testing"
)

func TestApifyDebugHistoryIdentityNeverLeaksLegacyValues(t *testing.T) {
	native := strings.Repeat("a", 32)
	if got := safeHistoryOperationID(native); got != native {
		t.Fatal("native operation identity changed")
	}
	private := "legacy-user-PRIVATE_SECRET?actor=https://example.invalid/path"
	got := safeHistoryOperationID(private)
	if got == private || strings.Contains(got,"PRIVATE") || strings.Contains(got,"http") ||
		!strings.HasPrefix(got,"legacy_") || len(got) != 31 ||
		safeHistoryOperationID(private) != got || safeHistoryOperationID(private+"2") == got {
		t.Fatal("unsafe/unstable historical identity")
	}
	for _, state := range []string{"starting","running","uncertain","succeeded","failed"} {
		if !validApifyState(state) { t.Fatalf("valid state %q rejected",state) }
	}
	for _, state := range []string{"", "private-value", "SUCCEEDED", "reconciled"} {
		if validApifyState(state) { t.Fatalf("unknown state %q accepted",state) }
	}
	for _, code := range []string{"apify_failed","apify_uncertain"} {
		if !safeApifyErrorCode.MatchString(code) {t.Fatal("known error code rejected")}
	}
	for _, code := range []string{"", "PRIVATE_SECRET", "apify_?token=secret", "apify_https://server"} {
		if safeApifyErrorCode.MatchString(code) {t.Fatal("private error code accepted")}
	}
}
