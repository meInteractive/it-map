//go:build mariadb && backupbrowser

package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"itmap/internal/applications"
	"itmap/internal/auth"
	"itmap/internal/backup"
	"itmap/internal/catalog"
	"itmap/internal/channels"
	"itmap/internal/crm"
	"itmap/internal/httpapi"
	"itmap/internal/roles"
	"itmap/internal/tasks"
	"itmap/internal/vacancies"
)

type backupBrowserStatus struct {
	http.ResponseWriter
	code int
}

func (w *backupBrowserStatus) WriteHeader(code int) {
	w.code = code
	w.ResponseWriter.WriteHeader(code)
}

func TestMariaDBBackupBrowserDisposable(t *testing.T) {
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("backup browser requires disposable GitHub CI")
		}
		t.Skip("backup browser requires disposable GitHub CI")
	}
	_, db := openDisposableMariaDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	var baseline schemaBaseline
	if e := json.Unmarshal(baselineJSON, &baseline); e != nil {
		t.Fatal(e)
	}
	expected := map[string]int{"app_runtime_generation": 1, "app_catalog": 1, "company_crm": 1, "company_identity": 1, "company_vacancies": 1, "company_applications": 1, "company_application_status_history": 1, "company_tasks": 1}
	for table := range baseline.Tables {
		var n int
		if e := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `"+table+"`").Scan(&n); e != nil || n != expected[table] {
			t.Fatal("backup browser requires exactly the reviewed fresh synthetic baseline")
		}
	}
	generation := func() (string, error) {
		var g string
		e := db.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&g)
		return g, e
	}
	initial, e := generation()
	if e != nil {
		t.Fatal("cannot inspect guarded browser generation")
	}
	root, e := filepath.Abs(filepath.Join("..", ".."))
	if e != nil {
		t.Fatal(e)
	}
	web := filepath.Join(root, "dist", "web")
	if _, e = os.Stat(filepath.Join(web, "index.html")); e != nil {
		t.Fatal("required built backup SPA missing")
	}
	const password = "CI_SYNTHETIC_BACKUP_BROWSER_PASSWORD"
	manager, e := auth.NewManager(strings.Repeat("j", 64), map[string]string{"eugene": password, "nastya": password, "demo": password})
	if e != nil {
		t.Fatal(e)
	}
	cat := catalog.Repository{DB: db}
	vac := vacancies.Repository{DB: db}
	crmRepo := crm.Repository{DB: db}
	apps := applications.Repository{DB: db}
	taskRepo := tasks.Repository{DB: db}
	backupRepo := backup.Repository{DB: db}
	handler := httpapi.NewHandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{
		DB: db, WebDir: web, Auth: manager, Catalog: cat, Compact: cat, Channels: channels.Repository{DB: db}, Roles: roles.Repository{DB: db},
		Vacancies: vac, VacancyDetails: vac, Applications: apps, ApplicationCompanies: apps, Tasks: taskRepo,
		CompanyFlags: crmRepo, CompanyFlagWrites: crmRepo, CompanyCRM: crmRepo, BackupExports: backupRepo, BackupRestores: backupRepo,
	})
	var successful atomic.Int64
	var unsafe atomic.Bool
	// Observe the real application; never mock a backup response. Every validation
	// and denied restore must preserve generation; each accepted restore rotates it.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/backup" && r.URL.Path != "/api/backup/validate" {
			handler.ServeHTTP(w, r)
			return
		}
		before, e := generation()
		if e != nil {
			unsafe.Store(true)
			http.Error(w, "CI generation unavailable", 503)
			return
		}
		observed := &backupBrowserStatus{ResponseWriter: w}
		handler.ServeHTTP(observed, r)
		after, e := generation()
		if e != nil {
			unsafe.Store(true)
			return
		}
		if r.URL.Path == "/api/backup" && observed.code == 200 {
			successful.Add(1)
			if after == before {
				unsafe.Store(true)
			}
		} else if after != before {
			unsafe.Store(true)
		}
	}))
	defer server.Close()
	command := exec.CommandContext(ctx, "node", "node_modules/@playwright/test/cli.js", "test", "--config=playwright.restore.config.ts", "--reporter=json")
	command.Dir = root
	command.WaitDelay = 5 * time.Second
	command.Env = append(os.Environ(), "ITMAP_BACKUP_BROWSER_WRITES=1", "ITMAP_BROWSER_URL="+server.URL, "ITMAP_SMOKE_PASSWORD="+password)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	e = command.Run()
	var report struct {
		Stats  struct{ Expected, Skipped, Unexpected, Flaky int }
		Errors []struct{ Message string }
	}
	if parse := json.Unmarshal(stdout.Bytes(), &report); parse != nil {
		t.Logf("synthetic backup browser stderr: %s", stderr.String())
		t.Fatal("required backup browser JSON report missing")
	}
	if e != nil || report.Stats.Expected != 6 || report.Stats.Skipped != 0 || report.Stats.Unexpected != 0 || report.Stats.Flaky != 0 {
		t.Logf("synthetic backup browser report: %s", stdout.String())
		t.Fatal("required desktop/mobile backup scenarios failed")
	}
	if unsafe.Load() || successful.Load() != 2 {
		t.Fatal("validation/restore generation boundary or exact restore count failed")
	}
	final, e := generation()
	if e != nil || final == initial {
		t.Fatal("browser did not rotate restore generation")
	}
	var favorites int
	if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM company_user_state WHERE favorite<>0").Scan(&favorites); e != nil || favorites != 0 {
		t.Fatal("browser did not restore the saved synthetic state")
	}
	for _, table := range []string{"company_vacancies", "company_applications", "company_tasks"} {
		var n int
		if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); e != nil || n != 1 {
			t.Fatal("browser restore lost baseline business records")
		}
	}
	t.Log("PASS: 6 desktop/mobile actual backup UI scenarios; 2 confirmed SQL restores, validation/denials preserve generation, zero skips")
}
