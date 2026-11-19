package vacancies

import (
    "encoding/json"
    "testing"
)

func agencyPtr(n int64) *int64 { return &n }

func expectAgencyBound(t *testing.T,label string,got,want *int64) {
    t.Helper()
    if !equalAgencyBound(got,want) {t.Fatalf("%s: got %v, want %v",label,got,want)}
}

func TestAgencySalaryProjectionLegacyCandidateContract(t *testing.T) {
    rows:=[]agencySalaryVacancy{
        {CompanyName:"Agency A",EntityType:"agency",Level:"Senior",SalaryFrom:agencyPtr(200_000),SalaryTo:agencyPtr(300_000)},
        {CompanyName:"Agency A",EntityType:"agency",Level:"Senior",SalaryFrom:agencyPtr(250_000),SalaryTo:agencyPtr(450_000)},
        {CompanyName:"Agency A",EntityType:"agency",Level:"Head",SalaryTo:agencyPtr(800_000)},
        {CompanyName:"Agency A",EntityType:"agency",Level:"Lead",SalaryFrom:agencyPtr(300_000),SalaryTo:agencyPtr(700_000)},
        {CompanyName:"Agency A",EntityType:"agency",Level:"Middle"}, // no salary
        {CompanyName:"Agency B",EntityType:"agency",Level:"Senior",SalaryFrom:agencyPtr(100_000),SalaryTo:agencyPtr(900_000)},
        {CompanyName:"Agency A",EntityType:"company",Level:"Senior",SalaryFrom:agencyPtr(1),SalaryTo:agencyPtr(9_000_000)}, // wrong entity_type
    }
    tests:=[]struct {
        name string
        level agencySalaryLevel
        from,to *int64
        source string
        eligible,changed bool
    }{
        {"two active vacancies",agencySalaryLevel{"Agency A","senior",json.RawMessage("[null,null]"),"manual"},agencyPtr(200_000),agencyPtr(450_000),"vacancies",true,true},
        {"preserve manual range",agencySalaryLevel{"Agency A","senior",json.RawMessage("[150000,600000]"),"manual"},agencyPtr(150_000),agencyPtr(600_000),"manual",false,false},
        {"known vacancy range unchanged",agencySalaryLevel{"Agency A","senior",json.RawMessage("[200000,450000]"),"vacancies"},agencyPtr(200_000),agencyPtr(450_000),"vacancies",true,false},
        {"numeric string bounds",agencySalaryLevel{"Agency A","senior",json.RawMessage("[\"200000\",\"450000\"]"),"vacancies"},agencyPtr(200_000),agencyPtr(450_000),"vacancies",true,false},
        {"one sided salary",agencySalaryLevel{"Agency A","head",json.RawMessage("[null,null]"),"manual"},nil,agencyPtr(800_000),"vacancies",true,true},
        {"salary-less vacancy remains unknown",agencySalaryLevel{"Agency A","middle",json.RawMessage("[null,null]"),"manual"},nil,nil,"manual",true,false},
        {"empty derived range",agencySalaryLevel{"Agency A","middle",json.RawMessage("[100000,200000]"),"vacancies"},nil,nil,"manual",true,true},
        {"owner isolation",agencySalaryLevel{"Agency B","senior",json.RawMessage("[null,null]"),"manual"},agencyPtr(100_000),agencyPtr(900_000),"vacancies",true,true},
        {"unrelated agency",agencySalaryLevel{"Agency C","senior",json.RawMessage("[0,0]"),"manual"},nil,nil,"manual",true,true},
    }
    for _,tc:=range tests {
        t.Run(tc.name,func(t *testing.T) {
            got,err:=projectAgencySalaryLevel(tc.level,rows)
            if err!=nil {t.Fatal(err)}
            expectAgencyBound(t,"from",got.From,tc.from)
            expectAgencyBound(t,"to",got.To,tc.to)
            if got.Source!=tc.source || got.Eligible!=tc.eligible || got.Changed!=tc.changed {
                t.Fatalf("projection metadata got %+v, want source=%q eligible=%v changed=%v",got,tc.source,tc.eligible,tc.changed)
            }
        })
    }
}

func TestAgencySalaryProjectionArchivedLegacyBehavior(t *testing.T) {
    // Old source: server/repositories/agency-salaries.js ranges query has
    // no archived predicate. S11-B03-27 says exclude IF the source did.
    // It didn't, so preserving source parity means archived contributes.
    rows:=[]agencySalaryVacancy{
        {CompanyName:"Agency",EntityType:"agency",Level:"Senior",SalaryFrom:agencyPtr(250_000),SalaryTo:agencyPtr(450_000),Archived:true},
        {CompanyName:"Agency",EntityType:"agency",Level:"Senior",SalaryFrom:agencyPtr(200_000),SalaryTo:agencyPtr(300_000),Archived:false},
    }
    got,err:=projectAgencySalaryLevel(agencySalaryLevel{"Agency","senior",json.RawMessage("[null,null]"),"manual"},rows)
    if err!=nil {t.Fatal(err)}
    expectAgencyBound(t,"lower",got.From,agencyPtr(200_000))
    expectAgencyBound(t,"upper (legacy includes archived)",got.To,agencyPtr(450_000))
}

func TestAgencySalaryProjectionBoundsFailClosed(t *testing.T) {
    cases:=[]string{"[100000]","[100000,200000,300000]","{\"foo\":1}","invalid json"}
    for _,raw:=range cases {
        _,err:=projectAgencySalaryLevel(agencySalaryLevel{"A","Senior",json.RawMessage(raw),"vacancies"},nil)
        if err==nil {t.Fatalf("malformed agency bounds unexpectedly accepted: %s",raw)}
    }
    got,err:=projectAgencySalaryLevel(agencySalaryLevel{"A","Senior",json.RawMessage("[\"not-a-number\",9007199254740992]"),"vacancies"},nil)
    if err!=nil {t.Fatal(err)}
    if !got.Eligible || got.Changed || got.From!=nil || got.To!=nil {
        t.Fatalf("unsafe bound must be unknown, got %+v",got)
    }
}

func TestAgencySalaryProjectionNoSQLOrProviderSideEffects(t *testing.T) {
    // Pure projection never opens DB, modifies app_catalog or starts providers.
    // Staging acceptance must independently prove atomic SQL create/update/
    // archive/delete + catalog in one disposable transaction before enabling
    // any currently guarded agency mutation.
    rows:=[]agencySalaryVacancy{{CompanyName:"A",EntityType:"agency",Level:"Middle",SalaryFrom:agencyPtr(0),SalaryTo:agencyPtr(0)}}
    got,err:=projectAgencySalaryLevel(agencySalaryLevel{"A","Middle",json.RawMessage("[null,null]"),"manual"},rows)
    if err!=nil {t.Fatal(err)}
    expectAgencyBound(t,"zero lower",got.From,agencyPtr(0))
    expectAgencyBound(t,"zero upper",got.To,agencyPtr(0))
    if got.Source!="manual" || !got.Changed {t.Fatalf("expected a zero range without fabricated vacancy provenance: %+v",got)}
}
