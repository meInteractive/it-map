//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"errors"
	"fmt"
	"itmap/internal/apifysettings"
	"os"
	"strings"
	"sync"
	"testing"
)

func TestMariaDBApifySettingsDisposable(t *testing.T) {
	ctx, db := schedulerDB(t)
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM apify_settings").Scan(&count); err != nil || count != 0 {
		t.Fatal("Apify settings fixture must start empty", err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(), "DELETE FROM apify_settings WHERE id=1"); err != nil {
			t.Error("fixture cleanup failed")
		}
	})
	r := apifysettings.NewRepository(db, strings.Repeat("s", 64))
	generation := os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION")
	key := "apify_api_" + strings.Repeat("x", 16)
	view, err := r.SaveKey(ctx, generation, apifysettings.KeyInput{Profile: "free", APIKey: key, Revision: 0})
	if err != nil || view.Revision != 1 || !view.Profiles["free"].Configured || view.Profiles["free"].KeyMask != "••••xxxx" {
		t.Fatal("free key save", err)
	}
	var stored string
	if err = db.QueryRowContext(ctx, "SELECT free_key_encrypted FROM apify_settings WHERE id=1").Scan(&stored); err != nil || strings.Contains(stored, key) || stored == "" {
		t.Fatal("key not encrypted", err)
	}
	resolved, err := r.Resolve(ctx, "free")
	if err != nil || resolved != key {
		t.Fatal("key roundtrip", err)
	}
	if _, err = apifysettings.NewRepository(db, "CI_OTHER_SECRET").Resolve(ctx, "free"); !errors.Is(err, apifysettings.ErrDecrypt) {
		t.Fatal("wrong secret accepted", err)
	}
	if _, err = r.SaveSettings(ctx, generation, apifysettings.SettingsInput{Enabled: true, Revision: 1}); err != nil {
		t.Fatal("enable", err)
	}
	if _, err = r.SaveKey(ctx, "11111111-2222-4333-8444-555555555555", apifysettings.KeyInput{Profile: "starter", APIKey: key, Revision: 2}); !errors.Is(err, apifysettings.ErrGeneration) {
		t.Fatal("stale generation accepted", err)
	}
	if _, err = r.SaveKey(ctx, generation, apifysettings.KeyInput{Profile: "starter", APIKey: key, Revision: 1}); !errors.Is(err, apifysettings.ErrChanged) {
		t.Fatal("stale revision accepted", err)
	}
	if _, err = r.SaveKey(ctx, generation, apifysettings.KeyInput{Profile: "starter", APIKey: key, Revision: 2}); err != nil {
		t.Fatal("starter key", err)
	}
	if _, err = r.SaveSettings(ctx, generation, apifysettings.SettingsInput{Enabled: true, PaidAllowed: true, Revision: 3}); !errors.Is(err, apifysettings.ErrInput) {
		t.Fatal("paid consent omitted", err)
	}
	if _, err = r.SaveSettings(ctx, generation, apifysettings.SettingsInput{Enabled: true, PaidAllowed: true, Confirmed: true, Revision: 3}); err != nil {
		t.Fatal("paid consent", err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := r.SaveKey(ctx, generation, apifysettings.KeyInput{Profile: "free", APIKey: key + fmt.Sprint(i), Revision: 4})
			results <- e
		}(i)
	}
	wg.Wait()
	close(results)
	won, stale := 0, 0
	for e := range results {
		if e == nil {
			won++
		} else if errors.Is(e, apifysettings.ErrChanged) {
			stale++
		} else {
			t.Fatal("concurrent save", e)
		}
	}
	if won != 1 || stale != 1 {
		t.Fatalf("CAS: wins=%d stale=%d", won, stale)
	}
	view, err = r.Read(ctx)
	if err != nil || view.Revision != 5 || view.PaidAllowed {
		t.Fatal("key rotation must disable paid mode", err)
	}
}
