//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"itmap/internal/parser"
)

type errorHistoryFrame struct {
	Name, Owner, ReadError string
	Now                    int64
	Rotate                 bool
	Records                []struct {
		Owner     string
		CountsRaw string
		Now       int64
	}
	Errors      []string
	Body        json.RawMessage
	StorageRows []map[string]string
}

func errorHistoryFrames(t *testing.T) []errorHistoryFrame {
	t.Helper()
	cmd := exec.Command("node", "../../scripts/check-parser-error-history.mjs")
	cmd.Stderr = os.Stderr
	raw, e := cmd.Output()
	if e != nil {
		t.Fatal("source history oracle failed")
	}
	d, out := json.NewDecoder(bytes.NewReader(raw)), []errorHistoryFrame{}
	for {
		var v errorHistoryFrame
		e = d.Decode(&v)
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		out = append(out, v)
	}
	if len(out) != 19 {
		t.Fatal("missing source history frames", len(out))
	}
	return out
}
func historyFixture(t *testing.T) *settingsSQLFixture {
	t.Helper()
	f := openSettingsSQL(t)
	var count int
	if f.reader.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM parser_error_daily").Scan(&count) != nil || count != 0 {
		t.Fatal("history namespace must start empty")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, e := f.db.ExecContext(ctx, "DELETE FROM parser_error_daily"); e != nil {
			t.Error("history cleanup failed")
		}
	})
	return f
}
func historyStorageRows(t *testing.T, f *settingsSQLFixture) []map[string]string {
	t.Helper()
	rows, e := f.reader.QueryContext(f.ctx, "SELECT owner_name,day,p1_peak,p2_peak,first_sample_at,last_sample_at FROM parser_error_daily ORDER BY BINARY owner_name,BINARY day")
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var owner, day string
		var p1, p2, first, last int64
		if rows.Scan(&owner, &day, &p1, &p2, &first, &last) != nil {
			t.Fatal("history row scan failed")
		}
		out = append(out, map[string]string{"owner_name": owner, "day": day, "p1_peak": strconv.FormatInt(p1, 10), "p2_peak": strconv.FormatInt(p2, 10), "first_sample_at": strconv.FormatInt(first, 10), "last_sample_at": strconv.FormatInt(last, 10)})
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	return out
}
func historyErrorCode(e error) string {
	if e == nil {
		return "ok"
	}
	if errors.Is(e, parser.ErrSettingsGeneration) {
		return "restored"
	}
	return "storage"
}
func historyPrepare(t *testing.T, f *settingsSQLFixture, owner string, readonly bool) parser.ErrorHistoryOperation {
	t.Helper()
	db := f.db
	if readonly {
		db = f.reader
	}
	op, e := (parser.ErrorHistoryRepository{DB: db}).PrepareHistory(f.ctx, owner)
	if e != nil {
		t.Fatal("cannot prepare trusted history operation")
	}
	return op
}
func TestMariaDBSchedulerErrorHistorySourceDisposable(t *testing.T) {
	requireSettingsCI(t)
	f := historyFixture(t)
	for _, v := range errorHistoryFrames(t) {
		t.Run(v.Name, func(t *testing.T) {
			f.exec("DELETE FROM parser_error_daily")
			f.exec("UPDATE app_runtime_generation SET generation=? WHERE id=1", f.generation)
			reader := historyPrepare(t, f, v.Owner, true)
			ops := map[string]parser.ErrorHistoryOperation{}
			for _, r := range v.Records {
				ops[r.Owner] = historyPrepare(t, f, r.Owner, false)
			}
			before := dashboardFingerprint(f, "parser_error_daily")
			for i, r := range v.Records {
				if v.Rotate {
					f.exec("UPDATE app_runtime_generation SET generation='22222222-3333-4333-8444-555555555555' WHERE id=1")
					before = dashboardFingerprint(f, "parser_error_daily")
				}
				var counts parser.ErrorHistoryCounts
				if json.Unmarshal([]byte(r.CountsRaw), &counts) != nil {
					t.Fatal("invalid counts fixture")
				}
				if got := historyErrorCode(ops[r.Owner].Record(f.ctx, counts, r.Now)); got != v.Errors[i] {
					t.Fatalf("source record outcome differs: %s/%s", got, v.Errors[i])
				}
			}
			beforeRead := dashboardFingerprint(f)
			body, e := reader.Read(f.ctx, v.Now)
			expectedError := v.ReadError
			if expectedError == "" {
				expectedError = "ok"
			}
			if historyErrorCode(e) != expectedError {
				t.Fatal("source history read error differs")
			}
			if e == nil {
				var got, want any
				if json.Unmarshal(body, &got) != nil || json.Unmarshal(v.Body, &want) != nil || !reflect.DeepEqual(got, want) {
					t.Fatal("source history points/average/gaps differ")
				}
			}
			if beforeRead != dashboardFingerprint(f) {
				t.Fatal("history read wrote one of 72 tables")
			}
			if !reflect.DeepEqual(historyStorageRows(t, f), v.StorageRows) {
				t.Fatal("source history full stored rows differ")
			}
			if before != dashboardFingerprint(f, "parser_error_daily") {
				t.Fatal("history record changed another of 71 tables")
			}
		})
	}
	t.Log("PASS: 19 unchanged source SQLite history frames, exact INTEGER storage, owner/day peaks, null gaps/averages, readonly reads and 71-table write isolation")
}
func TestMariaDBSchedulerErrorHistoryOwnershipDisposable(t *testing.T) {
	requireSettingsCI(t)
	const now = int64(1793260800000)
	t.Run("concurrent maxima", func(t *testing.T) {
		f := historyFixture(t)
		op := historyPrepare(t, f, "Eugene", false)
		before := dashboardFingerprint(f, "parser_error_daily")
		done := make(chan error, 12)
		for i := int64(0); i < 12; i++ {
			go func(i int64) { done <- op.Record(f.ctx, parser.ErrorHistoryCounts{P1: i, P2: 12 - i}, now+i*60000) }(i)
		}
		for i := 0; i < 12; i++ {
			if e := <-done; e != nil {
				t.Fatal("concurrent history record failed")
			}
		}
		rows := historyStorageRows(t, f)
		if len(rows) != 1 || rows[0]["p1_peak"] != "11" || rows[0]["p2_peak"] != "12" || rows[0]["first_sample_at"] != strconv.FormatInt(now, 10) || rows[0]["last_sample_at"] != strconv.FormatInt(now+11*60000, 10) || before != dashboardFingerprint(f, "parser_error_daily") {
			t.Fatal("concurrent peaks or first/last samples lost")
		}
	})
	t.Run("readonly cannot record", func(t *testing.T) {
		f := historyFixture(t)
		op := historyPrepare(t, f, "Eugene", true)
		before := dashboardFingerprint(f)
		if op.Record(f.ctx, parser.ErrorHistoryCounts{P1: 1}, now) == nil || before != dashboardFingerprint(f) {
			t.Fatal("readonly history operation wrote")
		}
	})
	t.Run("restore before generation lock", func(t *testing.T) {
		f := historyFixture(t)
		hook := &settingsHook{pauseBegin: true, entered: make(chan struct{}), release: make(chan struct{})}
		db := settingsFaultPool(f, hook, false)
		op, e := (parser.ErrorHistoryRepository{DB: db}).PrepareHistory(f.ctx, "Eugene")
		if e != nil {
			t.Fatal(e)
		}
		var once sync.Once
		release := func() { once.Do(func() { close(hook.release) }) }
		defer release()
		done := make(chan error, 1)
		go func() { done <- op.Record(f.ctx, parser.ErrorHistoryCounts{P1: 1}, now) }()
		awaitSettingsBarrier(t, hook.entered)
		f.exec("UPDATE app_runtime_generation SET generation='22222222-3333-4333-8444-555555555555' WHERE id=1")
		before := dashboardFingerprint(f)
		release()
		if e := <-done; !errors.Is(e, parser.ErrSettingsGeneration) || before != dashboardFingerprint(f) {
			t.Fatal("old history operation wrote after restore")
		}
	})
}
func TestMariaDBSchedulerErrorHistoryFaultsDisposable(t *testing.T) {
	requireSettingsCI(t)
	const now = int64(1793260800000)
	for _, name := range []string{"late upsert rollback", "unknown COMMIT without retry", "late history read fault", "cancelled record"} {
		t.Run(name, func(t *testing.T) {
			f := historyFixture(t)
			hook := &settingsHook{updatePrefix: "INSERT INTO parser_error_daily"}
			reader := false
			switch name {
			case "late upsert rollback":
				hook.failAfterUpdate = true
			case "unknown COMMIT without retry":
				hook.unknownCommit = true
			case "late history read fault":
				hook.query = "SELECT day,p1_peak"
				hook.failQuery = true
				reader = true
			}
			db := settingsFaultPool(f, hook, reader)
			op, e := (parser.ErrorHistoryRepository{DB: db}).PrepareHistory(f.ctx, "Eugene")
			if e != nil {
				t.Fatal(e)
			}
			before := dashboardFingerprint(f)
			if reader {
				body, e := op.Read(f.ctx, now)
				if e == nil || len(body) != 0 || !hook.fired.Load() || !hook.readOnly.Load() {
					t.Fatal("late read fault returned success or used writable TX")
				}
			} else {
				ctx := f.ctx
				if name == "cancelled record" {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				if e = op.Record(ctx, parser.ErrorHistoryCounts{P1: 3, P2: 4}, now); e == nil {
					t.Fatal("injected/cancelled record succeeded")
				}
			}
			if name == "unknown COMMIT without retry" {
				if hook.updates.Load() != 1 || hook.commits.Load() != 1 || len(historyStorageRows(t, f)) != 1 {
					t.Fatal("unknown COMMIT automatically repeated or not visible")
				}
				if e = op.Record(f.ctx, parser.ErrorHistoryCounts{P1: 3, P2: 4}, now); e != nil || len(historyStorageRows(t, f)) != 1 {
					t.Fatal("explicit replay added a second row")
				}
			} else if before != dashboardFingerprint(f) {
				t.Fatal("failed history operation changed data")
			}
			if name == "late upsert rollback" && (!hook.fired.Load() || hook.updates.Load() != 1 || hook.commits.Load() != 0) {
				t.Fatal("late upsert rollback was not exercised")
			}
		})
	}
}
