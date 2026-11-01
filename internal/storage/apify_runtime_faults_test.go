//go:build mariadb && schedulerwrites && schedulerbilling

package storage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/integrations/apify"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestMariaDBApifyRuntimeFaultsDisposable(t *testing.T) {
	for _, prefix := range []string{"INSERT INTO apify_runs(", "UPDATE apify_runs SET state='uncertain'", "UPDATE apify_runs SET state=?"} {
		t.Run("unknown commit "+prefix, func(t *testing.T) {
			f := openApifyRuntimeFixture(t)
			p := &runtimeProvider{start: func(context.Context) (apify.ActorRun, error) { return runtimeRun("SUCCEEDED", "0.08"), nil }}
			wrapped, fired := billingCommitRepository(t, f.v.Generation, prefix)
			repo := apifyruns.Repository{DB: wrapped.DB, Settings: apifysettings.NewRepository(wrapped.DB, "CI_SECRET")}
			s := runtimeService(f, p)
			s.Repository = repo
			if _, e := s.Execute(f.ctx, f.v, "CI unknown commit", apify.HHOptions{}); e == nil || !fired.Load() {
				t.Fatal("unknown commit fault did not fire", e)
			}
			got, e := runtimeService(f, p).Execute(f.ctx, f.v, "CI unknown commit", apify.HHOptions{})
			if strings.Contains(prefix, "state=?") {
				if e != nil || got.State != "succeeded" || p.starts.Load() != 1 {
					t.Fatal("committed response did not replay", e)
				}
			} else if !errors.Is(e, apifyruns.ErrUncertain) || p.starts.Load() != 0 {
				t.Fatal("unknown commit allowed POST", e)
			}
		})
	}
	t.Run("budget exhaustion sends no POST", func(t *testing.T) {
		f := openApifyRuntimeFixture(t)
		p := &runtimeProvider{}
		p.account = func(ctx context.Context) (apify.BillingAccount, error) {
			a, e := (&runtimeProvider{}).Account(ctx)
			a.ProviderUsedUSD = 4.9
			return a, e
		}
		if _, e := runtimeService(f, p).Execute(f.ctx, f.v, "CI exhausted", apify.HHOptions{}); !errors.Is(e, apifyruns.ErrBudget) || p.starts.Load() != 0 {
			t.Fatal("overspend allowed", e)
		}
	})
	t.Run("source revision and generation fence late response", func(t *testing.T) {
		for _, kind := range []string{"source", "generation"} {
			t.Run(kind, func(t *testing.T) {
				f := openApifyRuntimeFixture(t)
				p := &runtimeProvider{}
				var original string
				p.start = func(context.Context) (apify.ActorRun, error) {
					if kind == "source" {
						if _, e := f.db.ExecContext(f.ctx, "UPDATE vacancy_source_parsing SET revision=revision+1 WHERE company_id=991302"); e != nil {
							t.Fatal(e)
						}
					} else {
						original = f.v.Generation
						if _, e := f.db.ExecContext(f.ctx, "UPDATE app_runtime_generation SET generation='00000000-0000-4000-8000-000000000099' WHERE id=1"); e != nil {
							t.Fatal(e)
						}
						t.Cleanup(func() {
							if _, e := f.db.ExecContext(context.Background(), "UPDATE app_runtime_generation SET generation=? WHERE id=1", original); e != nil {
								t.Error("generation cleanup")
							}
						})
					}
					return runtimeRun("SUCCEEDED", "0.08"), nil
				}
				if _, e := runtimeService(f, p).Execute(f.ctx, f.v, "CI fence", apify.HHOptions{}); e == nil {
					t.Fatal("stale provider response accepted")
				}
				var n int
				if e := f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM apify_runs WHERE state='uncertain' AND run_id='' AND charged_usd IS NULL").Scan(&n); e != nil || n != 1 {
					t.Fatal("stale response released reserve", e)
				}
			})
		}
	})
}

func TestMariaDBApifyRuntimeCrashDisposable(t *testing.T) {
	for _, point := range []string{"before-post", "after-post"} {
		t.Run(point, func(t *testing.T) {
			f := openApifyRuntimeFixture(t)
			exe, e := os.Executable()
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(f.v)
			child := exec.Command(exe, "-test.run=^TestMariaDBApifyRuntimeCrashChild$", "-test.v")
			child.Env = append(os.Environ(), "ITMAP_APIFY_CRASH_POINT="+point, "ITMAP_APIFY_CRASH_VERSION="+string(raw))
			stdout, e := child.StdoutPipe()
			if e != nil {
				t.Fatal(e)
			}
			var stderr bytes.Buffer
			child.Stderr = &stderr
			if e = child.Start(); e != nil {
				t.Fatal(e)
			}
			defer func() { child.Process.Kill(); child.Wait() }()
			ready := make(chan bool, 1)
			go func() {
				scan := bufio.NewScanner(stdout)
				for scan.Scan() {
					if scan.Text() == "CI_APIFY_CRASH_READY" {
						ready <- true
						return
					}
				}
				ready <- false
			}()
			select {
			case ok := <-ready:
				if !ok {
					t.Fatal("child exited before crash point")
				}
			case <-time.After(15 * time.Second):
				t.Fatal("child never reached crash point")
			}
			if e = child.Process.Kill(); e != nil {
				t.Fatal(e)
			}
			_ = child.Wait()
			p := &runtimeProvider{}
			got, e := runtimeService(f, p).Execute(f.ctx, f.v, "CI crash", apify.HHOptions{})
			if !errors.Is(e, apifyruns.ErrUncertain) || got.State != "uncertain" || got.ChargedUSD != nil || p.starts.Load() != 0 {
				t.Fatal("restart resurrected crashed POST", e)
			}
		})
	}
}
func TestMariaDBApifyRuntimeCrashChild(t *testing.T) {
	point := os.Getenv("ITMAP_APIFY_CRASH_POINT")
	if point == "" {
		t.Skip("only subprocess crash probe")
	}
	ctx, db := schedulerDB(t)
	var v apifysettings.OperationVersion
	if json.Unmarshal([]byte(os.Getenv("ITMAP_APIFY_CRASH_VERSION")), &v) != nil {
		t.Fatal("invalid synthetic crash version")
	}
	p := &runtimeProvider{}
	block := func() { fmt.Println("CI_APIFY_CRASH_READY"); <-ctx.Done() }
	if point == "after-post" {
		p.start = func(context.Context) (apify.ActorRun, error) { block(); return apify.ActorRun{}, ctx.Err() }
	}
	calls := 0
	s := apifyruns.Service{Repository: apifyruns.Repository{DB: db, Settings: apifysettings.NewRepository(db, "CI_SECRET")}, NewClient: func(string) (apifyruns.Provider, error) {
		calls++
		if point == "before-post" && calls == 2 {
			block()
		}
		return p, nil
	}}
	if _, e := s.Execute(ctx, v, "CI crash", apify.HHOptions{}); e != nil {
		t.Fatal(e)
	}
	t.Fatal("crash probe was not killed")
}
