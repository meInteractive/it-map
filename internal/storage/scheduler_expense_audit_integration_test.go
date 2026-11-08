//go:build mariadb && schedulerwrites && schedulersettings && schedulerdashboard

package storage

import (
	"context"
	"database/sql"
	"math"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// The reviewed GO-434 queries must remain SELECT-only. This guard executes
// against disposable MariaDB, never a production connection or provider API.
func expenseAuditSQLStatements(t *testing.T) []string {
	t.Helper()
	raw,err:=os.ReadFile("../../docs/dev/GO-434-EXPENSE-AUDIT-QUERIES.sql")
	if err!=nil{t.Fatal(err)}
	unsafe:=regexp.MustCompile(`(?i)\b(INSERT|UPDATE|DELETE|DROP|ALTER|CREATE|REPLACE|TRUNCATE|CALL|LOAD|HANDLER|OUTFILE|DUMPFILE)\b`)
	parts:=strings.Split(string(raw),";")
	queries:=[]string{}
	for _,part:=range parts{
		lines:=[]string{}
		for _,line:=range strings.Split(part,"\n"){
			if strings.HasPrefix(strings.TrimSpace(line),"--"){continue}
			lines=append(lines,line)
		}
		q:=strings.TrimSpace(strings.Join(lines,"\n"))
		if q==""{continue}
		if !strings.HasPrefix(strings.ToUpper(q),"SELECT ")||unsafe.MatchString(q){
			t.Fatalf("GO-434 audit query is not strictly SELECT-only")
		}
		queries=append(queries,q)
	}
	if len(queries)!=11{t.Fatalf("expected 11 reviewed read-only queries, got %d",len(queries))}
	return queries
}

func TestMariaDBSchedulerAISpendingGO434ReadOnlyDisposable(t *testing.T) {
	requireSettingsCI(t)
	f:=openSettingsSQL(t)
	queries:=expenseAuditSQLStatements(t)
	before:=dashboardFingerprint(f)
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	tx,err:=f.reader.BeginTx(ctx,&sql.TxOptions{ReadOnly:true,Isolation:sql.LevelRepeatableRead})
	if err!=nil{t.Fatal(err)}
	defer tx.Rollback()
	// Execute every query against the actual imported empty-schema fixture.
	// Never print financial/account/candidate rows; discard row values.
	for i,q:=range queries{
		rows,err:=tx.QueryContext(ctx,q)
		if err!=nil{t.Fatalf("GO-434 SQL %d is not valid against baseline schema: %v",i+1,err)}
		names,err:=rows.Columns()
		if err!=nil{rows.Close();t.Fatal(err)}
		values:=make([]any,len(names))
		targets:=make([]any,len(names))
		for n:=range values{targets[n]=&values[n]}
		for rows.Next(){
			if err=rows.Scan(targets...);err!=nil{break}
		}
		if err==nil{err=rows.Err()}
		rows.Close()
		if err!=nil{t.Fatalf("GO-434 SQL %d could not be read: %v",i+1,err)}
	}
	// Inline synthetic SQL fixture: Actor scanned twice across sources, a
	// separate empty paid scan; the provider bill stays 0.50, not 0.90.
	var billed float64
	err=tx.QueryRowContext(ctx,`SELECT SUM(charged) FROM (
		SELECT account_scope,actor_id,MAX(cost_usd) charged FROM (
			SELECT 'ci' account_scope,'actor-a' actor_id,0.40 cost_usd
			UNION ALL SELECT 'ci','actor-a',0.40
			UNION ALL SELECT 'ci','actor-b',0.10
		) receipts GROUP BY account_scope,actor_id
	) provider_operations`).Scan(&billed)
	if err!=nil||math.Abs(billed-0.50)>1e-10{t.Fatalf("duplicate Actor was billed twice: %v",err)}
	// One newly created main vacancy across two sources + update + late
	// existing publication. Candidate or receipt counts are not the KPI.
	var created int
	err=tx.QueryRowContext(ctx,`SELECT COUNT(DISTINCT CASE WHEN status='success' AND action='create' THEN vacancy_id END) FROM (
		SELECT 101 vacancy_id,'create' action,'success' status
		UNION ALL SELECT 101,'unchanged','success'
		UNION ALL SELECT 101,'update','success'
		UNION ALL SELECT 102,'update','success'
		UNION ALL SELECT 103,'create','error'
	) synthetic_imports`).Scan(&created)
	if err!=nil||created!=1{t.Fatalf("nonunique or failed imports inflated publications: %v %d",err,created)}
	if err=tx.Commit();err!=nil{t.Fatal(err)}
	if dashboardFingerprint(f)!=before{t.Fatal("read-only GO-434 queries modified the disposable database")}
}
