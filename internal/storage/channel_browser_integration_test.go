//go:build mariadb && channelwrites && channelbrowser

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
    "itmap/internal/vacancies"
)

// Изолированная реальная MariaDB и браузер только внутри согласованного disposable CI.
func TestMariaDBChannelBrowserDisposable(t *testing.T) {
    f:=openChannelFixture(t)
    ctx,cancel:=context.WithTimeout(context.Background(),150*time.Second)
    defer cancel()
    root,err:=filepath.Abs(filepath.Join("..",".."))
    if err!=nil {t.Fatal(err)}
    web:=filepath.Join(root,"dist","web")
    if _,err:=os.Stat(filepath.Join(web,"index.html"));err!=nil {t.Fatal("нужна собранная SPA")}
    const password="CI_SYNTHETIC_CHANNEL_BROWSER_PASSWORD"
    manager,err:=auth.NewManager(strings.Repeat("b",64),map[string]string{"eugene":password,"demo":password,"nastya":password})
    if err!=nil {t.Fatal("синтетическая авторизация недоступна")}
    cat:=catalog.Repository{DB:f.db}
    vac:=vacancies.Repository{DB:f.db}
    handler:=httpapi.NewHandlerWithOptions(slog.New(slog.NewTextHandler(io.Discard,nil)),httpapi.Options{
        DB:f.db,WebDir:web,Auth:manager,Catalog:cat,Compact:cat,Channels:f.repo,ChannelWrites:f.repo,Vacancies:vac,VacancyDetails:vac,
    })
    server:=httptest.NewServer(handler)
    defer server.Close()
    cmd:=exec.CommandContext(ctx,"node","node_modules/@playwright/test/cli.js","test","--config=playwright.channels.config.ts","--reporter=json")
    cmd.Dir=root
    cmd.WaitDelay=5*time.Second
    cmd.Env=append(os.Environ(),"ITMAP_CHANNEL_BROWSER_WRITES=1","ITMAP_BROWSER_URL="+server.URL,"ITMAP_SMOKE_PASSWORD="+password)
    var stdout,stderr bytes.Buffer
    cmd.Stdout=&stdout
    cmd.Stderr=&stderr
    resultErr:=cmd.Run()
    var report struct{Stats struct{Expected,Skipped,Unexpected,Flaky int}}
    if json.Unmarshal(stdout.Bytes(),&report)!=nil {t.Logf("stderr: %s",stderr.String());t.Fatal("browser JSON unavailable")}
    if resultErr!=nil||report.Stats.Expected!=2||report.Stats.Skipped!=0||report.Stats.Unexpected!=0||report.Stats.Flaky!=0 {
        t.Logf("browser report: %s",stdout.String())
        t.Fatalf("channel desktop/mobile: expected=%d unexpected=%d skipped=%d",report.Stats.Expected,report.Stats.Unexpected,report.Stats.Skipped)
    }
    if f.count("SELECT COUNT(*) FROM telegram_channels WHERE name LIKE 'CI Browser Channel %'")!=2 {
        t.Fatal("каналы desktop/mobile не сохранены в MariaDB")
    }
    if f.count("SELECT COUNT(*) FROM telegram_channels WHERE comment LIKE 'CI browser comment %'")!=2 {
        t.Fatal("изменённые комментарии не сохранены в MariaDB")
    }
}
