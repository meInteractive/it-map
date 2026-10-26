//go:build mariadb && taskwrites && taskbrowser

package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"itmap/internal/applications"
	"itmap/internal/auth"
	"itmap/internal/catalog"
	"itmap/internal/channels"
	"itmap/internal/crm"
	"itmap/internal/httpapi"
	"itmap/internal/roles"
	"itmap/internal/vacancies"
)

// A real writable Go/router/SPA server exists only while guarded synthetic SQL
// fixtures are owned by this parent. Cleanup restores them BEFORE old smoke.
func TestMariaDBTaskBrowserDisposable(t *testing.T) {
	f := openTaskFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	f.ctx = ctx
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal("cannot locate test source root")
	}
	web := filepath.Join(root, "dist", "web")
	if _, err := os.Stat(filepath.Join(web, "index.html")); err != nil {
		t.Fatal("required built SPA missing before task browser gate")
	}
	const password = "CI_SYNTHETIC_TASK_BROWSER_PASSWORD"
	manager, err := auth.NewManager(strings.Repeat("b", 64), map[string]string{"eugene": password, "demo": password, "nastya": password})
	if err != nil {
		t.Fatal("cannot configure synthetic browser sessions")
	}
	cat := catalog.Repository{DB: f.db}
	vac := vacancies.Repository{DB: f.db}
	crmRepo := crm.Repository{DB: f.db}
	handler := httpapi.NewHandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard, nil)), httpapi.Options{
		DB: f.db, WebDir: web, Auth: manager, Catalog: cat, Compact: cat, Channels: channels.Repository{DB: f.db}, Roles: roles.Repository{DB: f.db},
		Vacancies: vac, VacancyDetails: vac, Applications: applications.Repository{DB: f.db}, ApplicationCompanies: applications.Repository{DB: f.db},
		CompanyFlags: crmRepo, CompanyCRM: crmRepo, Tasks: f.repo, TaskCreates: f.repo, TaskCompletion: f.repo, TaskReschedule: f.repo})
	server := httptest.NewServer(handler)
	defer server.Close()
	command := exec.CommandContext(ctx, "node", "node_modules/@playwright/test/cli.js", "test", "--config=playwright.task-writes.config.ts", "--reporter=json")
	command.Dir = root
	command.WaitDelay = 5 * time.Second
	command.Env = append(os.Environ(), "ITMAP_TASK_BROWSER_WRITES=1", "ITMAP_BROWSER_URL="+server.URL, "ITMAP_SMOKE_PASSWORD="+password)
	var output, stderr bytes.Buffer
	command.Stdout = &output
	command.Stderr = &stderr
	err = command.Run()
	var report struct {
		Stats  struct{ Expected, Skipped, Unexpected, Flaky int }
		Errors []struct{ Message string }
	}
	if parseErr := json.Unmarshal(output.Bytes(), &report); parseErr != nil {
		t.Logf("task browser stderr: %s", stderr.String())
		t.Fatal("task browser did not return a valid required JSON report")
	}
	if err != nil || report.Stats.Expected != 2 || report.Stats.Skipped != 0 || report.Stats.Unexpected != 0 || report.Stats.Flaky != 0 {
		// Only synthetic CI test data is available; never persist cookies or screenshots.
		t.Logf("task browser errors: %s", output.String())
		t.Fatalf("required desktop/mobile task UI scenarios failed: expected=%d skipped=%d unexpected=%d", report.Stats.Expected, report.Stats.Skipped, report.Stats.Unexpected)
	}
	rows, err := f.repo.List(ctx, taskUser, "", true, nil)
	if err != nil || len(rows.Tasks) != 2 {
		t.Fatal("task browser did not commit exactly its two synthetic tasks")
	}
	seen := map[string]bool{}
	for _, row := range rows.Tasks {
		if row.Type != "meeting" || row.DueAt != "2026-10-29T20:45:00.000Z" || row.Completed || row.CompletedAt != nil || row.VacancyID != nil {
			t.Fatal("task UI roundtrip lost persisted fields")
		}
		seen[row.Note] = true
	}
	if !seen["CI synthetic task browser desktop"] || !seen["CI synthetic task browser mobile"] {
		t.Fatal("task browser project rows missing")
	}
	t.Log("PASS: desktop/mobile task create/reload/reschedule/complete/reopen/isolation, zero skips")
}
