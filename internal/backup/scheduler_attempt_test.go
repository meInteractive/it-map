package backup

import (
	"bytes"
	"errors"
	"testing"
)

func TestSchedulerAttemptProjectionBoundary(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"type":1}`, `{"type":"diagnostic","key_encrypted":"CI_PRIVATE"}`, `{"type":"diagnostic","result_json":{}}`} {
		if _, e := PrepareSchedulerAttemptJSON([]byte(raw)); !errors.Is(e, ErrSchedulerAttemptProjection) {
			t.Fatal("unreviewed selected row accepted")
		}
	}
	input := []byte(`{"type":"classify_title","message":"CI public","result_json":"{\"accepted\":true,\"prompt\":\"CI_PRIVATE_PROMPT\"}"}`)
	before := append([]byte(nil), input...)
	got, e := PrepareSchedulerAttemptJSON(input)
	if e != nil || bytes.Contains(got, []byte("CI_PRIVATE")) || bytes.Contains(got, []byte("result_json")) || !bytes.Contains(got, []byte(`"outcome":{"accepted":true}`)) || !bytes.Equal(input, before) {
		t.Fatal("attempt projection leaked raw result or mutated input")
	}
}
