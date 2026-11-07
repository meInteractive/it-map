//go:build mariadb && schedulerwrites

package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/auth"
	"itmap/internal/httpapi"
)

// SQL writes below occur only in a disposable CI database; the application
// endpoint itself must make no provider calls and perform no writes.
func TestMariaDBApifyUsageHistoryDisposable(t *testing.T) {
	ctx, db := schedulerDB(t)
	var initial int
	if err := db.QueryRowContext(ctx,"SELECT COUNT(*) FROM apify_runs").Scan(&initial); err!=nil || initial!=0 {
		t.Fatal("Apify history fixture requires an empty ledger",err)
	}
	t.Cleanup(func(){
		if _,err:=db.ExecContext(context.Background(),
			"DELETE FROM apify_runs WHERE account_id='CI_APIFY_HISTORY'");err!=nil {
			t.Error("failed to clean synthetic Apify operations")
		}
	})
	const start = int64(1793534400000)
	const periodStart = "2026-11-01T00:00:00Z"
	const periodEnd = "2026-12-01T00:00:00Z"
	for i:=0;i<14;i++ {
		id:=fmt.Sprintf("%032x",i+1)
		state:="succeeded"
		var charge any=0.1
		runID:="CI_PRIVATE_RUN"
		errCode:=""
		if i==13 {state="uncertain";charge=nil;runID="";errCode="apify_uncertain"}
		if i==12 {charge=0.0}
		generation:="00000000-0000-4000-8000-000000000001"
		if i>=10 {generation="00000000-0000-4000-8000-000000000002"}
		at:=start+int64(i)*1000
		// Equal timestamps must be ordered by BINARY id DESC.
		if i==12 {at=start+11*1000}
		_,err:=db.ExecContext(ctx,`INSERT INTO apify_runs
		  (id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,
		   reserved_usd,charged_usd,state,run_id,dataset_id,error_code,created_at,updated_at)
		  VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		   id,generation,"CI_PRIVATE_KEY_"+id,map[bool]string{true:"starter",false:"free"}[i%2==1],
		   "CI_APIFY_HISTORY","CI_PRIVATE_HASH",periodStart,periodEnd,
		   .25,charge,state,runID,"CI_PRIVATE_DATASET",errCode,at,at+100)
		if err!=nil {t.Fatal("insert synthetic operation",err)}
	}
	legacyID:="CI_PRIVATE_LEGACY_ID"
	_,err:=db.ExecContext(ctx,`INSERT INTO apify_runs
		(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,
		reserved_usd,charged_usd,state,run_id,dataset_id,error_code,created_at,updated_at)
		VALUES(?,?,'CI_PRIVATE_INPUT','free','CI_APIFY_HISTORY','CI_PRIVATE_HASH',?,?,0.25,0,'failed','CI_PRIVATE_RUN','','CI_PRIVATE_REASON',?,?)`,
		legacyID,"00000000-0000-4000-8000-000000000001",periodStart,periodEnd,start+20000,start-20000)
	if err!=nil {t.Fatal("legacy operation fixture",err)}
	reader:=apifyruns.Repository{DB:db}
	view,err:=reader.ReadUsage(ctx)
	if err!=nil {t.Fatal("read-only usage failed",err)}
	if !view.OperationHistoryAvailable || view.OperationsTotal!=15 ||
		len(view.RecentOperations)!=10 || view.OperationsCheckedAt<=0 {
		t.Fatalf("incomplete complete-history window: total=%d recent=%d",
			view.OperationsTotal,len(view.RecentOperations))
	}
	first:=view.RecentOperations[0]
	if !strings.HasPrefix(first.OperationID,"legacy_") || first.ErrorCode!=nil ||
		!first.HasProviderRunID || !first.StartOutcomeKnown || first.ChargedUSD==nil || *first.ChargedUSD!=0 {
		t.Fatalf("legacy operation redaction/confirmed zero incorrect: %+v",first)
	}
	second:=view.RecentOperations[1]
	if second.OperationID!=fmt.Sprintf("%032x",14) || second.ChargedUSD!=nil ||
		second.State!="uncertain" || second.HasProviderRunID || second.StartOutcomeKnown {
		t.Fatalf("latest unknown operation changed: %+v",second)
	}
	// Within the timestamp tied group, the greater native ID sorts first.
	// We never claim the reserve is a confirmed charge.
	var zeroFound, confirmedFound bool
	for _,v:=range view.RecentOperations {
		if v.ChargedUSD!=nil && *v.ChargedUSD==0 {zeroFound=true}
		if v.ChargedUSD!=nil && *v.ChargedUSD>0 {confirmedFound=true}
	}
	if !zeroFound || !confirmedFound {t.Fatal("known zero and confirmed paid rows lost")}
	raw,err:=json.Marshal(view)
	if err!=nil {t.Fatal(err)}
	for _,secret:=range []string{legacyID,"CI_PRIVATE_INPUT","CI_PRIVATE_REASON","CI_PRIVATE_HASH","CI_PRIVATE_DATASET"} {
		if strings.Contains(string(raw),secret){t.Fatal("private Apify data in read view")}
	}
	manager,err:=auth.NewManager(strings.Repeat("x",64),map[string]string{"eugene":"CI_pw","nastya":"CI_pw","demo":"CI_pw"})
	if err!=nil {t.Fatal(err)}
	tokens:=map[string]string{}
	for _,name:=range []string{"eugene","nastya"} {
		user,_:=manager.Authenticate(name,"CI_pw")
		token,err:=manager.Issue(user,time.Now())
		if err!=nil {t.Fatal(err)}
		tokens[name]=token
	}
	handler:=httpapi.NewHandlerWithOptions(nil,httpapi.Options{
		Auth:manager,ApifySettings:apifysettings.NewRepository(db,"CI_LOCAL_SECRET"),
		ApifyUsage:reader,
	})
	call:=func(user,method string)*httptest.ResponseRecorder {
		req:=httptest.NewRequest(method,"https://example.invalid/api/apify/usage",nil)
		if user!="" {req.AddCookie(&http.Cookie{Name:auth.CookieName,Value:tokens[user]})}
		w:=httptest.NewRecorder()
		handler.ServeHTTP(w,req)
		return w
	}
	for _,tc:=range []struct{user,method string;status int}{
		{"","GET",401},{"nastya","GET",403},{"eugene","POST",405},{"eugene","GET",200},
	}{
		w:=call(tc.user,tc.method)
		if w.Code!=tc.status {t.Fatalf("HTTP %s %s: %d, want %d",tc.user,tc.method,w.Code,tc.status)}
		if tc.status!=200 && strings.Contains(w.Body.String(),"recentOperations"){
			t.Fatal("history leaked to unauthorized method/user")
		}
		if tc.status==200 && (!strings.Contains(w.Body.String(),"\"operationsTotal\":15") ||
			strings.Contains(w.Body.String(),legacyID)) {t.Fatal("HTTP history invalid or leaked")}
	}
	var after int
	if err:=db.QueryRowContext(ctx,"SELECT COUNT(*) FROM apify_runs").Scan(&after);err!=nil||after!=15{
		t.Fatal("GET usage mutated paid operation ledger")
	}
}
