package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"itmap/internal/apifyruns"
)

type fakeBackend struct {
	rows                   []candidate
	results                []error
	inspect, apply, closed int
}

func (f *fakeBackend) List(context.Context) ([]candidate, error) { return f.rows, nil }
func (f *fakeBackend) Inspect(context.Context, candidate) error {
	i := f.inspect
	f.inspect++
	if i < len(f.results) {
		return f.results[i]
	}
	return nil
}
func (f *fakeBackend) Apply(context.Context, candidate) error {
	i := f.apply
	f.apply++
	if i < len(f.results) {
		return f.results[i]
	}
	return nil
}
func (f *fakeBackend) Close() error { f.closed++; return nil }

func TestCommandRequiresExplicitApplyAndNeverPrintsPrivateErrors(t *testing.T) {
	env := map[string]string{"ITMAP_RECOVERY_ENABLED": "1"}
	get := func(k string) string { return env[k] }
	f := &fakeBackend{rows: []candidate{{id: "private_id"}, {id: "another_private_id"}}, results: []error{errors.New("private_secret"), nil}}
	opens := 0
	open := func(context.Context, func(string) string) (recoveryBackend, error) { opens++; return f, nil }
	var out bytes.Buffer
	if code := run(context.Background(), []string{"apply"}, get, open, &out); code != 2 || opens != 0 || f.apply != 0 {
		t.Fatal("apply enabled without explicit guard", code)
	}
	out.Reset()
	env["ITMAP_RECOVERY_APPLY"] = "1"
	if code := run(context.Background(), []string{"apply"}, get, open, &out); code != 2 || opens != 0 || f.apply != 0 {
		t.Fatal("apply enabled without quiescence acknowledgment", code)
	}
	out.Reset()
	if code := run(context.Background(), []string{"inspect"}, get, open, &out); code != 1 || f.inspect != 2 || f.apply != 0 || f.closed != 1 {
		t.Fatal("inspect failed", code)
	}
	if got := out.String(); got != "confirmed=1 blocked=1 commit_unknown=0\n" || strings.Contains(got, "private") {
		t.Fatal("unsafe output", got)
	}
}
func TestCommandStopsBatchWhenCommitOutcomeUnknown(t *testing.T) {
	env := map[string]string{"ITMAP_RECOVERY_ENABLED": "1", "ITMAP_RECOVERY_APPLY": "1", "ITMAP_RECOVERY_QUIESCED": "1"}
	get := func(k string) string { return env[k] }
	f := &fakeBackend{rows: []candidate{{id: "first"}, {id: "second"}}, results: []error{apifyruns.ErrRecoveryCommitUnknown}}
	var out bytes.Buffer
	code := run(context.Background(), []string{"apply"}, get, func(context.Context, func(string) string) (recoveryBackend, error) { return f, nil }, &out)
	if code != 1 || f.apply != 1 || f.closed != 1 || out.String() != "confirmed=0 blocked=0 commit_unknown=1\n" {
		t.Fatal("ambiguous commit was not fenced", code, out.String())
	}
}
func TestCommandRejectsExcessAndUnknownMode(t *testing.T) {
	get := func(k string) string {
		if k == "ITMAP_RECOVERY_ENABLED" {
			return "1"
		}
		return ""
	}
	f := &fakeBackend{rows: make([]candidate, maxOperations+1)}
	opens := 0
	open := func(context.Context, func(string) string) (recoveryBackend, error) { opens++; return f, nil }
	var out bytes.Buffer
	if code := run(context.Background(), []string{"unknown"}, get, open, &out); code != 2 || opens != 0 {
		t.Fatal("unknown mode admitted")
	}
	if code := run(context.Background(), []string{"inspect"}, get, open, &out); code != 1 || f.inspect != 0 || out.String() != "recovery_limit_exceeded\n" {
		t.Fatal("unbounded batch admitted", code, out.String())
	}
}
