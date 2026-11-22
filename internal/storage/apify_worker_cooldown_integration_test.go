//go:build mariadb && schedulerwrites

package storage

import (
	"errors"
	"sync"
	"testing"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/integrations/apify"
)

func TestMariaDBApifyWorkerCooldownDisposable(t *testing.T) {
	f := openApifyRuntimeFixture(t)
	p := &runtimeProvider{}
	service := runtimeService(f, p)
	service.Repository.EnforceActorCooldown = true
	opts := apify.HHOptions{}
	old := "11111111-2222-4333-8444-555555555555"
	if old == f.v.Generation {
		old = "11111111-2222-4333-8444-555555555556"
	}
	if _, err := f.db.ExecContext(f.ctx, `INSERT INTO apify_runs(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,reserved_usd,charged_usd,state,run_id,dataset_id,created_at,updated_at) VALUES('CI_old_cooldown',?,'CI_old','free','CIHistory','CIhash','CIstart','CIend',0.25,0.1,'succeeded','CIOldRun','CIOldData',1,CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED))`, old); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(f.ctx, f.v, "CI initial", opts); !errors.Is(err, apifyruns.ErrCooldown) || p.starts.Load() != 0 {
		t.Fatal("historical cooldown bypassed", err)
	}
	if _, err := f.db.ExecContext(f.ctx, "UPDATE apify_runs SET updated_at=1 WHERE id='CI_old_cooldown'"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(f.ctx, f.v, "CI initial", opts); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(f.ctx, f.v, "CI initial", opts); err != nil {
		t.Fatal("known run did not settle", err)
	}
	if _, err := service.Execute(f.ctx, f.v, "CI initial", opts); err != nil || p.starts.Load() != 1 {
		t.Fatal("existing result blocked", err)
	}
	if _, err := service.Execute(f.ctx, f.v, "CI detail", opts); !errors.Is(err, apifyruns.ErrCooldown) || p.starts.Load() != 1 {
		t.Fatal("cooldown bypassed", err)
	}
	if _, err := f.db.ExecContext(f.ctx, "UPDATE apify_runs SET updated_at=CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)-? WHERE id<>'CI_old_cooldown'", (apifyruns.ActorCooldown - 20*time.Second).Milliseconds()); err != nil {
		t.Fatal(err)
	}
	_, waitErr := service.Execute(f.ctx, f.v, "CI detail", opts)
	var cooldown *apifyruns.CooldownError
	if !errors.As(waitErr, &cooldown) || cooldown.RetryAfter < 15*time.Second || cooldown.RetryAfter > 20*time.Second || p.starts.Load() != 1 {
		t.Fatal("incorrect remaining cooldown", waitErr)
	}
	if _, err := f.db.ExecContext(f.ctx, "UPDATE apify_runs SET updated_at=CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3))*1000 AS SIGNED)-?", (apifyruns.ActorCooldown + time.Second).Milliseconds()); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for _, key := range []string{"CI listing", "CI detail"} {
		wg.Add(1)
		go func(key string) { defer wg.Done(); _, err := service.Execute(f.ctx, f.v, key, opts); outcomes <- err }(key)
	}
	wg.Wait()
	close(outcomes)
	success, denied := 0, 0
	for err := range outcomes {
		if err == nil {
			success++
		} else if errors.Is(err, apifyruns.ErrBusy) || errors.Is(err, apifyruns.ErrCurrentUnresolved) {
			denied++
		} else {
			t.Fatal("unexpected concurrent result", err)
		}
	}
	if success != 1 || denied != 1 || p.starts.Load() != 2 {
		t.Fatal("concurrent workers started extra Actor", success, denied, p.starts.Load())
	}
}
