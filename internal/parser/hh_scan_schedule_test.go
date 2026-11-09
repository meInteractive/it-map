package parser

import (
	"database/sql"
	"testing"
	"time"
)

func TestGO290HHScanScheduleAfterIntervalChange(t *testing.T) {
	success := sql.NullString{String:"2026-11-08T10:00:00.000Z", Valid:true}
	oldNext := time.Date(2026,11,15,10,0,0,0,time.UTC).UnixMilli()
	due := time.Date(2026,11,9,10,0,0,0,time.UTC).UnixMilli()
	now := due + int64(time.Minute)
	rows := []HHScanScheduleSource{
		{Enabled:true,Favorite:true,NextRunAt:oldNext,IntervalDays:7,LastSuccessAt:success},
		{Enabled:true,Favorite:true,NextRunAt:oldNext,IntervalDays:7,LastSuccessAt:success,HasActiveScan:true},
		{Enabled:true,Favorite:false,NextRunAt:oldNext,IntervalDays:7,LastSuccessAt:success},
		{Enabled:false,Favorite:true,NextRunAt:0,IntervalDays:7},
		{Enabled:true,Favorite:true,NextRunAt:now+7200000,IntervalDays:1},
		{Enabled:true,Favorite:true,NextRunAt:oldNext,IntervalDays:7},
	}
	s := summarizeHHScanSchedule(rows,now,1,true)
	if s.Configured!=6||s.Disabled!=1||s.OutsideScope!=1||s.ActiveScan!=1||
		s.PotentiallyDue!=1||s.Waiting!=2||s.Recalculated!=1||
		s.UnknownLastSuccess!=1||s.NextDueAt==nil||*s.NextDueAt!=now+7200000 {
		t.Fatalf("wrong interval-change HH schedule: %+v",s)
	}
	unchanged,recalculated,missing:=effectiveHHNextRun(rows[0],7)
	if unchanged!=oldNext||recalculated||missing{t.Fatal("seven-day unchanged cursor moved",unchanged)}
	newNext,updated,missing:=effectiveHHNextRun(rows[0],1)
	if newNext!=due||!updated||missing{t.Fatal("daily change failed",newNext,updated,missing)}
}

func TestGO290HHScanScheduleEmptyAndUnparseableCursor(t *testing.T) {
	now:=time.Date(2026,11,9,10,0,0,0,time.UTC).UnixMilli()
	none:=summarizeHHScanSchedule(nil,now,1,true)
	if none.Configured!=0||none.NextDueAt!=nil{t.Fatal("empty sources misclassified")}
	broken:=HHScanScheduleSource{Enabled:true,Favorite:true,IntervalDays:7,
		NextRunAt:now+1000,LastSuccessAt:sql.NullString{Valid:true,String:"not-a-timestamp"}}
	next,changed,unknown:=effectiveHHNextRun(broken,1)
	if next!=now+1000||changed||!unknown{t.Fatal("invalid saved timestamp should preserve stored deadline")}
	due:=summarizeHHScanSchedule([]HHScanScheduleSource{
		{Enabled:true,Favorite:true,NextRunAt:0,IntervalDays:1},
		{Enabled:true,Favorite:true,NextRunAt:now,IntervalDays:1},
		{Enabled:true,Favorite:true,NextRunAt:now+1,IntervalDays:1},
	},now,1,false)
	if due.PotentiallyDue!=2||due.Waiting!=1||due.NextDueAt==nil||*due.NextDueAt!=now+1{
		t.Fatalf("time boundary wrong: %+v",due)
	}
}
