//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"itmap/internal/parser"
)

// Simulate a real process loss inside a paid AI stage, after reservation and
// entry to the provider adapter, without giving the child a chance to cleanup.
func TestMariaDBAIStageSIGKILLRecoveryDisposable(t *testing.T) {
	const marker = "CI_AI_STAGE_POST_ACCEPTED"
	if os.Getenv("ITMAP_AI_STAGE_CRASH_CHILD") == "1" {
		ctx, db := schedulerDB(t)
		repo := parser.RunRepository{DB: db, Generation: os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION")}
		run, err := repo.Begin(ctx, parser.RunOptions{RequestID: "CI_AI_STAGE_CRASH_CHILD"})
		if err != nil || !run.Acquired {
			t.Fatal("child run not acquired", err)
		}
		id := "CI_AI_CRASH_JOB"
		lease, err := repo.ClaimJob(ctx, run.Run.ID, parser.ClaimOptions{JobID: &id})
		if err != nil || lease == nil {
			t.Fatal("child AI job not claimed", err)
		}
		runner := &aiStageFixtureRunner{after: func() {
			fmt.Println(marker)
			// Parent SIGKILL interrupts this synthetic provider operation.
			<-ctx.Done()
		}}
		_, err = (&parser.AIStageExecutor{Store: &aiStageFixtureStore{}, Generator: runner}).Execute(ctx, lease)
		t.Fatal("child survived provider work before SIGKILL", err)
		return
	}

	f := billingFixture(t)
	billingReset(f)
	schedulerSeedJob(f, "CI_AI_CRASH_JOB", nil)
	f.exec("UPDATE scheduler_jobs SET type='extract_vacancy' WHERE id='CI_AI_CRASH_JOB'")
	cmd := exec.Command(os.Args[0], "-test.run=^TestMariaDBAIStageSIGKILLRecoveryDisposable$", "-test.count=1")
	cmd.Env = append(os.Environ(), "ITMAP_AI_STAGE_CRASH_CHILD=1")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal("cannot capture synthetic AI child", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal("cannot start synthetic AI process", err)
	}
	reaped := false
	defer func() {
		if !reaped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(pipe)
		for scanner.Scan() {
			if scanner.Text() == marker {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("child never reached synthetic paid AI provider")
		}
	case <-time.After(12 * time.Second):
		t.Fatal("child did not reach synthetic AI POST")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal("SIGKILL failed", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("child exited without SIGKILL")
	}
	reaped = true
	var attemptID string
	var reserve float64
	if err := f.db.QueryRowContext(f.ctx, "SELECT id,reserved_usd FROM scheduler_attempts WHERE job_id='CI_AI_CRASH_JOB' AND status='running' AND cost_usd IS NULL AND COALESCE(provider,'')<>''").Scan(&attemptID, &reserve); err != nil || reserve <= 0 {
		t.Fatal("crash lost pre-POST monetary reservation", err, reserve)
	}
	if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_AI_CRASH_JOB' AND status='running'") != 1 ||
		f.count("SELECT COUNT(*) FROM scheduler_runs WHERE status='running'") != 1 {
		t.Fatal("SIGKILL unexpectedly finished job or run")
	}

	// These lease writes are confined to disposable CI; production is untouched.
	f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE status='running'")
	f.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE id='CI_AI_CRASH_JOB'")
	if err := f.repo.RecoverExpired(f.ctx); err != nil {
		t.Fatal("failed to recover expired AI owner", err)
	}
	if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE id=? AND status='interrupted' AND reserved_usd=? AND cost_usd IS NULL", attemptID, reserve) != 1 ||
		f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_AI_CRASH_JOB' AND status='pending' AND locked_by=''") != 1 {
		t.Fatal("recovery did not retain unknown AI payment")
	}
	next := f.begin("CI_AI_STAGE_AFTER_SIGKILL")
	lease := schedulerRequireLease(t, f, next, "CI_AI_CRASH_JOB")
	attempt, err := lease.StartAttempt(f.ctx)
	if err != nil {
		t.Fatal("recovery cannot create new owned attempt", err)
	}
	replays := 0
	_, err = attempt.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) {
		replays++
		return syntheticUsage, nil
	})
	if !errors.Is(err, parser.ErrMoneyUncertain) || replays != 0 || attempt.Requests() != 0 {
		t.Fatal("SIGKILL recovery allowed another paid POST", err, replays)
	}
	if err := attempt.Finish(f.ctx, parser.AttemptFinish{Job: parser.FinishOptions{Status: "failed", Code: "unresolved_ai_crash"}}); err != nil {
		t.Fatal("cannot close blocked replacement attempt", err)
	}
	f.finish(next)
}

// An unresolved AI provider operation must fence paid work in a new generation,
// including a different job ID. Generation rotation cannot reset the ledger.
func TestMariaDBAIGenerationRotationBlocksPaidReplayDisposable(t *testing.T) {
	f := billingFixture(t)
	billingReset(f)
	old := f.begin("CI_AI_GEN_OLD")
	schedulerSeedJob(f, "CI_AI_GEN_OLD_JOB", nil)
	f.exec("UPDATE scheduler_jobs SET type='extract_vacancy' WHERE id='CI_AI_GEN_OLD_JOB'")
	lease := schedulerRequireLease(t, f, old, "CI_AI_GEN_OLD_JOB")
	attempt, err := lease.StartAttempt(f.ctx)
	if err != nil {
		t.Fatal("cannot create first generation attempt", err)
	}
	sends := 0
	_, err = attempt.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) {
		sends++
		return nil, errors.New("CI_NO_PROVIDER_ACK")
	})
	if err == nil || sends != 1 || !attempt.Unknown() || attempt.Requests() != 1 {
		t.Fatal("first generation did not produce synthetic unresolved send", err)
	}
	reserve, cost, status := billingAmount(t, f, attempt.ID())
	if reserve <= 0 || cost.Valid || status != "running" {
		t.Fatal("unresolved generation failed to retain reserve")
	}
	f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE id=?", old.Run.ID)
	f.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE id='CI_AI_GEN_OLD_JOB'")
	if err := f.repo.RecoverExpired(f.ctx); err != nil {
		t.Fatal("old generation recovery failed", err)
	}
	nextGeneration := "22222222-2222-4333-8444-555555555555"
	if f.repo.Generation == nextGeneration {
		nextGeneration = "22222222-2222-4333-8444-555555555556"
	}
	// Restore the global singleton even on assertion failure. CI-only DB.
	defer f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", f.repo.Generation)
	f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", nextGeneration)
	newRepo := parser.RunRepository{DB: f.db, Generation: nextGeneration}
	run, err := newRepo.Begin(f.ctx, parser.RunOptions{RequestID: "CI_AI_GEN_NEW"})
	if err != nil || !run.Acquired {
		t.Fatal("rotated generation failed to acquire run", err)
	}
	id := "CI_AI_GEN_NEW_JOB"
	f.exec("INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,priority,run_after,created_at,updated_at) VALUES(?,?,?,'extract_vacancy',0,0,0,0)", id, nextGeneration, id)
	freshLease, err := newRepo.ClaimJob(f.ctx, run.Run.ID, parser.ClaimOptions{JobID: &id})
	if err != nil || freshLease == nil {
		t.Fatal("cannot claim new generation job", err)
	}
	freshAttempt, err := freshLease.StartAttempt(f.ctx)
	if err != nil {
		t.Fatal("new generation attempt creation failed", err)
	}
	_, err = freshAttempt.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) {
		sends++
		return syntheticUsage, nil
	})
	if !errors.Is(err, parser.ErrHistoricalAIUnresolved) || sends != 1 || freshAttempt.Requests() != 0 || freshAttempt.Unknown() {
		t.Fatal("new generation crossed historical paid fence", err, sends)
	}
	afterReserve, afterCost, afterStatus := billingAmount(t, f, attempt.ID())
	if afterReserve != reserve || afterCost.Valid || afterStatus != "interrupted" {
		t.Fatal("generation rotation changed old unknown money evidence")
	}
	if err := freshAttempt.Finish(f.ctx, parser.AttemptFinish{Job: parser.FinishOptions{Status: "failed", Code: "historical_ai_unknown"}}); err != nil {
		t.Fatal("new generation blocked attempt finish failed", err)
	}
	if err := newRepo.Finish(f.ctx, run.Run.ID, "completed"); err != nil {
		t.Fatal("new generation run release failed", err)
	}
}
