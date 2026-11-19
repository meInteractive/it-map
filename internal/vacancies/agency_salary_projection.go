package vacancies

import (
    "bytes"
    "encoding/json"
    "errors"
    "math"
    "strconv"
    "strings"
)

// Pure read-only extraction of the legacy agency salary rule. This deliberately
// does NOT enable agency writes: update/delete must remain fail-closed until a
// transactional app_catalog patch and disposable MariaDB rollback are proven.
var errAgencySalaryBounds = errors.New("invalid agency salary bounds")

type agencySalaryVacancy struct {
    CompanyName string
    EntityType string
    Level string
    SalaryFrom *int64
    SalaryTo *int64
    Archived bool
}

type agencySalaryLevel struct {
    CompanyName string
    Level string
    Bounds json.RawMessage
    Source string
}

type agencySalaryProjection struct {
    From *int64
    To *int64
    Source string
    Eligible bool
    Changed bool
}

// normalizedAgencyBound follows the D1 projection's strict safe-integer check:
// numeric JSON values and numeric strings are accepted; absent/invalid bounds
// are unknown (NULL), never silently treated as a saved zero.
func normalizedAgencyBound(raw json.RawMessage) (*int64, error) {
    if len(raw)==0 || bytes.Equal(raw, []byte("null")) {
        return nil,nil
    }
    var value any
    dec:=json.NewDecoder(bytes.NewReader(raw))
    dec.UseNumber()
    if err:=dec.Decode(&value); err!=nil { return nil,errAgencySalaryBounds }
    switch x:=value.(type) {
    case json.Number:
        return positiveSafeInteger(string(x))
    case string:
        return positiveSafeInteger(x)
    case bool:
        // SQLite's json_extract returns 1 / 0 for JSON true / false.
        v:=int64(0);if x {v=1};return &v,nil
    default:
        return nil,nil
    }
}
func positiveSafeInteger(raw string) (*int64,error) {
    // Legacy accepts JSON numeric strings, not arbitrary prose.
    n,err:=strconv.ParseFloat(strings.TrimSpace(raw),64)
    if err!=nil || math.IsInf(n,0) || math.IsNaN(n) || n<0 || n>9007199254740991 || math.Trunc(n)!=n {
        return nil,nil
    }
    i:=int64(n)
    return &i,nil
}

func decodeAgencyBounds(raw json.RawMessage) (*int64,*int64,error) {
    if len(raw)==0 || bytes.Equal(raw, []byte("null")) {
        return nil,nil,nil
    }
    var parts []json.RawMessage
    if err:=json.Unmarshal(raw,&parts);err!=nil || len(parts)!=2 {
        return nil,nil,errAgencySalaryBounds
    }
    lo,err:=normalizedAgencyBound(parts[0]);if err!=nil {return nil,nil,err}
    hi,err:=normalizedAgencyBound(parts[1]);if err!=nil {return nil,nil,err}
    return lo,hi,nil
}

func equalAgencyBound(a,b *int64) bool {
    if a==nil || b==nil {return a==nil && b==nil}
    return *a==*b
}

// projectAgencySalaryLevel matches the old agency-salaries.js logical rule on
// one owner+level, including null/min/max, manual levels and provenance.
// Important: the legacy D1 query does NOT filter archived rows. Filtering
// them would alter behavior; the caller must not invent a new rule here.
func projectAgencySalaryLevel(level agencySalaryLevel, rows []agencySalaryVacancy) (agencySalaryProjection,error) {
    oldFrom,oldTo,err:=decodeAgencyBounds(level.Bounds)
    if err!=nil {return agencySalaryProjection{},err}
    result:=agencySalaryProjection{From:oldFrom,To:oldTo,Source:level.Source}
    key:=strings.ToLower(level.Level)
    if key!="middle" && key!="senior" && key!="lead" && key!="head" {
        return agencySalaryProjection{},errAgencySalaryBounds
    }
    if level.Source!="vacancies" && ((oldFrom!=nil && *oldFrom!=0) || (oldTo!=nil && *oldTo!=0)) {
        // A manually specified nonzero level is not owned by vacancy ranges.
        return result,nil
    }
    result.Eligible=true
    result.From,result.To=nil,nil
    for _,row:=range rows {
        if row.CompanyName!=level.CompanyName || row.EntityType!="agency" || strings.ToLower(row.Level)!=key {
            continue
        }
        if row.SalaryFrom!=nil && (result.From==nil || *row.SalaryFrom<*result.From) {
            v:=*row.SalaryFrom;result.From=&v
        }
        if row.SalaryTo!=nil && (result.To==nil || *row.SalaryTo>*result.To) {
            v:=*row.SalaryTo;result.To=&v
        }
    }
    nonzero:=(result.From!=nil && *result.From!=0) || (result.To!=nil && *result.To!=0)
    result.Source="manual"
    if nonzero {result.Source="vacancies"}
    // D1 update condition deliberately doesn't count a provenance-only
    // vacancies→manual reset when both numeric bounds are already NULL.
    result.Changed=!equalAgencyBound(oldFrom,result.From) || !equalAgencyBound(oldTo,result.To) ||
        (nonzero && level.Source!="vacancies")
    return result,nil
}
