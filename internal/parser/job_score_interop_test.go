//go:build legacyinterop

package parser

import (
	"bytes"
	"encoding/json"
	"math"
	"math/rand"
	"os/exec"
	"strconv"
	"testing"
)

func TestSchedulerScoreSource(t *testing.T) {
	// Bind decimal text and CAST inside SQLite: JS Number must not erase BIGINT
	// low bits before the oracle sees aging/priority arithmetic.
	random := rand.New(rand.NewSource(312))
	type pair struct {
		now  int64
		a, b Job
	}
	pairs := []pair{{math.MaxInt64, Job{CreatedAt: math.MaxInt64 - 3599999}, Job{CreatedAt: math.MaxInt64 - 3600000}}}
	for i := 0; i < 1000; i++ {
		a, b := Job{Priority: int64(random.Uint64()), CreatedAt: int64(random.Uint64())}, Job{Priority: int64(random.Uint64()), CreatedAt: int64(random.Uint64())}
		if i%2 == 0 {
			s := "hh"
			a.Type = "scan_source"
			a.SourceID = &s
		}
		pairs = append(pairs, pair{int64(random.Uint64()), a, b})
	}
	input := make([][]string, len(pairs))
	for i, p := range pairs {
		penalty := "0"
		if p.a.SourceID != nil {
			penalty = "50"
		}
		input[i] = []string{strconv.FormatInt(p.a.Priority, 10), strconv.FormatInt(p.now, 10), strconv.FormatInt(p.a.CreatedAt, 10), penalty, strconv.FormatInt(p.b.Priority, 10), strconv.FormatInt(p.now, 10), strconv.FormatInt(p.b.CreatedAt, 10), "0"}
	}
	raw, _ := json.Marshal(input)
	source := `import {DatabaseSync} from 'node:sqlite';import {readFileSync} from 'node:fs';const db=new DatabaseSync(':memory:');const e='CAST(? AS INTEGER)+CAST((CAST(? AS INTEGER)-CAST(? AS INTEGER))/3600000 AS INTEGER)-CAST(? AS INTEGER)';const q=db.prepare('SELECT CASE WHEN a<b THEN -1 WHEN a>b THEN 1 ELSE 0 END c FROM (SELECT '+e+' a,'+e+' b)');process.stdout.write(JSON.stringify(JSON.parse(readFileSync(0,'utf8')).map(v=>q.get(...v).c)));db.close();`
	cmd := exec.Command("node", "--input-type=module", "-e", source)
	cmd.Stdin = bytes.NewReader(raw)
	out, e := cmd.Output()
	if e != nil {
		t.Fatal("SQLite score oracle failed", e)
	}
	var expected []int
	if json.Unmarshal(out, &expected) != nil || len(expected) != len(pairs) {
		t.Fatal("score oracle incomplete")
	}
	for i, p := range pairs {
		if got := scoreCompare(score(p.a, p.now), score(p.b, p.now)); got != expected[i] {
			t.Errorf("SQLite BIGINT score comparison %d: Go %d source %d", i, got, expected[i])
		}
	}
}
