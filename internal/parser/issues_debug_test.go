package parser

import (
    "encoding/json"
    "strings"
    "testing"
)

func TestIssueP2DebugCoversAllActiveRowsWithoutSecrets(t *testing.T) {
    b := newIssueP2Breakdown()
    for i:=0;i<125;i++ {
        status,reason := "failed","extract_failed"
        if i%5 == 0 { status,reason="pending","retry_due" }
        b.add("CI_PRIVATE_JOB_TOKEN","extract_vacancy","hh",status,reason,int64(200000-i))
    }
    b.finish(125,true)
    if !b.Available || b.TotalActiveP2!=125 || len(b.RecentJobs)!=10 || !b.Truncated || !b.SampleLimited {
        t.Fatalf("incomplete current P2 breakdown: %+v",b)
    }
    if len(b.ByStatus)!=2 || b.ByStatus[0].Value!="failed" || b.ByStatus[0].Count!=100 ||
       b.ByStatus[1].Value!="pending" || b.ByStatus[1].Count!=25 {
        t.Fatalf("bad statuses: %+v",b.ByStatus)
    }
    if len(b.ByReasonCode)!=2 || b.ByReasonCode[0].Count!=100 || b.ByReasonCode[1].Count!=25 {
        t.Fatalf("bad reasons: %+v",b.ByReasonCode)
    }
    if b.RecentJobs[0].UpdatedAt != 200000 || b.RecentJobs[9].UpdatedAt != 199991 {
        t.Fatalf("latest ten must follow SQL sort: %+v",b.RecentJobs)
    }
    encoded,err := json.Marshal(b)
    if err!=nil { t.Fatal(err) }
    if strings.Contains(string(encoded),"CI_PRIVATE_JOB_TOKEN") {
        t.Fatal("raw job ID leaked to diagnostic JSON")
    }
    if !strings.Contains(string(encoded),"\"jobId\":\"job-") {
        t.Fatalf("missing safe correlation ID: %s",encoded)
    }
}

func TestIssueP2DebugUnknownCodesAndReasonGroupLimit(t *testing.T) {
    b:=newIssueP2Breakdown()
    for i:=0;i<50;i++ {
        b.add("private/"+string(rune('a'+i)),"extract_vacancy","https://private.test","failed",
            "reason_"+strings.Repeat("x",i+1),int64(500-i))
    }
    b.finish(50,false)
    if !b.GroupTruncated || len(b.ByReasonCode)!=31 || b.ByReasonCode[30].Value!="remaining" || b.ByReasonCode[30].Count!=20 {
        t.Fatalf("reason group overflow: %+v",b)
    }
    if len(b.BySourceID)!=1 || b.BySourceID[0].Value!="unknown" || b.BySourceID[0].Count!=50 {
        t.Fatalf("source must be sanitized: %+v",b.BySourceID)
    }
    if issueDebugCode("sk-PRIVATE")!="unknown" || issueDebugCode("")!="unknown" {
        t.Fatal("unsafe or empty code accepted")
    }
    empty:=newIssueP2Breakdown()
    empty.finish(0,false)
    if len(empty.ByStatus)!=0 || len(empty.RecentJobs)!=0 || empty.Truncated {
        t.Fatalf("empty sample has fake incidents: %+v",empty)
    }
}
