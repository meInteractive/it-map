//go:build mariadb && profilewrites

package storage

import (
	"context"
	"errors"
	"itmap/internal/aiproviders"
	"itmap/internal/integrations/aihttp"
	"strings"
	"sync"
	"testing"
)

func TestMariaDBAIKeySettingsDisposable(t *testing.T) {
	f := openProfileResumeFixture(t)
	for _, table := range []string{"openai_settings", "openrouter_settings"} {
		if f.count("SELECT COUNT(*) FROM "+table) != 0 {
			t.Fatal("AI fixture requires empty synthetic tables")
		}
		table := table
		t.Cleanup(func() { f.exec("DELETE FROM " + table + " WHERE id=1") })
	}
	r := aiproviders.NewRepository(f.db, "CI_SYNTHETIC_SECRET", "CI_ENV_KEY")
	g, e := r.Generation(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []aihttp.Provider{aihttp.OpenAI, aihttp.OpenRouter} {
		fault, db := openMutationFaultPool(t, "")
		read := aiproviders.NewRepository(db, "CI_SYNTHETIC_SECRET", "")
		s, e := read.Read(f.ctx, p)
		fault.mu.Lock()
		writes := fault.writes
		fault.mu.Unlock()
		if e != nil || s.Configured || writes != 0 {
			t.Fatal("GET wrote storage")
		}
		prefix := "sk-"
		if p == aihttp.OpenRouter {
			prefix += "or-"
		}
		key := prefix + strings.Repeat("x", 20)
		if e = r.SaveKey(f.ctx, p, "stale-generation", key); !errors.Is(e, aiproviders.ErrGeneration) {
			t.Fatal("stale generation accepted")
		}
		if e = r.SaveKey(f.ctx, p, g, key); e != nil {
			t.Fatal(e)
		}
		got, e := r.Resolve(f.ctx, p)
		if e != nil || got != key {
			t.Fatal("encrypted key roundtrip failed")
		}
		other := aiproviders.NewRepository(f.db, "DIFFERENT_SYNTHETIC_SECRET", "CI_ENV_KEY")
		if _, e = other.Resolve(f.ctx, p); !errors.Is(e, aiproviders.ErrDecrypt) {
			t.Fatal("decrypt failure fell back to env or erased key")
		}
		s, e = r.Read(f.ctx, p)
		if e != nil || !s.Configured || s.KeyMask != "••••xxxx" || s.Revision != 1 {
			t.Fatal("public key projection differs")
		}
		var wg sync.WaitGroup
		results := make(chan error, 4)
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); results <- r.SaveKey(context.Background(), p, g, key) }()
		}
		wg.Wait()
		close(results)
		for e := range results {
			if e != nil {
				t.Fatal(e)
			}
		}
		s, e = r.Read(f.ctx, p)
		if e != nil || s.Revision != 5 {
			t.Fatal("concurrent saves lost a revision")
		}
	}
	if f.count("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE()") != 72 {
		t.Fatal("provider save changed schema")
	}
}
