//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
    "encoding/json"
    "fmt"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "itmap/internal/parser"
    "itmap/internal/auth"
)

// S11-DIAG-02: SQL-окно из ВСЕХ совпавших jobs, а не из видимой
// пагинированной страницы. Доступ только администратору, без payload.
func TestMariaDBSchedulerQueueDebugLatestTen(t *testing.T) {
    requireSettingsCI(t)
    f := queueReadFixture(t)
    f.exec("DELETE FROM scheduler_jobs")
    f.exec("UPDATE company_identity SET company_name='CI DEBUG COMPANY' WHERE id=1")
    for i:=0;i<28;i++ {
        status := "failed"
        if i>=25 {status="completed"}
        at := int64(1730000000000+i*1000)
        if i==24 {at=1730000000000+23*1000} // tied time, sort by ID desc
        f.exec(`INSERT INTO scheduler_jobs
            (id,generation,dedupe_key,type,company_id,source_id,payload,status,priority,run_after,attempts,max_attempts,last_error,reason_code,created_at,updated_at)
            VALUES(?,?,?,'classify_title',1,'hh',?,?,?,?,2,4,?,?,?,?)`,
            fmt.Sprintf("CI_DEBUG_%02d",i),f.generation,fmt.Sprintf("D_%02d",i),
            `{"secret":"CI_PRIVATE_PAYLOAD"}`,status,int64(i),at,
            "CI_PRIVATE_ERROR","retry_due",at,at)
    }
    f.exec(`INSERT INTO scheduler_jobs
            (id,generation,dedupe_key,type,company_id,source_id,payload,status,priority,run_after,created_at,updated_at)
            VALUES('CI_OTHER_GENERATION','11111111-1111-4111-8111-111111111111','OLD','classify_title',1,'hh','{}','failed',1,0,0,0)`)

    before:=dashboardFingerprint(f)
    handler,tokens:=queueReadHandler(t,parser.QueueRepository{DB:f.reader})
    call:=func(user,method,url string) *httptest.ResponseRecorder {
        t.Helper()
        req:=httptest.NewRequest(method,url,strings.NewReader("{}"))
        if user!="" {req.AddCookie(&http.Cookie{Name:auth.CookieName,Value:tokens[user]})}
        w:=httptest.NewRecorder()
        handler.ServeHTTP(w,req)
        return w
    }
    url:="https://example.invalid/api/scheduler/queue/debug?status=failed&type=classify_title&search=DEBUG"
    admin:=call("Eugene","GET",url)
    if admin.Code!=200 {t.Fatalf("admin read failed: %d %s",admin.Code,admin.Body.String())}
    var response struct {
        Items []struct {
            JobID string `json:"jobId"`
            Status string `json:"status"`
            ReasonCode string `json:"reasonCode"`
            CreatedAt int64 `json:"createdAt"`
        } `json:"items"`
        Total int `json:"total"`
        Limit int `json:"limit"`
        Coverage string `json:"coverage"`
    }
    if json.Unmarshal(admin.Body.Bytes(),&response)!=nil {t.Fatal("invalid JSON")}
    if response.Total!=25||response.Limit!=10||response.Coverage!="server_latest"||len(response.Items)!=10 {
        t.Fatalf("incorrect full queue count or page: total=%d items=%d",response.Total,len(response.Items))
    }
    // Идентичное время ID 24 и 23. При DESC BINARY ID сначала 24.
    if response.Items[0].JobID!="CI_DEBUG_24"||response.Items[1].JobID!="CI_DEBUG_23"||
        response.Items[9].JobID!="CI_DEBUG_15" {t.Fatalf("wrong order: %+v",response.Items)}
    for _,row:=range response.Items {
        if row.Status!="failed"||row.ReasonCode!="retry_due" {t.Fatal("filter or reason lost",row)}
    }
    for _,s:=range []string{"CI_PRIVATE_PAYLOAD","CI_PRIVATE_ERROR","companyName","candidateTitle","last_error","generation","dedupe_key","locked_by"} {
        if strings.Contains(admin.Body.String(),s) {t.Fatal("private field leaked",s)}
    }
    for _,v:=range []struct{user,method,url string;want int}{
        {"Nastya","GET",url,403},{"","GET",url,401},
        {"Eugene","GET",url+"&page=3",400},
        {"Eugene","POST",url,405},
    } {
        w:=call(v.user,v.method,v.url)
        if w.Code!=v.want {t.Fatalf("auth/method/query mismatch %s %s got %d want %d",v.user,v.method,w.Code,v.want)}
    }
    missing:=call("Eugene","GET","https://example.invalid/api/scheduler/queue/debug?status=failed&search=NONMATCH")
    var empty struct{Total int;Items []any}
    if missing.Code!=200||json.Unmarshal(missing.Body.Bytes(),&empty)!=nil||empty.Total!=0||len(empty.Items)!=0 {
        t.Fatalf("search filter not applied: %d %s",missing.Code,missing.Body.String())
    }
    if dashboardFingerprint(f)!=before {t.Fatal("debug GET changed MariaDB")}
}
