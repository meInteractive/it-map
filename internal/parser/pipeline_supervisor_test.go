package parser

import (
	"context"
	"errors"
	"testing"
	"time"

	"itmap/internal/workerloop"
)

type supervisorFake struct {
	recoveries, sessions int
	cancel               context.CancelFunc
}

func (f *supervisorFake) RecoverPipeline(context.Context) error {
	f.recoveries++
	if f.recoveries == 1 {
		return ErrRunBusy
	}
	return nil
}

func (f *supervisorFake) TryPipelineSession(context.Context) (bool, error) {
	f.sessions++
	f.cancel()
	return false, nil
}

func TestPipelineSupervisorRetriesRecoveryBeforeProcessing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f := &supervisorFake{cancel: cancel}
	reported := 0
	cfg := workerloop.Config{IdleDelay: time.Millisecond, ErrorMin: time.Millisecond, ErrorMax: time.Millisecond, OnError: func(err error) {
		if errors.Is(err, ErrRunBusy) {
			reported++
		}
	}}
	err := RunPipelineSupervisor(ctx, f, time.Hour, cfg)
	if !errors.Is(err, context.Canceled) || f.recoveries != 2 || f.sessions != 1 || reported != 1 {
		t.Fatalf("err=%v recovery=%d sessions=%d reported=%d", err, f.recoveries, f.sessions, reported)
	}
}

func TestPipelineSupervisorRejectsInvalidConfiguration(t *testing.T) {
	if err := RunPipelineSupervisor(context.Background(), nil, time.Second, workerloop.Config{}); !errors.Is(err, workerloop.ErrConfig) {
		t.Fatalf("invalid runtime: %v", err)
	}
}

type busySupervisorFake struct {
	recoveries, sessions int
	cancel               context.CancelFunc
}

func (f *busySupervisorFake) RecoverPipeline(context.Context) error {
	f.recoveries++
	return nil
}

func (f *busySupervisorFake) TryPipelineSession(context.Context) (bool, error) {
	f.sessions++
	if f.sessions == 3 {
		f.cancel()
	}
	return true, nil
}

func TestPipelineSupervisorRecoversDuringContinuousWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := &busySupervisorFake{cancel: cancel}
	cfg := workerloop.Config{IdleDelay: time.Second, ErrorMin: time.Second, ErrorMax: time.Second}
	err := RunPipelineSupervisor(ctx, f, time.Nanosecond, cfg)
	if !errors.Is(err, context.Canceled) || f.recoveries != 3 || f.sessions != 3 {
		t.Fatalf("err=%v recovery=%d sessions=%d", err, f.recoveries, f.sessions)
	}
}
