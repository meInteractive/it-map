//go:build mariadb && schedulerwrites

package storage

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"itmap/internal/parser"
)

func schedulerClock(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var now int64
	if db.QueryRow("SELECT CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)").Scan(&now) != nil {
		t.Fatal("synthetic scheduler clock failed")
	}
	return now
}
func schedulerSeedJob(f *schedulerFixture, id string, candidate any) {
	f.exec("INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,candidate_id,priority,run_after,created_at,updated_at) VALUES(?,?,?,'diagnostic',?,0,0,0,0)", id, f.repo.Generation, id, candidate)
}
func schedulerRequireLease(t *testing.T, f *schedulerFixture, run parser.RunStart, id string) *parser.JobLease {
	t.Helper()
	lease, e := f.repo.ClaimJob(f.ctx, run.Run.ID, parser.ClaimOptions{JobID: &id})
	if e != nil || lease == nil {
		t.Fatal("synthetic job not claimed", e)
	}
	return lease
}
func schedulerRows(t *testing.T, f *schedulerFixture, selected string) []map[string]any {
	t.Helper()
	rows, e := f.db.QueryContext(f.ctx, "SELECT id,generation,dedupe_key,type,company_id,agency_id,candidate_id,source_id,payload,status,priority,run_after,attempts,max_attempts,locked_by,locked_until,last_error,reason_code,created_at,updated_at,completed_at FROM scheduler_jobs ORDER BY id")
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	names, e := rows.Columns()
	if e != nil {
		t.Fatal(e)
	}
	out := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(names))
		ptr := make([]any, len(names))
		for i := range values {
			ptr[i] = &values[i]
		}
		if rows.Scan(ptr...) != nil {
			t.Fatal("cannot read synthetic jobs")
		}
		r := map[string]any{}
		for i, k := range names {
			if b, ok := values[i].([]byte); ok {
				r[k] = string(b)
			} else {
				r[k] = values[i]
			}
		}
		if r["id"] == selected && r["locked_by"] != "" {
			r["locked_by"] = "<claimed>"
		}
		out = append(out, r)
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	return out
}
func TestMariaDBSchedulerJobSourceDisposable(t *testing.T) {
	f := openSchedulerFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	f.ctx = ctx
	// Source vectors include exact due/deferred/hour boundaries. Pin only this
	// guarded fixture's SQL session clock, so seeding latency cannot turn a +1ms
	// future job into due work. The ownership/crash parent uses real clocks and
	// separate actual connections. Production has no injected clock or SET SQL.
	f.db.SetMaxOpenConns(1)
	f.exec("SET timestamp=1800000000")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, e := f.db.ExecContext(ctx, "SET timestamp=0"); e != nil {
			t.Error("cannot restore synthetic source session clock")
		}
	})
	if schedulerClock(t, f.db) != 1800000000000 {
		t.Fatal("source clock was not pinned exactly")
	}
	for _, table := range []string{"apify_runs", "vacancy_discovery_runs"} {
		if f.count("SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatal("source fixture requires empty owned auxiliary tables")
		}
	}
	t.Cleanup(func() {
		for _, table := range []string{"apify_runs", "vacancy_discovery_runs"} {
			f.exec("DELETE FROM " + table)
		}
	})
	cmd := exec.Command("node", "scripts/check-scheduler-claim.mjs")
	cmd.Dir = "../.."
	data, e := cmd.Output()
	if e != nil {
		t.Fatal("unchanged source fixture failed", e)
	}
	var references []map[string]any
	var actual [][]map[string]any
	rejected := 0
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		var v map[string]any
		if json.Unmarshal(line, &v) != nil {
			t.Fatal("source fixture JSON invalid")
		}
		ok := t.Run(v["name"].(string), func(t *testing.T) {
			f := f.sub(t)
			for _, table := range []string{"scheduler_jobs", "scheduler_runs", "apify_runs", "vacancy_discovery_runs"} {
				f.exec("DELETE FROM " + table)
			}
			base := schedulerClock(t, f.db)
			offset := float64(base) - v["now"].(float64)
			v["now"] = base
			oldGeneration := v["generation"].(string)
			v["generation"] = f.repo.Generation
			settings, _ := json.Marshal(v["settings"])
			if raw, ok := v["settingsRaw"].(string); ok {
				settings = []byte(raw)
			}
			f.exec("UPDATE firecrawl_settings SET scheduler_json=? WHERE id=1", string(settings))
			// Seed the source's existing run; this gate tests job operations, not Begin.
			f.exec("INSERT INTO scheduler_runs(id,generation,`trigger`,started_at,status,lease_until) VALUES('CI_run',?,'manual',?,'running',?)", f.repo.Generation, base-1, base+3600000)
			if v["mariadbReject"] == "lone_UTF16" {
				raw := v["jobs"].([]any)[0].(map[string]any)["PayloadRaw"].(string)
				var valid int
				if f.db.QueryRowContext(f.ctx, "SELECT JSON_VALID(?)", raw).Scan(&valid) != nil || valid != 0 {
					t.Fatal("target Unicode representability boundary changed")
				}
				_, e := f.db.ExecContext(f.ctx, "INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,payload,priority,run_after,created_at,updated_at) VALUES('CI_unrepresentable',?,'CI_unrepresentable','diagnostic',?,0,0,0,0)", f.repo.Generation, raw)
				if e == nil || f.count("SELECT COUNT(*) FROM scheduler_jobs") != 0 {
					t.Fatal("unrepresentable source JSON escaped native CHECK")
				}
				rejected++
				return
			}
			for _, item := range v["jobs"].([]any) {
				j := item.(map[string]any)
				if j["Generation"] == oldGeneration {
					j["Generation"] = f.repo.Generation
				}
				for _, key := range []string{"RunAfter", "CreatedAt", "UpdatedAt"} {
					j[key] = j[key].(float64) + offset
				}
				payload, _ := json.Marshal(j["Payload"])
				if raw, ok := j["PayloadRaw"].(string); ok {
					payload = []byte(raw)
				}
				owner := ""
				if j["Status"] == "running" {
					owner = "CI_previous"
				}
				f.exec("INSERT INTO scheduler_jobs(id,generation,dedupe_key,type,company_id,agency_id,candidate_id,source_id,payload,status,priority,run_after,attempts,max_attempts,locked_by,locked_until,last_error,reason_code,created_at,updated_at,completed_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", j["ID"], j["Generation"], j["DedupeKey"], j["Type"], j["CompanyID"], j["AgencyID"], j["CandidateID"], j["SourceID"], string(payload), j["Status"], j["Priority"], j["RunAfter"], j["Attempts"], j["MaxAttempts"], owner, j["LockedUntil"], j["LastError"], j["ReasonCode"], j["CreatedAt"], j["UpdatedAt"], j["CompletedAt"])
			}
			if operations, ok := v["operations"].([]any); ok {
				for i, key := range operations {
					f.exec("INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,state,created_at,updated_at) VALUES(?,?,?,'free','CI_account','CI_hash','2026-01-01','2027-01-01',0.01,'failed',0,0)", fmt.Sprintf("CI_op_%d", i), f.repo.Generation, key)
				}
			}
			if pages, ok := v["pages"].(map[string]any); ok {
				for id, page := range pages {
					raw, _ := json.Marshal(map[string]any{"paused": map[string]any{"apify": map[string]any{"page": page}}})
					f.exec("INSERT INTO vacancy_discovery_runs(id,generation,user_name,source_id,source_url,source_revision,settings_revision,result_json,created_at,expires_at) VALUES(?,?,'CI_user','hh','https://example.invalid',0,0,?,0,?)", id, f.repo.Generation, string(raw), base+3600000)
				}
			}
			var opts struct {
				Types                         []string
				Diagnostic, DeferredApifyOnly bool
				AllowApify, AllowApifyStarts  *bool
				JobID                         *string
			}
			raw, _ := json.Marshal(v["options"])
			if json.Unmarshal(raw, &opts) != nil {
				t.Fatal("invalid options")
			}
			lease, e := f.repo.ClaimJob(f.ctx, "CI_run", parser.ClaimOptions{Types: opts.Types, Diagnostic: opts.Diagnostic, DeferredApifyOnly: opts.DeferredApifyOnly, AllowApify: opts.AllowApify, AllowApifyStarts: opts.AllowApifyStarts, JobID: opts.JobID})
			if v["error"] == "input" {
				if !errors.Is(e, parser.ErrJobInput) {
					t.Fatal("unknown type accepted", e)
				}
			} else if e != nil {
				t.Fatal("job claim failed", e)
			}
			selected := ""
			if lease != nil {
				selected = lease.Job().ID
				v["now"] = lease.ClaimedAt()
			}
			if selected != v["selected"] {
				t.Fatalf("MariaDB chose %q, source %q", selected, v["selected"])
			}
			if finish, ok := v["finish"].(map[string]any); ok && lease != nil {
				var options struct {
					Status, Code, Message string
					DelayMs               int64
					Payload               json.RawMessage
					Deferred              bool
				}
				raw, _ := json.Marshal(finish)
				if json.Unmarshal(raw, &options) != nil {
					t.Fatal("invalid finish options")
				}
				if e = lease.Finish(f.ctx, parser.FinishOptions{Status: options.Status, Code: options.Code, Message: options.Message, Delay: time.Duration(options.DelayMs) * time.Millisecond, Payload: options.Payload, Deferred: options.Deferred}); e != nil {
					t.Fatal("job finish failed", e)
				}
				var updated int64
				if f.db.QueryRowContext(f.ctx, "SELECT updated_at FROM scheduler_jobs WHERE id=?", selected).Scan(&updated) != nil {
					t.Fatal("finish time unavailable")
				}
				v["finishNow"] = updated
			}
			references = append(references, v)
			actual = append(actual, schedulerRows(t, f, selected))
		})
		if !ok {
			break
		}
	}
	if t.Failed() {
		return
	}
	if len(references) != 142 || rejected != 1 {
		t.Fatal("missing SQL source vectors", len(references))
	}
	input, _ := json.Marshal(references)
	oracle := exec.Command("node", "scripts/check-scheduler-claim.mjs", "--reference")
	oracle.Dir = "../.."
	oracle.Stdin = bytes.NewReader(input)
	data, e = oracle.Output()
	if e != nil {
		t.Fatal("source SQL comparison failed", e)
	}
	var expected []struct {
		Name string
		Rows []map[string]any
	}
	if json.Unmarshal(data, &expected) != nil {
		t.Fatal("invalid source result")
	}
	for i, v := range expected {
		raw, _ := json.Marshal(actual[i])
		var normalized []map[string]any
		_ = json.Unmarshal(raw, &normalized)
		if !reflect.DeepEqual(normalized, v.Rows) {
			t.Errorf("complete scheduler row state differs from unchanged source: %s", v.Name)
		}
	}
}

func TestMariaDBSchedulerJobOwnershipDisposable(t *testing.T) {
	if os.Getenv("ITMAP_JOB_CRASH_CHILD") == "1" {
		schedulerJobCrashChild(t)
		return
	}
	f := openSchedulerFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	f.ctx = ctx
	t.Run("opaque snapshot rollback and finish", func(t *testing.T) {
		f := f.sub(t)
		f.exec("DELETE FROM scheduler_jobs")
		run := f.begin("CI job writes")
		schedulerSeedJob(f, "CI_owned", nil)
		lease := schedulerRequireLease(t, f, run, "CI_owned")
		view := lease.Job()
		view.ID = "CI_other"
		view.Payload[0] = 'x'
		failure := errors.New("CI rollback")
		e := lease.WithinOwned(f.ctx, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(f.ctx, "UPDATE scheduler_jobs SET last_error='CI_partial' WHERE id='CI_owned'")
			if e != nil {
				return e
			}
			return failure
		})
		if !errors.Is(e, failure) || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE last_error='CI_partial'") != 0 {
			t.Fatal("owned partial write escaped rollback")
		}
		if lease.Finish(f.ctx, parser.FinishOptions{Payload: json.RawMessage(`{"s":"\ud800"}`)}) == nil || lease.AssertActive(f.ctx) != nil || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_owned' AND status='running' AND payload='{}' AND completed_at IS NULL") != 1 {
			t.Fatal("target JSON refusal lost owned job or escaped rollback")
		}
		if lease.WithinOwned(f.ctx, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(f.ctx, "UPDATE scheduler_jobs SET last_error='CI_saved' WHERE id='CI_owned'")
			return e
		}) != nil {
			t.Fatal("owned write failed")
		}
		if lease.Finish(f.ctx, parser.FinishOptions{Status: "pending", Deferred: true, Delay: time.Minute}) != nil {
			t.Fatal("defer failed")
		}
		if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_owned' AND status='pending' AND attempts=0 AND payload='{}' AND locked_by='' AND locked_until=0 AND completed_at IS NULL") != 1 {
			t.Fatal("defer contract or opaque payload failed")
		}
		called := false
		if !errors.Is(lease.WithinOwned(f.ctx, func(*sql.Tx) error { called = true; return nil }), parser.ErrJobLeaseLost) || called {
			t.Fatal("released lease wrote")
		}
		f.finish(run)
	})
	t.Run("stale owner run and generation", func(t *testing.T) {
		f := f.sub(t)
		f.exec("DELETE FROM scheduler_jobs")
		run := f.begin("CI stale fences")
		schedulerSeedJob(f, "CI_stale", nil)
		lease := schedulerRequireLease(t, f, run, "CI_stale")
		var originalOwner string
		if f.db.QueryRowContext(f.ctx, "SELECT locked_by FROM scheduler_jobs WHERE id='CI_stale'").Scan(&originalOwner) != nil {
			t.Fatal("missing original owner")
		}
		for _, kind := range []string{"owner", "expiry", "run", "generation"} {
			f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", f.repo.Generation)
			f.exec("UPDATE scheduler_runs SET lease_until=? WHERE id=?", run.Run.LeaseUntil, run.Run.ID)
			f.exec("UPDATE scheduler_jobs SET locked_by=?,locked_until=? WHERE id='CI_stale'", originalOwner, lease.Job().LockedUntil)
			if kind == "owner" {
				f.exec("UPDATE scheduler_jobs SET locked_by='CI_new_owner' WHERE id='CI_stale'")
			}
			if kind == "expiry" {
				f.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE id='CI_stale'")
			}
			if kind == "run" {
				f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE id=?", run.Run.ID)
			}
			if kind == "generation" {
				f.exec("UPDATE app_runtime_generation SET generation='22222222-2222-4333-8444-555555555555' WHERE id=1")
			}
			called := false
			e := lease.WithinOwned(f.ctx, func(*sql.Tx) error { called = true; return nil })
			if e == nil || called || lease.Finish(f.ctx, parser.FinishOptions{}) == nil || lease.AssertActive(f.ctx) == nil {
				t.Fatal("stale fence permitted a mutation", kind)
			}
		}
		f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", f.repo.Generation)
		f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE id=?", run.Run.ID)
		f.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE id='CI_stale'")
		next := f.begin("CI recovered stale")
		replacement := schedulerRequireLease(t, f, next, "CI_stale")
		if replacement.Job().Attempts != 2 || lease.Finish(f.ctx, parser.FinishOptions{}) == nil {
			t.Fatal("recovery reused old owner")
		}
		if replacement.Finish(f.ctx, parser.FinishOptions{}) != nil {
			t.Fatal("replacement owner cannot finish")
		}
		f.finish(next)
	})
	t.Run("late driver faults rollback claim and finish", func(t *testing.T) {
		f := f.sub(t)
		f.exec("DELETE FROM scheduler_jobs")
		run := f.begin("CI late job faults")
		schedulerSeedJob(f, "CI_fault", nil)
		faulted, fired := schedulerFaultRepository(t, f.repo.Generation, "UPDATE scheduler_jobs SET status='running'")
		id := "CI_fault"
		if _, e := faulted.ClaimJob(f.ctx, run.Run.ID, parser.ClaimOptions{JobID: &id}); !errors.Is(e, parser.ErrRunStorage) || !fired.Load() {
			t.Fatal("real claim fault not injected", e)
		}
		if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id=? AND status='pending' AND attempts=0 AND locked_by=''", id) != 1 {
			t.Fatal("failed claim left owner")
		}
		finishRepo, finishFired := schedulerFaultRepository(t, f.repo.Generation, "UPDATE scheduler_jobs SET status=?")
		lease, e := finishRepo.ClaimJob(f.ctx, run.Run.ID, parser.ClaimOptions{JobID: &id})
		if e != nil || lease == nil {
			t.Fatal("retry claim failed", e)
		}
		if e = lease.Finish(f.ctx, parser.FinishOptions{}); !errors.Is(e, parser.ErrRunStorage) || !finishFired.Load() {
			t.Fatal("real finish fault not injected", e)
		}
		if lease.AssertActive(f.ctx) != nil || f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id=? AND status='running' AND attempts=1 AND completed_at IS NULL", id) != 1 {
			t.Fatal("failed finish lost owner")
		}
		if lease.Finish(f.ctx, parser.FinishOptions{}) != nil {
			t.Fatal("finish retry failed")
		}
		f.finish(run)
	})
	t.Run("two actual connections claim once and exclude same candidate", func(t *testing.T) {
		f := f.sub(t)
		f.exec("DELETE FROM scheduler_jobs")
		run := f.begin("CI competing jobs")
		schedulerSeedJob(f, "CI_candidate_a", 987654321)
		schedulerSeedJob(f, "CI_candidate_b", 987654321)
		_, a := schedulerDB(t)
		_, b := schedulerDB(t)
		a.SetMaxOpenConns(1)
		b.SetMaxOpenConns(1)
		var ai, bi int64
		if a.QueryRow("SELECT CONNECTION_ID()").Scan(&ai) != nil || b.QueryRow("SELECT CONNECTION_ID()").Scan(&bi) != nil || ai == bi {
			t.Fatal("two different SQL connections required")
		}
		held, e := f.db.BeginTx(f.ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		defer held.Rollback()
		var gen string
		if held.QueryRowContext(f.ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&gen) != nil {
			t.Fatal("cannot hold generation")
		}
		type result struct {
			lease *parser.JobLease
			err   error
		}
		results := make(chan result, 2)
		for _, db := range []*sql.DB{a, b} {
			go func(db *sql.DB) {
				repo := parser.RunRepository{DB: db, Generation: f.repo.Generation}
				lease, e := repo.ClaimJob(f.ctx, run.Run.ID, parser.ClaimOptions{})
				results <- result{lease, e}
			}(db)
		}
		observed := false
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if f.count("SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID IN (?,?) AND INFO LIKE 'SELECT generation FROM app_runtime_generation%FOR UPDATE'", ai, bi) == 2 {
				observed = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !observed {
			t.Fatal("both claimers must actually wait on SQL lock")
		}
		if held.Commit() != nil {
			t.Fatal("cannot release contenders")
		}
		x, y := <-results, <-results
		if x.err != nil || y.err != nil {
			t.Fatal("concurrent claim failed", x.err, y.err)
		}
		if x.lease == nil {
			x, y = y, x
		}
		if x.lease == nil || y.lease != nil || x.lease.Job().ID != "CI_candidate_a" {
			t.Fatal("candidate claimed twice")
		}
		if x.lease.Finish(f.ctx, parser.FinishOptions{}) != nil {
			t.Fatal("first candidate finish failed")
		}
		second := schedulerRequireLease(t, f, run, "CI_candidate_b")
		if second.Finish(f.ctx, parser.FinishOptions{}) != nil {
			t.Fatal("released candidate did not progress")
		}
		f.finish(run)
	})
	t.Run("SIGKILL rolls back owned TX and fresh process recovers claim", func(t *testing.T) {
		f := f.sub(t)
		cmd := exec.Command(os.Args[0], "-test.run=^TestMariaDBSchedulerJobOwnershipDisposable$", "-test.count=1")
		cmd.Env = append(os.Environ(), "ITMAP_JOB_CRASH_CHILD=1")
		pipe, e := cmd.StdoutPipe()
		if e != nil || cmd.Start() != nil {
			t.Fatal("cannot start owned job crash child")
		}
		defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
		ready := make(chan bool, 1)
		go func() {
			s := bufio.NewScanner(pipe)
			for s.Scan() {
				if s.Text() == "CI_JOB_TX_READY" {
					ready <- true
					return
				}
			}
			ready <- false
		}()
		select {
		case ok := <-ready:
			if !ok {
				t.Fatal("crash child not ready")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("crash readiness timeout")
		}
		if cmd.Process.Kill() != nil || cmd.Wait() == nil {
			t.Fatal("owned crash child was not killed")
		}
		if f.count("SELECT COUNT(*) FROM scheduler_jobs WHERE id='CI_crash_owned' AND status='running' AND attempts=1 AND last_error='' AND locked_by<>''") != 1 {
			t.Fatal("SIGKILL lost committed claim or kept partial SQL")
		}
		f.exec("UPDATE scheduler_runs SET lease_until=0 WHERE status='running'")
		f.exec("UPDATE scheduler_jobs SET locked_until=0 WHERE id='CI_crash_owned'")
		_, freshDB := schedulerDB(t)
		fresh := *f
		fresh.repo = parser.RunRepository{DB: freshDB, Generation: f.repo.Generation}
		next := fresh.begin("CI after owned crash")
		lease := schedulerRequireLease(t, &fresh, next, "CI_crash_owned")
		if lease.Job().Attempts != 2 || lease.Finish(f.ctx, parser.FinishOptions{}) != nil {
			t.Fatal("fresh recovery failed")
		}
		fresh.finish(next)
	})
}
func schedulerJobCrashChild(t *testing.T) {
	ctx, db := schedulerDB(t)
	f := &schedulerFixture{t: t, ctx: ctx, db: db, repo: parser.RunRepository{DB: db, Generation: os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION")}}
	run := f.begin("CI owned crash child")
	schedulerSeedJob(f, "CI_crash_owned", nil)
	lease := schedulerRequireLease(t, f, run, "CI_crash_owned")
	if e := lease.WithinOwned(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, "UPDATE scheduler_jobs SET last_error='CI_uncommitted' WHERE id='CI_crash_owned'"); e != nil {
			return e
		}
		fmt.Println("CI_JOB_TX_READY")
		for {
			time.Sleep(time.Second)
		}
	}); e != nil {
		t.Fatal(e)
	}
}
