package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"itmap/internal/workerengine"
)

func TestReleaseControlRequiresMatchingHoldAndInstance(t *testing.T) {
	gate := workerengine.NewPauseGate()
	hold := "release-123"
	control := releaseControl{
		gate: gate, instance: "current",
		readHold: func() (string, error) { return hold, nil },
	}
	request := func(path, instance, token string, status int, state string) {
		t.Helper()
		recorder := httptest.NewRecorder()
		body, err := json.Marshal(releaseRequest{Instance: instance, Token: token})
		if err != nil {
			t.Fatal(err)
		}
		control.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body))))
		if recorder.Code != status {
			t.Fatalf("%s: status %d, want %d", path, recorder.Code, status)
		}
		if state != "" {
			var result map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result["state"] != state {
				t.Fatalf("%s: unexpected state %q, %v", path, recorder.Body.String(), err)
			}
		}
	}

	request("/pause", "previous", hold, http.StatusBadRequest, "")
	request("/pause", "current", "wrong", http.StatusConflict, "")
	request("/pause", "current", hold, http.StatusOK, "held")
	request("/pause", "current", hold, http.StatusOK, "held")
	request("/resume", "current", hold, http.StatusConflict, "")
	if !gate.Paused() {
		t.Fatal("hold did not survive a resume request")
	}
	token := hold
	hold = ""
	request("/resume", "current", token, http.StatusOK, "resumed")
	request("/resume", "current", token, http.StatusOK, "resumed")
	if gate.Paused() {
		t.Fatal("gate stayed paused after verified resume")
	}
}

func TestReleasePauseReportsLiveButUnready(t *testing.T) {
	gate := workerengine.NewPauseGate()
	monitor := workerengine.NewMonitor(1)
	monitor.SetPauseGate(gate)
	if _, err := gate.Pause("release"); err != nil {
		t.Fatal(err)
	}
	state := monitor.Snapshot()
	if !state.Live || state.Ready || state.Reason != "release_paused" {
		t.Fatalf("unexpected paused state: %+v", state)
	}
}
