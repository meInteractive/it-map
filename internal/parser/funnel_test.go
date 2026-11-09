package parser

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func TestFunnelFilterCalendarAndCounts(t *testing.T) {
	for _, tc := range []struct{source, parser string; valid bool}{
		{"all","all",true},{"hh","apify",true},{"companySite","http",true},
		{"unknown","unknown",true},{"rabota","firecrawl",true},
		{"hh","invalid",false},{"no-source","apify",false},{"","all",false},
	} {
		if FunnelFilterValid(tc.source,tc.parser)!=tc.valid {
			t.Fatalf("unexpected filter validation: %+v",tc)
		}
	}
	for _,tc:=range []struct{raw,want string}{
		{"2026-11-07T20:59:59Z","2026-11-07"},
		{"2026-11-07T21:00:00Z","2026-11-08"},
		{"2026-11-07T23:10:00+05:00","2026-11-07"},
		{"2026-11-07","2026-11-07"},
		{"bad-date",""},
	} {
		if got:=funnelDate(sql.NullString{String:tc.raw,Valid:true});got!=tc.want{
			t.Fatalf("MSK date %s: got %q, want %q",tc.raw,got,tc.want)
		}
	}
	if got:=funnelDate(sql.NullString{});got!="" {t.Fatal("NULL day fabricated")}
	if _,e:=(FunnelRepository{}).ReadFunnel(context.Background(),"wrong","all");!errors.Is(e,ErrFunnelInput){
		t.Fatal("invalid filters should fail before SQL")
	}
	if _,e:=(FunnelRepository{}).ReadFunnel(context.Background(),"all","all");!errors.Is(e,ErrFunnelStorage){
		t.Fatal("missing SQL must fail closed")
	}
	c:=funnelCounts{}
	for i:=range funnelKeys {c.add(i)}
	avg:=c.average(30)
	for _,key:=range funnelKeys {if math.Abs(avg[key]-1.0/30)>1e-12 {t.Fatalf("wrong 30-calendar-day average for %s",key)}}
	raw,_:=json.Marshal(funnelDay{Day:"2026-11-07",funnelCounts:c})
	var v map[string]any
	if json.Unmarshal(raw,&v)!=nil || len(v)!=8 || v["published"]!=float64(1) || v["newVacancies"]!=float64(0) {t.Fatal("daily stage/new vacancy contract changed")}
	newTotals,_:=json.Marshal(funnelTotals{NewVacancies:2,funnelCounts:c})
	var summary map[string]any
	if json.Unmarshal(newTotals,&summary)!=nil || len(summary)!=7 || summary["newVacancies"]!=float64(2) || summary["published"]!=float64(1){
		t.Fatal("new unique publication count must coexist with candidate import count")
	}
	r,_:=json.Marshal(funnelCohort{funnelCounts:c,Rejected:1,Created:1})
	v=map[string]any{} // Unmarshal into an existing map retains unrelated daily fields.
	if json.Unmarshal(r,&v)!=nil || len(v)!=12 || v["rejected"]!=float64(1) {t.Fatal("cohort shape changed")}
}
