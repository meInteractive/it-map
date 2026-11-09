package parser

import (
	"testing"
	"time"
)

func TestUnitCostFilterAndMoscowCalendar(t *testing.T){
	for _,n:=range []int{-1,0,1,8,31,365}{
		if UnitCostFilterValid(n,"all"){t.Fatalf("accepted period %d",n)}
	}
	for _,source:=range []string{"","invalid","hh; DROP TABLE"," HH ","\x00"}{
		if UnitCostFilterValid(7,source){t.Fatalf("accepted source %q",source)}
	}
	for _,pair:=range []struct{days int;source string}{
		{7,"all"},{30,"all"},{7,"hh"},{7,"companySite"},{30,"unknown"},
	}{
		if !UnitCostFilterValid(pair.days,pair.source){t.Fatalf("rejected %v",pair)}
	}
	// UTC 20:59:59 belongs to previous Moscow day; UTC 21:00 is the
	// next local midnight and must be in exactly one period/day.
	now:=time.Date(2026,11,10,10,0,0,0,time.UTC)
	w,days:=unitCostWindowFor(now,7)
	if len(days)!=7||days[0].Day!="2026-11-04"||days[6].Day!="2026-11-10"{
		t.Fatalf("wrong 7-day Moscow calendar: %+v",days)
	}
	before:=time.Date(2026,11,9,20,59,59,0,time.UTC)
	at:=time.Date(2026,11,9,21,0,0,0,time.UTC)
	if before.In(moscow).Format("2006-01-02")!="2026-11-09" ||
		at.In(moscow).Format("2006-01-02")!="2026-11-10"{
		t.Fatal("Moscow midnight boundary changed")
	}
	if !(w.start<before.UnixMilli()&&w.end>at.UnixMilli()){
		t.Fatal("calendar excludes valid last-day events")
	}
	w30,calendar30:=unitCostWindowFor(now,30)
	if len(calendar30)!=30||calendar30[0].Day!="2026-10-12" ||
		w30.start>=w.start||w30.end!=w.end {t.Fatal("wrong 30-day calendar")}
}
