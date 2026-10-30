//go:build mariadb && crmwrites && companybrowser

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

    "itmap/internal/auth"
    "itmap/internal/catalog"
    "itmap/internal/httpapi"
)

// Только временная CI-БД. Проверяем браузер с настоящими HTTP и SQL без production.
func TestMariaDBCompanyFlagBrowserDisposable(t *testing.T) {
    f := openCRMFlagFixture(t)
    ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
    defer cancel()
    root, err := filepath.Abs(filepath.Join("..", ".."))
    if err != nil { t.Fatal(err) }
    if _, err := os.Stat(filepath.Join(root, "dist", "web", "index.html")); err != nil {
        t.Fatal("перед проверкой нужна собранная SPA")
    }
    const password = "CI_SYNTHETIC_COMPANY_BROWSER_PASSWORD"
    manager, err := auth.NewManager(strings.Repeat("b", 64), map[string]string{"eugene":password, "demo":password, "nastya":password})
    if err != nil { t.Fatal("не создана синтетическая сессия") }
    cat := catalog.Repository{DB:f.db}
    h := httpapi.NewHandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard,nil)),httpapi.Options{
        DB:f.db, WebDir:filepath.Join(root,"dist","web"), Auth:manager, Catalog:cat,Compact:cat,
        CompanyFlags:f.repo, CompanyFlagWrites:f.repo, CompanyCRM:f.repo,
    })
    server := httptest.NewServer(h)
    defer server.Close()
    cmd := exec.CommandContext(ctx,"node","node_modules/@playwright/test/cli.js","test","--config=playwright.company-flags.config.ts","--reporter=json")
    cmd.Dir=root
    cmd.WaitDelay=5*time.Second
    cmd.Env=append(os.Environ(),"ITMAP_COMPANY_BROWSER_WRITES=1","ITMAP_BROWSER_URL="+server.URL,"ITMAP_SMOKE_PASSWORD="+password)
    var stdout, stderr bytes.Buffer
    cmd.Stdout=&stdout
    cmd.Stderr=&stderr
    runErr:=cmd.Run()
    var result struct { Stats struct { Expected, Skipped, Unexpected, Flaky int } }
    if json.Unmarshal(stdout.Bytes(),&result)!=nil {
        t.Logf("browser stderr: %s",stderr.String())
        t.Fatal("браузер не вернул JSON")
    }
    if runErr!=nil || result.Stats.Expected!=2 || result.Stats.Skipped!=0 || result.Stats.Unexpected!=0 || result.Stats.Flaky!=0 {
        t.Logf("browser report: %s",stdout.String())
        t.Fatalf("desktop/mobile flags: expected=%d unexpected=%d skipped=%d",result.Stats.Expected,result.Stats.Unexpected,result.Stats.Skipped)
    }
    if f.count("SELECT COUNT(*) FROM company_user_state WHERE user_name IN ('Demo','Nastya') AND company_name=?",crmFlagCompany)!=0 {
        t.Fatal("браузер оставил личные флаги после выключения")
    }
    t.Log("PASS: desktop/mobile CRM flags, retry, real SQL and user isolation")
}
