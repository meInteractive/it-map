//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"encoding/json"
	"testing"
	"time"

	"itmap/internal/parser"
)

// The fixture uses only the one-time MariaDB CI database; the diagnostic
// connects through the guarded SELECT-only account.
func TestMariaDBHHScanScheduleReadonlyDisposable(t *testing.T) {
	requireSettingsCI(t)
	f:=dashboardFixture(t)
	raw:=`{"enabled":true,"ownerName":"Eugene","revision":1,"sourceIds":["hh"],"intervalDays":1,"vacancyAgeDays":3,"favoritesOnly":false}`
	f.seed(settingsSQLCase{Raw:&raw,Favorite:0,InitialRevision:1})
	f.exec(`INSERT INTO company_identity(id,company_name) VALUES
	 (29001,'CI GO290 Due'),(29002,'CI GO290 Running'),(29003,'CI GO290 Disabled'),(29004,'CI GO290 Waiting')`)
	nextSeven:=time.Date(2026,11,15,9,0,0,0,time.UTC).UnixMilli()
	nextDay:=time.Date(2026,11,10,9,0,0,0,time.UTC).UnixMilli()
	for _,v:=range []struct{id int;enabled,interval int;next int64;last string}{
		{29001,1,7,nextSeven,"2026-11-08T09:00:00.000Z"},
		{29002,1,1,nextDay,"2026-11-09T09:00:00.000Z"},
		{29003,0,1,nextDay,""},
		{29004,1,1,nextDay,""},
	}{
		f.exec(`INSERT INTO vacancy_source_parsing
		(company_id,source_id,source_url,enabled,auto_enabled,interval_days,next_run_at,last_success_at)
		VALUES(?,'hh',?, ?,1,?,?,NULLIF(?,''))`,v.id,"https://example.invalid/ci-go290",v.enabled,v.interval,v.next,v.last)
	}
	f.exec(`INSERT INTO scheduler_jobs
	 (id,generation,dedupe_key,type,company_id,source_id,payload,status,priority,run_after,created_at,updated_at)
	 VALUES('ci-go290-active',?,'ci-go290-active-key','scan_source',29002,'hh','{}','pending',20,1,1,1)`,f.generation)
	before:=dashboardFingerprint(f)
	repo:=parser.HHScanScheduleRepository{DB:f.reader,Now:func()time.Time{
		return time.Date(2026,11,9,11,0,0,0,time.UTC)
	}}
	body,err:=repo.ReadHHScanSchedule(f.ctx)
	if err!=nil{t.Fatal(err)}
	var got parser.HHScanScheduleReport
	if e:=json.Unmarshal(body,&got);e!=nil{t.Fatal(e)}
	if !got.Enabled||!got.SourceSelected||got.IntervalDays!=1||got.VacancyAgeDays!=3||
		got.Summary.Configured!=4||got.Summary.Disabled!=1||got.Summary.ActiveScan!=1||
		got.Summary.Waiting!=1||got.Summary.PotentiallyDue!=1||got.Summary.Recalculated!=1||
		got.PendingHHScans!=1||got.RunningHHScans!=0||got.FinalEligibilityVerified||
		got.Summary.NextDueAt==nil||*got.Summary.NextDueAt!=nextDay {
			t.Fatalf("unexpected HH source diagnostic: %+v",got)
	}
	if gotAfter:=dashboardFingerprint(f);gotAfter!=before{
		t.Fatal("SELECT-only diagnostic mutated disposable MariaDB")
	}
}
