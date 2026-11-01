package apifyruns

import (
	"bytes"
	"encoding/json"
	"itmap/internal/integrations/apify"
	"os/exec"
	"testing"
	"time"
)

func TestRunSettlementPreservesUnknownAndMonotonicCharge(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	known := 0.12
	for _, tc := range []struct {
		name, status, finished, cost, wantState string
		old                                     *float64
		want                                    *float64
	}{
		{"running cost is provisional", "RUNNING", "", "0.1", "running", nil, nil},
		{"fresh terminal cost is provisional", "SUCCEEDED", "2026-11-01T11:59:55Z", "0.1", "succeeded", nil, nil},
		{"settled terminal cost", "SUCCEEDED", "2026-11-01T11:59:50Z", "0.12", "succeeded", nil, &known},
		{"late missing charge", "SUCCEEDED", "", "null", "succeeded", &known, &known},
		{"late smaller charge", "SUCCEEDED", "", "0.01", "succeeded", &known, &known},
		{"failed is not free", "FAILED", "", "null", "failed", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(`{"id":"Run123","defaultDatasetId":"Data123","status":"` + tc.status + `","userId":"CIAccount","finishedAt":"` + tc.finished + `","usageTotalUsd":` + tc.cost + `}`)
			got, e := mergeRun(Record{AccountID: "CIAccount", State: "uncertain", ChargedUSD: tc.old}, apify.ActorRun{ID: "Run123", DefaultDatasetID: "Data123", Status: tc.status, Raw: raw}, now)
			if e != nil || got.State != tc.wantState || (got.ChargedUSD == nil) != (tc.want == nil) || got.ChargedUSD != nil && *got.ChargedUSD != *tc.want {
				t.Fatalf("settlement %+v, error %v", got, e)
			}
		})
	}
}
func TestRunSettlementRejectsIdentityChangesAndTerminalRegression(t *testing.T) {
	now := time.Now()
	prior := Record{AccountID: "CIAccount", State: "succeeded", RunID: "Run123", DatasetID: "Data123"}
	for _, raw := range []string{
		`{"id":"Other","defaultDatasetId":"Data123","status":"RUNNING"}`,
		`{"id":"Run123","defaultDatasetId":"Other","status":"RUNNING"}`,
		`{"id":"Run123","defaultDatasetId":"Data123","status":"RUNNING","userId":"Other"}`,
	} {
		var v struct{ ID, DefaultDatasetID, Status string }
		json.Unmarshal([]byte(raw), &v)
		if _, e := mergeRun(prior, apify.ActorRun{ID: v.ID, DefaultDatasetID: v.DefaultDatasetID, Status: v.Status, Raw: json.RawMessage(raw)}, now); e == nil {
			t.Fatal("identity substitution accepted")
		}
	}
	got, e := mergeRun(prior, apify.ActorRun{ID: "Run123", DefaultDatasetID: "Data123", Status: "RUNNING", Raw: json.RawMessage(`{"id":"Run123","defaultDatasetId":"Data123","status":"RUNNING"}`)}, now)
	if e != nil || got.State != "succeeded" {
		t.Fatal("terminal state regressed", e)
	}
}

func TestRunSettlementMatchesUnchangedJavaScript(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	type sample struct {
		Row map[string]any `json:"row"`
		Run map[string]any `json:"run"`
	}
	cases := []sample{}
	for _, status := range []string{"RUNNING", "SUCCEEDED", "FAILED"} {
		for _, finished := range []string{"", "2026-11-01T11:59:55Z", "2026-11-01T11:59:50Z"} {
			for _, cost := range []any{nil, 0, 0.02, 0.2, "invalid"} {
				cases = append(cases, sample{map[string]any{"id": "CIrow", "state": "succeeded", "run_id": "CIRun", "dataset_id": "CIData", "account_id": "CIAccount", "charged_usd": 0.1}, map[string]any{"id": "CIRun", "defaultDatasetId": "CIData", "userId": "CIAccount", "status": status, "finishedAt": finished, "usageTotalUsd": cost}})
			}
		}
	}
	script := `import {DatabaseSync} from 'node:sqlite';import {recordApifyRun} from './server/services/apify-runs.js';let raw='';for await(const c of process.stdin)raw+=c;Date.now=()=>1793534400000;let output=[];for(const x of JSON.parse(raw)){const sql=new DatabaseSync(':memory:');sql.exec('CREATE TABLE apify_runs(id TEXT,state TEXT,run_id TEXT,dataset_id TEXT,account_id TEXT,charged_usd REAL,error_code TEXT,updated_at INTEGER)');sql.prepare('INSERT INTO apify_runs VALUES(?,?,?,?,?,?,?,?)').run(x.row.id,x.row.state,x.row.run_id,x.row.dataset_id,x.row.account_id,x.row.charged_usd,'',0);const db={prepare(q){const stmt=sql.prepare(q);return {bind(...args){return {async run(){return {meta:{changes:stmt.run(...args).changes}}},async first(){return stmt.get(...args)}}}}}};const r=await recordApifyRun(db,x.row,x.run);output.push({state:r.state,charged:r.charged_usd});sql.close()}console.log(JSON.stringify(output));`
	input, _ := json.Marshal(cases)
	cmd := exec.Command("node", "--input-type=module", "-e", script)
	cmd.Dir = "../.."
	cmd.Stdin = bytes.NewReader(input)
	raw, e := cmd.Output()
	if e != nil {
		t.Fatal("source oracle", e)
	}
	var expected []struct {
		State   string   `json:"state"`
		Charged *float64 `json:"charged"`
	}
	if json.Unmarshal(raw, &expected) != nil || len(expected) != len(cases) {
		t.Fatal("invalid source oracle")
	}
	for i, c := range cases {
		raw, _ := json.Marshal(c.Run)
		var v struct{ ID, DefaultDatasetID, Status string }
		json.Unmarshal(raw, &v)
		old := 0.1
		got, e := mergeRun(Record{State: "succeeded", RunID: "CIRun", DatasetID: "CIData", AccountID: "CIAccount", ChargedUSD: &old}, apify.ActorRun{ID: v.ID, DefaultDatasetID: v.DefaultDatasetID, Status: v.Status, Raw: raw}, now)
		want := expected[i]
		if e != nil || got.State != want.State || got.ChargedUSD == nil || want.Charged == nil || *got.ChargedUSD != *want.Charged {
			t.Fatalf("source case %d: %+v != %+v (%v)", i, got, want, e)
		}
	}
}
