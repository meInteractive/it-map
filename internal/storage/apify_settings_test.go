//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"database/sql"
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

func TestMariaDBApifyHHSourceDisposable(t *testing.T) {
	ctx, db := schedulerDB(t)
	var count int
	if err := db.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM app_catalog)+(SELECT COUNT(*) FROM company_identity WHERE id=991201)").Scan(&count); err != nil || count != 0 {
		t.Fatal("HH source fixture must start empty", err)
	}
	const first = `{"companies":[{"name":"CI HH Company","vacancySources":{"hh":"https://hh.ru/employer/123"}}],"agencies":[]}`
	const changed = `{"companies":[{"name":"CI HH Company","vacancySources":{"hh":"https://hh.ru/employer/456"}}],"agencies":[]}`
	if _, err := db.ExecContext(ctx, "INSERT INTO company_identity(id,company_name) VALUES(991201,'CI HH Company')"); err != nil {
		t.Fatal("identity fixture", err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(context.Background(), "DELETE FROM vacancy_source_parsing WHERE company_id=991201"); err != nil {
			t.Error("source cleanup")
		}
		if _, err := db.ExecContext(context.Background(), "DELETE FROM company_identity WHERE id=991201"); err != nil {
			t.Error("identity cleanup")
		}
		if _, err := db.ExecContext(context.Background(), "DELETE FROM app_catalog WHERE id=1"); err != nil {
			t.Error("catalog cleanup")
		}
	})
	if _, err := db.ExecContext(ctx, "INSERT INTO app_catalog(id,data_json) VALUES(1,?)", first); err != nil {
		t.Fatal("catalog fixture", err)
	}
	r := apifysettings.NewRepository(db, "CI_SECRET")
	generation := os.Getenv("ITMAP_TEST_DISPOSABLE_GENERATION")
	source, err := r.ReadHHSource(ctx, 991201)
	if err != nil || source.URL != "https://hh.ru/employer/123" || source.Revision != 0 || source.Enabled {
		t.Fatal("initial HH source", err)
	}
	input := apifysettings.HHSourceInput{SourceURL: source.URL, Enabled: true, Revision: 0}
	source, err = r.SaveHHSource(ctx, generation, 991201, input)
	if err != nil || !source.Enabled || source.Revision != 1 {
		t.Fatal("save HH source", err)
	}
	if _, err = r.SaveHHSource(ctx, generation, 991201, input); !errors.Is(err, apifysettings.ErrChanged) {
		t.Fatal("stale source revision accepted", err)
	}
	if _, err = r.SaveHHSource(ctx, "11111111-2222-4333-8444-555555555555", 991201, apifysettings.HHSourceInput{SourceURL: source.URL, Revision: 1}); !errors.Is(err, apifysettings.ErrGeneration) {
		t.Fatal("stale generation accepted", err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE vacancy_source_parsing SET auto_enabled=1,next_run_at=123 WHERE company_id=991201 AND source_id='hh'"); err != nil {
		t.Fatal("schedule fixture", err)
	}
	if _, err = db.ExecContext(ctx, "UPDATE app_catalog SET data_json=? WHERE id=1", changed); err != nil {
		t.Fatal("catalog update", err)
	}
	if _, err = r.SaveHHSource(ctx, generation, 991201, apifysettings.HHSourceInput{SourceURL: source.URL, Enabled: true, Revision: 1}); !errors.Is(err, apifysettings.ErrChanged) {
		t.Fatal("outdated catalog URL accepted", err)
	}
	source, err = r.ReadHHSource(ctx, 991201)
	if err != nil || source.URL != "https://hh.ru/employer/456" || source.Enabled {
		t.Fatal("stale source not disabled", err)
	}
	if _, err = r.SaveHHSource(ctx, generation, 991201, apifysettings.HHSourceInput{SourceURL: source.URL, Enabled: true, Revision: 1}); err != nil {
		t.Fatal("updated source", err)
	}
	var auto int
	var next sql.NullInt64
	if err = db.QueryRowContext(ctx, "SELECT auto_enabled,next_run_at FROM vacancy_source_parsing WHERE company_id=991201 AND source_id='hh'").Scan(&auto, &next); err != nil || auto != 0 || next.Valid {
		t.Fatal("old source schedule retained", err)
	}
	if _, err = r.ReadHHSource(ctx, 991202); !errors.Is(err, apifysettings.ErrSourceMissing) {
		t.Fatal("unknown owner accepted", err)
	}
}
