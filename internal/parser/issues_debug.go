package parser

import (
    "crypto/sha256"
    "encoding/hex"
    "sort"
    "strings"
)

const issueDebugJobsLimit = 10
const issueDebugGroupsLimit = 30

// issueP2Breakdown отражает все актуальные проблемные задания разрешённой области,
// а не первые 120 строк, по которым формируется краткий список проблем в UI.
type issueP2Breakdown struct {
    Available bool `json:"available"`
    TotalActiveP2 int64 `json:"totalActiveP2"`
    SampleLimited bool `json:"sampleLimited"`
    Limit int `json:"limit"`
    Truncated bool `json:"truncated"`
    GroupTruncated bool `json:"groupTruncated"`
    ByStatus []issueDebugCount `json:"byStatus"`
    ByType []issueDebugCount `json:"byType"`
    BySourceID []issueDebugCount `json:"bySourceId"`
    ByReasonCode []issueDebugCount `json:"byReasonCode"`
    RecentJobs []issueDebugJob `json:"recentJobs"`
    countsStatus map[string]int64
    countsType map[string]int64
    countsSource map[string]int64
    countsReason map[string]int64
}

type issueDebugCount struct {
    Value string `json:"value"`
    Count int64 `json:"count"`
}

type issueDebugJob struct {
    JobID string `json:"jobId"`
    Status string `json:"status"`
    Type string `json:"type"`
    SourceID string `json:"sourceId"`
    ReasonCode string `json:"reasonCode"`
    UpdatedAt int64 `json:"updatedAt"`
}

func newIssueP2Breakdown() *issueP2Breakdown {
    return &issueP2Breakdown{
        Available:true, Limit:issueDebugJobsLimit,
        ByStatus:[]issueDebugCount{}, ByType:[]issueDebugCount{},
        BySourceID:[]issueDebugCount{}, ByReasonCode:[]issueDebugCount{},
        RecentJobs:[]issueDebugJob{},
        countsStatus:map[string]int64{}, countsType:map[string]int64{},
        countsSource:map[string]int64{}, countsReason:map[string]int64{},
    }
}

// Безопасный короткий код; нестандартные значения из старых записей не
// становятся произвольным содержимым копируемого JSON.
func issueDebugCode(raw string) string {
    if raw == "" || len(raw) > 64 {
        return "unknown"
    }
    for _, r := range raw {
        if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
            return "unknown"
        }
    }
    return raw
}

func (b *issueP2Breakdown) add(id, kind, source, status, rawReason string, updatedAt int64) {
    if b == nil { return }
    kind, source, status = issueDebugCode(kind),issueDebugCode(source),issueDebugCode(status)
    reason := issueDebugCode(rawReason)
    b.countsStatus[status]++
    b.countsType[kind]++
    b.countsSource[source]++
    b.countsReason[reason]++
    // SQL уже сортирует записи по updated_at DESC, BINARY id DESC.
    if len(b.RecentJobs) < issueDebugJobsLimit {
        digest := sha256.Sum256([]byte(id))
        b.RecentJobs = append(b.RecentJobs, issueDebugJob{
            JobID:"job-"+hex.EncodeToString(digest[:8]), Status:status,
            Type:kind,SourceID:source,ReasonCode:reason,UpdatedAt:updatedAt,
        })
    }
}

// В больших журналах 30 наиболее частых причин + агрегат остальных
// позволяют сохранить точную сумму, не создавая бесконтрольный JSON.
func issueDebugBuckets(counts map[string]int64) ([]issueDebugCount,bool) {
    values := make([]issueDebugCount,0,len(counts))
    for value,n := range counts { values=append(values,issueDebugCount{Value:value,Count:n}) }
    sort.Slice(values,func(i,j int) bool {
        if values[i].Count != values[j].Count { return values[i].Count > values[j].Count }
        return strings.Compare(values[i].Value,values[j].Value) < 0
    })
    if len(values) <= issueDebugGroupsLimit { return values,false }
    var rest int64
    for _,entry := range values[issueDebugGroupsLimit:] { rest += entry.Count }
    values=append(values[:issueDebugGroupsLimit],issueDebugCount{Value:"remaining",Count:rest})
    return values,true
}

func (b *issueP2Breakdown) finish(total int64, sampleLimited bool) {
    if b == nil { return }
    b.TotalActiveP2=total
    b.SampleLimited=sampleLimited
    b.Truncated=total>int64(len(b.RecentJobs))
    var cut bool
    b.ByStatus,cut=issueDebugBuckets(b.countsStatus); b.GroupTruncated=b.GroupTruncated||cut
    b.ByType,cut=issueDebugBuckets(b.countsType); b.GroupTruncated=b.GroupTruncated||cut
    b.BySourceID,cut=issueDebugBuckets(b.countsSource); b.GroupTruncated=b.GroupTruncated||cut
    b.ByReasonCode,cut=issueDebugBuckets(b.countsReason); b.GroupTruncated=b.GroupTruncated||cut
}
