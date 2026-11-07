//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"itmap/internal/apifyruns"
)

func TestMariaDBSchedulerApifyOperationsDebugDisposable(t *testing.T) {
	requireSettingsCI(t)
	f:=dashboardFixture(t)
	var baseline int64
	if err:=f.db.QueryRowContext(f.ctx,"SELECT COUNT(*) FROM apify_runs").Scan(&baseline);err!=nil{t.Fatal(err)}
	t.Cleanup(func(){
		ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
		defer cancel()
		for i:=0;i<12;i++{
			if _,err:=f.db.ExecContext(ctx,"DELETE FROM apify_runs WHERE id=?",fmt.Sprintf("CI_APIFY_DEBUG_%02d",i));err!=nil{t.Error("test cleanup failed")}
		}
	})
	for i:=0;i<12;i++{
		id:=fmt.Sprintf("CI_APIFY_DEBUG_%02d",i)
		at:=int64(4102444800000+i*1000)
		if i==11 {at=4102444800000+10*1000}
		generation:=f.generation
		if i==1 {generation="11111111-1111-4111-8111-111111111111"}
		var charged any=0.02
		state:="succeeded"
		if i==11{charged=nil;state="uncertain"}
		f.exec(`INSERT INTO apify_runs
		(id,generation,operation_key,profile,account_id,key_hash,period_start,period_end,
		reserved_usd,charged_usd,state,run_id,dataset_id,error_code,created_at,updated_at)
		VALUES(?,?,?,'free','CI_PRIVATE_ACCOUNT','CI_PRIVATE_KEY','2100-01-01','2100-02-01',
		0.25,?,?,'CI_PRIVATE_RUN','CI_PRIVATE_DATASET','ci_reason',?,?)`,
			id,generation,"ci_op_"+id,charged,state,at,at)
	}
	before:=dashboardFingerprint(f)
	repo:=apifyruns.Repository{DB:f.reader}
	view,err:=repo.ReadOperationsDebug(context.Background())
	if err!=nil{t.Fatal(err)}
	if view.Total!=baseline+12||len(view.Items)!=10||view.Limit!=10||
		view.Coverage!="server_latest"||view.CheckedAt<=0 {
		t.Fatalf("wrong count/coverage: total=%d len=%d",view.Total,len(view.Items))
	}
	if view.Items[0].OperationID!="CI_APIFY_DEBUG_11"||
		view.Items[1].OperationID!="CI_APIFY_DEBUG_10"||
		view.Items[0].ChargedUSD!=nil||
		view.Items[0].ReservedUSD!=0.25||
		view.Items[0].ErrorCode==nil||*view.Items[0].ErrorCode!="ci_reason"{
		t.Fatalf("latest unknown or tie ordering incorrect: %+v",view.Items[:2])
	}
	for _,item:=range view.Items{
		for _,private:=range []string{"ACCOUNT","KEY","RUN","DATASET"}{
			if strings.Contains(item.OperationID,private)||strings.Contains(item.State,private)||
				strings.Contains(item.Profile,private){t.Fatal("sensitive column leaked",private)}
		}
	}
	if dashboardFingerprint(f)!=before{t.Fatal("read-only Apify operation query mutated 72 tables")}
}
