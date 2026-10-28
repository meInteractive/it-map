//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/parser"
)

var syntheticPaid = json.RawMessage(`{"provider":"apify","model":"CI_actor","capUsd":0.26}`)
var syntheticFree = json.RawMessage(`{"provider":"apify","model":"CI_actor","free":true,"capUsd":0.26}`)
var syntheticUsage = json.RawMessage(`{"usage":{"cost":0.01}}`)

func billingFixture(t *testing.T) *schedulerFixture {
	f := openSchedulerFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	f.ctx = ctx
	return f
}
func billingReset(f *schedulerFixture) {
	for _, table := range []string{"scheduler_attempts", "scheduler_jobs", "scheduler_runs"} {
		f.exec("DELETE FROM " + table)
	}
	f.exec(`UPDATE firecrawl_settings SET scheduler_json='{"enabled":true,"dayBudgetUsd":0.3,"monthBudgetUsd":0.3}' WHERE id=1`)
}
func billingStart(t *testing.T, f *schedulerFixture, key string) (parser.RunStart, *parser.JobLease, *parser.Attempt) {
	t.Helper()
	run := f.begin(key)
	schedulerSeedJob(f, "CI_money_job", nil)
	lease := schedulerRequireLease(t, f, run, "CI_money_job")
	a, e := lease.StartAttempt(f.ctx)
	if e != nil {
		t.Fatal("cannot start owned attempt", e)
	}
	return run, lease, a
}
func billingAmount(t *testing.T, f *schedulerFixture, id string) (float64, sql.NullFloat64, string) {
	t.Helper()
	var reserve float64
	var cost sql.NullFloat64
	var status string
	if f.db.QueryRowContext(f.ctx, "SELECT reserved_usd,cost_usd,status FROM scheduler_attempts WHERE id=?", id).Scan(&reserve, &cost, &status) != nil {
		t.Fatal("cannot read synthetic money ledger")
	}
	return reserve, cost, status
}
func TestMariaDBSchedulerBillingSourceDisposable(t *testing.T) {
	f := billingFixture(t)
	f.db.SetMaxOpenConns(1)
	f.exec("SET timestamp=1800000000")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := f.db.ExecContext(ctx, "SET timestamp=0"); e != nil {
			t.Error("cannot reset billing source clock")
		}
	})
	cmd := exec.Command("node", "scripts/check-scheduler-billing.mjs")
	cmd.Dir = "../.."
	data, e := cmd.Output()
	if e != nil {
		t.Fatal("source billing fixture failed", e)
	}
	count := 0
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		var v struct {
			Name, Throw, ResponseRaw string
			Now                      int64
			Day, Month               float64
			Request, Response        json.RawMessage
			Unknown                  bool
			Requests, Executions     int
			Seeds                    []struct {
				Period   string
				Offset   int64
				Reserved float64
				Cost     *float64
			}
			Attempt struct {
				Status  string
				Reserve float64  `json:"reserved_usd"`
				Cost    *float64 `json:"cost_usd"`
				Code    string   `json:"reason_code"`
				Message string
				Result  string `json:"result_json"`
			}
			Job struct {
				Status   string
				After    int64 `json:"run_after"`
				Attempts int64
			}
		}
		if json.Unmarshal(line, &v) != nil {
			t.Fatal("invalid billing source vector")
		}
		count++
		if !t.Run(v.Name, func(t *testing.T) {
			f := f.sub(t)
			billingReset(f)
			settings, _ := json.Marshal(map[string]any{"enabled": true, "dayBudgetUsd": v.Day, "monthBudgetUsd": v.Month})
			f.exec("UPDATE firecrawl_settings SET scheduler_json=? WHERE id=1", string(settings))
			period := parser.SchedulerPeriod(time.UnixMilli(v.Now))
			for i, s := range v.Seeds {
				start := period.Day
				if s.Period == "month" {
					start = period.Month
				}
				f.exec("INSERT INTO scheduler_attempts(id,job_id,run_id,type,started_at,status,reserved_usd,cost_usd) VALUES(?,'CI_other_job','CI_old_generation_run','diagnostic',?,'interrupted',?,?)", fmt.Sprintf("CI_seed_%d", i), start+s.Offset, s.Reserved, s.Cost)
			}
			run, _, a := billingStart(t, f, "CI source "+v.Name)
			executions := 0
			_, e := a.Run(f.ctx, v.Request, func(ctx context.Context) (json.RawMessage, error) {
				executions++
				reserve, cost, status := billingAmount(t, f, a.ID())
				upper, e := parser.UpperCostJSON(v.Request)
				if e != nil || math.Abs(reserve-upper) > 1e-12 || cost.Valid || status != "running" || a.Requests() != 1 {
					t.Error("mock entered before durable reserve")
				}
				if v.Throw != "" {
					return nil, errors.New("CI synthetic network failure")
				}
				if v.ResponseRaw != "" {
					return json.RawMessage(v.ResponseRaw), nil
				}
				return v.Response, nil
			})
			if v.Executions == 0 && !errors.Is(e, parser.ErrBudgetLimit) {
				t.Fatal("source budget denial not preserved", e)
			}
			if v.Executions > 0 && v.Throw == "" && e != nil {
				t.Fatal("known mock request failed", e)
			}
			if v.Throw != "" && e == nil {
				t.Fatal("mock failure swallowed")
			}
			if executions != v.Executions || a.Requests() != v.Requests || a.Unknown() != v.Unknown {
				t.Fatal("request/unknown state differs from source")
			}
			if e = a.Finish(f.ctx, parser.AttemptFinish{Job: parser.FinishOptions{Status: v.Attempt.Status, Code: v.Attempt.Code, Message: v.Attempt.Message, Delay: time.Duration(v.Job.After-v.Now) * time.Millisecond, Deferred: v.Job.Attempts == 0}, Result: json.RawMessage(v.Attempt.Result)}); e != nil {
				t.Fatal("atomic attempt/job finish failed", e)
			}
			reserve, cost, status := billingAmount(t, f, a.ID())
			if math.Abs(reserve-v.Attempt.Reserve) > 1e-12 || cost.Valid != (v.Attempt.Cost != nil) || cost.Valid && math.Abs(cost.Float64-*v.Attempt.Cost) > 1e-12 || status != v.Attempt.Status {
				t.Fatal("final ledger differs from unchanged source")
			}
			if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_money_job' AND status=? AND attempts=? AND run_after=? AND locked_by='' AND locked_until=0", v.Job.Status, v.Job.Attempts, v.Job.After) != 1 {
				t.Fatal("joint job receipt differs from source")
			}
			f.finish(run)
		}) {
			break
		}
	}
	if count != 38 {
		t.Fatal("missing required source billing cases", count)
	}
}

func TestMariaDBSchedulerBillingOwnershipDisposable(t *testing.T) {
	if os.Getenv("ITMAP_BILLING_CRASH_CHILD") == "1" {
		billingCrashChild(t)
		return
	}
	f := billingFixture(t)
	t.Run("one opaque attempt and known/unknown lifecycle", func(t *testing.T) {
		f := f.sub(t)
		billingReset(f)
		run, lease, a := billingStart(t, f, "CI attempt lifecycle")
		if next, e := lease.StartAttempt(f.ctx); !errors.Is(e, parser.ErrAttemptReplay) || next != nil {
			t.Fatal("attempt replay returned execution handle")
		}
		calls := 0
		for i := 0; i < 2; i++ {
			if _, e := a.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) { calls++; return syntheticUsage, nil }); e != nil {
				t.Fatal("known request failed", e)
			}
		}
		if calls != 2 || a.Requests() != 2 || a.Unknown() {
			t.Fatal("known billing metrics wrong")
		}
		if a.Finish(f.ctx, parser.AttemptFinish{Result: json.RawMessage(`{"mock":true}`)}) != nil {
			t.Fatal("known finish failed")
		}
		reserve, cost, status := billingAmount(t, f, a.ID())
		if math.Abs(reserve-.02) > 1e-12 || !cost.Valid || cost.Float64 != reserve || status != "completed" {
			t.Fatal("known amount lost")
		}
		if _, e := a.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) {
			t.Error("closed attempt called provider")
			return nil, nil
		}); !errors.Is(e, parser.ErrAttemptClosed) {
			t.Fatal("closed attempt reused")
		}
		f.finish(run)
	})
	t.Run("unknown reservation blocks this handle and replacement paid work", func(t *testing.T) {
		f := f.sub(t)
		billingReset(f)
		run, _, a := billingStart(t, f, "CI unresolved work")
		calls := 0
		work := func(context.Context) (json.RawMessage, error) { calls++; return json.RawMessage(`{"usage":{}}`), nil }
		if _, e := a.Run(f.ctx, syntheticPaid, work); e != nil || !a.Unknown() {
			t.Fatal("unknown usage did not retain money", e)
		}
		copy := *a
		if _, e := copy.Run(f.ctx, syntheticPaid, work); !errors.Is(e, parser.ErrMoneyUncertain) || calls != 1 {
			t.Fatal("copied handle repeated unknown request")
		}
		if a.Finish(f.ctx, parser.AttemptFinish{Job: parser.FinishOptions{Status: "pending", Deferred: true}}) != nil {
			t.Fatal("unknown attempt receipt failed")
		}
		f.finish(run)
		next := f.begin("CI unresolved replacement")
		lease := schedulerRequireLease(t, f, next, "CI_money_job")
		b, e := lease.StartAttempt(f.ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.Run(f.ctx, syntheticPaid, work); !errors.Is(e, parser.ErrMoneyUncertain) || calls != 1 {
			t.Fatal("replacement blindly replayed paid request", e)
		}
		if _, e = b.Run(f.ctx, syntheticFree, func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{}`), nil }); e != nil {
			t.Fatal("known free local progress denied", e)
		}
		if b.Finish(f.ctx, parser.AttemptFinish{}) != nil {
			t.Fatal("free receipt failed")
		}
		reserve, cost, _ := billingAmount(t, f, a.ID())
		if reserve != .26 || cost.Valid {
			t.Fatal("replacement released unknown prior spend")
		}
		f.finish(next)
	})
	t.Run("recovered adapter panic cannot declare reserve known", func(t *testing.T) {
		f := f.sub(t)
		billingReset(f)
		run, _, a := billingStart(t, f, "CI recovered adapter panic")
		recovered := false
		func() {
			defer func() { recovered = recover() != nil }()
			_, _ = a.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) { panic("CI synthetic adapter panic") })
		}()
		if !recovered || !a.Unknown() || a.Requests() != 1 {
			t.Fatal("recovered panic lost unresolved outcome")
		}
		if _, e := a.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) {
			t.Error("panic triggered paid replay")
			return nil, nil
		}); !errors.Is(e, parser.ErrMoneyUncertain) {
			t.Fatal("panic outcome replay allowed")
		}
		if a.Finish(f.ctx, parser.AttemptFinish{Job: parser.FinishOptions{Status: "pending", Deferred: true}}) != nil {
			t.Fatal("panic receipt failed")
		}
		reserve, cost, _ := billingAmount(t, f, a.ID())
		if reserve != .26 || cost.Valid {
			t.Fatal("panic recorded a known cost")
		}
		f.finish(run)
	})
	t.Run("generation rotation cannot reset global spend", func(t *testing.T) {
		f := f.sub(t)
		billingReset(f)
		run, _, a := billingStart(t, f, "CI before generation rotation")
		if _, e := a.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) { return json.RawMessage(`{"usage":{}}`), nil }); e != nil {
			t.Fatal(e)
		}
		defer f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", f.repo.Generation)
		nextGeneration := "22222222-2222-4333-8444-555555555555"
		f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", nextGeneration)
		f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE id=?", run.Run.ID)
		fresh := *f
		fresh.repo = parser.RunRepository{DB: f.db, Generation: nextGeneration}
		next := fresh.begin("CI after generation rotation")
		schedulerSeedJob(&fresh, "CI_new_generation_job", nil)
		lease := schedulerRequireLease(t, &fresh, next, "CI_new_generation_job")
		b, e := lease.StartAttempt(f.ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) {
			t.Error("restore reset spending ceiling")
			return nil, nil
		}); !errors.Is(e, parser.ErrBudgetLimit) {
			t.Fatal("old generation money ignored", e)
		}
		reserve, cost, status := billingAmount(t, f, a.ID())
		if reserve != .26 || cost.Valid || status != "interrupted" {
			t.Fatal("generation rotation lost conservative history")
		}
		if b.Finish(f.ctx, parser.AttemptFinish{}) != nil {
			t.Fatal("new generation receipt failed")
		}
		fresh.finish(next)
	})
	t.Run("work deadline stays within owned run lease", func(t *testing.T) {
		f := f.sub(t)
		billingReset(f)
		run, _, a := billingStart(t, f, "CI bounded work")
		f.exec("UPDATE scheduler_runs SET lease_until=CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)+1000 WHERE id=?", run.Run.ID)
		_, e := a.Run(f.ctx, syntheticPaid, func(ctx context.Context) (json.RawMessage, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > time.Second {
				t.Error("work outlives owned lease")
			}
			<-ctx.Done()
			return nil, ctx.Err()
		})
		if !errors.Is(e, context.DeadlineExceeded) || !a.Unknown() {
			t.Fatal("expiry did not cancel mock work", e)
		}
		reserve, cost, _ := billingAmount(t, f, a.ID())
		if reserve != .26 || cost.Valid {
			t.Fatal("timeout freed reserve")
		}
	})
	t.Run("late SQL faults and joint receipt rollback", func(t *testing.T) {
		f := f.sub(t)
		for i, prefix := range []string{"INSERT INTO scheduler_attempts", "UPDATE scheduler_attempts SET reserved_usd=reserved_usd+", "UPDATE scheduler_attempts SET reserved_usd=GREATEST", "UPDATE scheduler_jobs SET status=?"} {
			billingReset(f)
			run := f.begin(fmt.Sprintf("CI money fault %d", i))
			schedulerSeedJob(f, "CI_money_job", nil)
			repo, fired := schedulerFaultRepository(t, f.repo.Generation, prefix)
			id := "CI_money_job"
			lease, e := repo.ClaimJob(f.ctx, run.Run.ID, parser.ClaimOptions{JobID: &id})
			if e != nil || lease == nil {
				t.Fatal(e)
			}
			a, e := lease.StartAttempt(f.ctx)
			if i == 0 {
				if !errors.Is(e, parser.ErrRunStorage) || !fired.Load() || f.count("SELECT COUNT(*) FROM scheduler_attempts") != 0 {
					t.Fatal("late attempt INSERT not rolled back")
				}
				f.finish(run)
				continue
			}
			if e != nil {
				t.Fatal(e)
			}
			calls := 0
			_, e = a.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) { calls++; return syntheticUsage, nil })
			if i == 1 {
				if !errors.Is(e, parser.ErrRunStorage) || calls != 0 || !fired.Load() {
					t.Fatal("failed reserve executed provider")
				}
				reserve, _, _ := billingAmount(t, f, a.ID())
				if reserve != 0 {
					t.Fatal("failed reserve escaped rollback")
				}
			}
			if i == 2 {
				if !errors.Is(e, parser.ErrRunStorage) || calls != 1 || !fired.Load() || !a.Unknown() {
					t.Fatal("settlement failure not conservative")
				}
				reserve, cost, _ := billingAmount(t, f, a.ID())
				if reserve != .26 || cost.Valid {
					t.Fatal("failed settlement lost reserve")
				}
			}
			if i == 3 {
				if e != nil {
					t.Fatal(e)
				}
				if e = a.Finish(f.ctx, parser.AttemptFinish{}); !errors.Is(e, parser.ErrRunStorage) || !fired.Load() {
					t.Fatal("joint finish fault not exercised")
				}
				_, cost, status := billingAmount(t, f, a.ID())
				if status != "running" || cost.Valid || lease.AssertActive(f.ctx) != nil || f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE finished_at IS NOT NULL") != 0 {
					t.Fatal("attempt receipt committed without job")
				}
				if a.Finish(f.ctx, parser.AttemptFinish{}) != nil {
					t.Fatal("known rollback retry failed")
				}
			}
			f.finish(run)
		}
	})
	t.Run("actual concurrent reservation serializes global ceiling", func(t *testing.T) {
		f := f.sub(t)
		billingReset(f)
		run := f.begin("CI concurrent money")
		_, dbA := schedulerDB(t)
		_, dbB := schedulerDB(t)
		dbA.SetMaxOpenConns(1)
		dbB.SetMaxOpenConns(1)
		dbs := []*sql.DB{dbA, dbB}
		var ids [2]int64
		attempts := make([]*parser.Attempt, 2)
		for i, db := range dbs {
			id := fmt.Sprintf("CI_paid_%d", i)
			schedulerSeedJob(f, id, nil)
			repo := parser.RunRepository{DB: db, Generation: f.repo.Generation}
			lease, e := repo.ClaimJob(f.ctx, run.Run.ID, parser.ClaimOptions{JobID: &id})
			if e != nil || lease == nil {
				t.Fatal(e)
			}
			attempts[i], e = lease.StartAttempt(f.ctx)
			if e != nil || db.QueryRow("SELECT CONNECTION_ID()").Scan(&ids[i]) != nil {
				t.Fatal("cannot prepare actual money contender")
			}
		}
		if ids[0] == ids[1] {
			t.Fatal("different SQL connections required")
		}
		held, e := f.db.BeginTx(f.ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		defer held.Rollback()
		var gen string
		if held.QueryRowContext(f.ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&gen) != nil {
			t.Fatal("cannot hold budget mutex")
		}
		ready := make(chan int, 2)
		release := make(chan struct{})
		var once sync.Once
		unlock := func() { once.Do(func() { close(release) }) }
		defer unlock()
		type result struct {
			i int
			e error
		}
		results := make(chan result, 2)
		for i, a := range attempts {
			go func(i int, a *parser.Attempt) {
				_, e := a.Run(f.ctx, syntheticPaid, func(ctx context.Context) (json.RawMessage, error) {
					ready <- i
					select {
					case <-release:
						return syntheticUsage, nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				})
				results <- result{i, e}
			}(i, a)
		}
		observed := false
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if f.count("SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID IN (?,?) AND INFO LIKE 'SELECT generation FROM app_runtime_generation%FOR UPDATE'", ids[0], ids[1]) == 2 {
				observed = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !observed {
			t.Fatal("both reservations must actually block")
		}
		if held.Commit() != nil {
			t.Fatal("cannot release contenders")
		}
		var winner int
		select {
		case winner = <-ready:
		case <-time.After(5 * time.Second):
			t.Fatal("no provider mock entered after reserve")
		}
		var loser result
		select {
		case loser = <-results:
		case <-time.After(5 * time.Second):
			t.Fatal("budget loser did not return")
		}
		if !errors.Is(loser.e, parser.ErrBudgetLimit) || loser.i == winner {
			t.Fatal("concurrent request bypassed ceiling", loser.e)
		}
		if f.count("SELECT COUNT(*) FROM scheduler_attempts WHERE reserved_usd=0.26 AND cost_usd IS NULL") != 1 || attempts[winner].Requests() != 1 || attempts[loser.i].Requests() != 0 {
			t.Fatal("non-atomic budget reservation")
		}
		unlock()
		if done := <-results; done.e != nil || done.i != winner {
			t.Fatal("winner did not settle")
		}
		if _, e = attempts[loser.i].Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) { return syntheticUsage, nil }); e != nil {
			t.Fatal("released actual cost did not permit next request", e)
		}
		for _, a := range attempts {
			if a.Finish(f.ctx, parser.AttemptFinish{}) != nil {
				t.Fatal("concurrent receipt failed")
			}
		}
		f.finish(run)
	})
	t.Run("expired owner cannot settle or release reserve", func(t *testing.T) {
		f := f.sub(t)
		billingReset(f)
		run, _, a := billingStart(t, f, "CI stale settlement")
		_, e := a.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) {
			f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE id=?", run.Run.ID)
			return syntheticUsage, nil
		})
		if e == nil || !a.Unknown() || a.Finish(f.ctx, parser.AttemptFinish{}) == nil {
			t.Fatal("expired worker settled receipt")
		}
		reserve, cost, _ := billingAmount(t, f, a.ID())
		if reserve != .26 || cost.Valid {
			t.Fatal("expiry released reserved spend")
		}
	})
	t.Run("unknown actual COMMIT never grants replay handle or provider work", func(t *testing.T) {
		f := f.sub(t)
		for i, prefix := range []string{"INSERT INTO scheduler_attempts", "UPDATE scheduler_attempts SET reserved_usd=reserved_usd+"} {
			billingReset(f)
			run := f.begin(fmt.Sprintf("CI unknown money commit %d", i))
			schedulerSeedJob(f, "CI_money_job", nil)
			repo, fired := billingCommitRepository(t, f.repo.Generation, prefix)
			id := "CI_money_job"
			lease, e := repo.ClaimJob(f.ctx, run.Run.ID, parser.ClaimOptions{JobID: &id})
			if e != nil || lease == nil {
				t.Fatal(e)
			}
			a, e := lease.StartAttempt(f.ctx)
			if i == 0 {
				if !errors.Is(e, parser.ErrRunStorage) || a != nil || !fired.Load() || f.count("SELECT COUNT(*) FROM scheduler_attempts") != 1 {
					t.Fatal("unknown INSERT commit not reproduced")
				}
				if _, e = lease.StartAttempt(f.ctx); !errors.Is(e, parser.ErrAttemptReplay) {
					t.Fatal("unknown attempt commit replayed")
				}
			}
			if i == 1 {
				if e != nil {
					t.Fatal(e)
				}
				calls := 0
				work := func(context.Context) (json.RawMessage, error) { calls++; return syntheticUsage, nil }
				if _, e = a.Run(f.ctx, syntheticPaid, work); !errors.Is(e, parser.ErrRunStorage) || calls != 0 || !fired.Load() || !a.Unknown() {
					t.Fatal("unknown reserve commit executed work")
				}
				reserve, cost, _ := billingAmount(t, f, a.ID())
				if reserve != .26 || cost.Valid {
					t.Fatal("actual unknown commit did not retain reserve")
				}
				if _, e = a.Run(f.ctx, syntheticPaid, work); !errors.Is(e, parser.ErrMoneyUncertain) || calls != 0 {
					t.Fatal("unknown reserve was blindly retried")
				}
			}
			f.finish(run)
		}
	})
	t.Run("SIGKILL keeps money and recovery denies paid replay", func(t *testing.T) {
		f := f.sub(t)
		billingReset(f)
		cmd := exec.Command(os.Args[0], "-test.run=^TestMariaDBSchedulerBillingOwnershipDisposable$", "-test.count=1")
		cmd.Env = append(os.Environ(), "ITMAP_BILLING_CRASH_CHILD=1")
		pipe, e := cmd.StdoutPipe()
		if e != nil || cmd.Start() != nil {
			t.Fatal("cannot start billing crash child")
		}
		defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
		ready := make(chan bool, 1)
		go func() {
			s := bufio.NewScanner(pipe)
			for s.Scan() {
				if s.Text() == "CI_BILLING_WORK_READY" {
					ready <- true
					return
				}
			}
			ready <- false
		}()
		select {
		case ok := <-ready:
			if !ok {
				t.Fatal("billing child not ready")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("billing crash timeout")
		}
		// A provider mock may block, but no generation lock/TX may span that I/O.
		tx, e := f.db.BeginTx(f.ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		var gen string
		if tx.QueryRowContext(f.ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&gen) != nil {
			t.Fatal("external work held budget transaction")
		}
		_ = tx.Rollback()
		if cmd.Process.Kill() != nil || cmd.Wait() == nil {
			t.Fatal("billing child not killed")
		}
		var oldID string
		if f.db.QueryRowContext(f.ctx, "SELECT id FROM scheduler_attempts WHERE job_id='CI_money_job'").Scan(&oldID) != nil {
			t.Fatal("committed reservation lost")
		}
		reserve, cost, status := billingAmount(t, f, oldID)
		if reserve != .26 || cost.Valid || status != "running" {
			t.Fatal("SIGKILL freed reserve")
		}
		f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE status='running'")
		f.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE id='CI_money_job'")
		_, db := schedulerDB(t)
		fresh := *f
		fresh.repo = parser.RunRepository{DB: db, Generation: f.repo.Generation}
		run := fresh.begin("CI after billing crash")
		lease := schedulerRequireLease(t, &fresh, run, "CI_money_job")
		a, e := lease.StartAttempt(f.ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = a.Run(f.ctx, syntheticPaid, func(context.Context) (json.RawMessage, error) {
			t.Error("crash recovery blindly posted")
			return nil, nil
		}); !errors.Is(e, parser.ErrMoneyUncertain) {
			t.Fatal("crash recovery did not block paid replay", e)
		}
		reserve, cost, status = billingAmount(t, f, oldID)
		if reserve != .26 || cost.Valid || status != "interrupted" {
			t.Fatal("recovery lost historical conservative receipt")
		}
		if a.Finish(f.ctx, parser.AttemptFinish{Job: parser.FinishOptions{Status: "pending", Deferred: true}}) != nil {
			t.Fatal("blocked recovery receipt failed")
		}
		fresh.finish(run)
	})
}
func billingCrashChild(t *testing.T) {
	ctx, db := schedulerDB(t)
	f := &schedulerFixture{t: t, ctx: ctx, db: db, repo: parser.RunRepository{DB: db, Generation: os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION")}}
	_, _, a := billingStart(t, f, "CI billing crash child")
	if _, e := a.Run(ctx, syntheticPaid, func(ctx context.Context) (json.RawMessage, error) {
		fmt.Println("CI_BILLING_WORK_READY")
		<-ctx.Done()
		return nil, ctx.Err()
	}); e != nil {
		t.Fatal(e)
	}
}

type billingCommitFault struct {
	prefix       string
	armed, fired atomic.Bool
}
type billingCommitConnector struct {
	base  driver.Connector
	fault *billingCommitFault
}

func (c billingCommitConnector) Driver() driver.Driver { return c.base.Driver() }
func (c billingCommitConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, e := c.base.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return billingCommitConn{conn, c.fault}, nil
}

type billingCommitConn struct {
	driver.Conn
	fault *billingCommitFault
}

func (c billingCommitConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, e := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if e != nil {
		return nil, e
	}
	return billingCommitTx{tx, c.fault}, nil
}
func (c billingCommitConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}
func (c billingCommitConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	result, e := c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
	if e == nil && strings.HasPrefix(q, c.fault.prefix) {
		c.fault.armed.Store(true)
	}
	return result, e
}
func (c billingCommitConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	s, e := c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, q)
	if e != nil {
		return nil, e
	}
	return billingCommitStmt{s, q, c.fault}, nil
}

type billingCommitStmt struct {
	driver.Stmt
	q     string
	fault *billingCommitFault
}

func (s billingCommitStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	r, e := s.Stmt.(driver.StmtExecContext).ExecContext(ctx, args)
	if e == nil && strings.HasPrefix(s.q, s.fault.prefix) {
		s.fault.armed.Store(true)
	}
	return r, e
}
func (s billingCommitStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.Stmt.(driver.StmtQueryContext).QueryContext(ctx, args)
}

type billingCommitTx struct {
	driver.Tx
	fault *billingCommitFault
}

func (tx billingCommitTx) Commit() error {
	e := tx.Tx.Commit()
	if e == nil && tx.fault.armed.Load() && tx.fault.fired.CompareAndSwap(false, true) {
		return errors.New("CI synthetic error after actual COMMIT")
	}
	return e
}
func billingCommitRepository(t *testing.T, generation, prefix string) (parser.RunRepository, *atomic.Bool) {
	t.Helper()
	_, guarded := schedulerDB(t)
	_ = guarded
	cfg, e := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
	if e != nil {
		t.Fatal("cannot parse guarded billing DSN")
	}
	base, e := mysql.NewConnector(cfg)
	if e != nil {
		t.Fatal("cannot configure guarded billing connector")
	}
	fault := &billingCommitFault{prefix: prefix}
	db := sql.OpenDB(billingCommitConnector{base, fault})
	t.Cleanup(func() { db.Close() })
	return parser.RunRepository{DB: db, Generation: generation}, &fault.fired
}
