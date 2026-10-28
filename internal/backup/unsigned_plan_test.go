package backup

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"itmap/internal/v39"
)

func TestLegacyCanonicalStableCatalog(t *testing.T) {
	// Canonically equivalent raw spellings retain their input positions. Never
	// deduplicate or rewrite them; company score still takes precedence over name.
	const body = `{"companies":[{"name":"и\ud839\udcec\u0308","tag":"first"},{"name":"и\u0308\ud839\udcec","tag":"second"},{"name":"и\ud839\udcec\u0308","tag":"third"}],"agencies":[{"name":"Я","rank":17},{"name":"А","rank":18}]}`
	got, e := normalizeLegacyCatalog([]byte(body))
	if e != nil {
		t.Fatal(e)
	}
	root, e := orderedJSON(got)
	if e != nil {
		t.Fatal(e)
	}
	original, _ := orderedJSON([]byte(body))
	for i, c := range root.field("companies").items {
		if !reflect.DeepEqual(c.field("name").text, original.field("companies").items[i].field("name").text) || string(rawValue(c.field("tag"))) != string(rawValue(original.field("companies").items[i].field("tag"))) || c.field("rank").number != float64(i+1) {
			t.Fatal("canonical tie rewrote/deduplicated/reordered names")
		}
	}
	for i, a := range root.field("agencies").items {
		if a.field("rank").number != float64(17+i) || !reflect.DeepEqual(a.field("name").text, original.field("agencies").items[i].field("name").text) {
			t.Fatal("agency order/rank changed")
		}
	}
}
func TestLegacyPlanDoesNotEnableRuntime(t *testing.T) {
	for _, version := range []int{14, 15} {
		tables := []Table{}
		for _, d := range v39.Trusted().Tables {
			if tableSince[d.Name] != 0 || d.Name == "app_catalog" || d.Name == "google_calendar_connection" || d.Name == "vacancy_audit_runs" {
				continue
			}
			cols := []json.RawMessage{}
			for _, c := range d.Columns {
				b, _ := json.Marshal(map[string]string{"name": c.Name})
				cols = append(cols, b)
			}
			tables = append(tables, Table{Name: d.Name, Columns: cols, Rows: []map[string]json.RawMessage{}})
		}
		root := map[string]any{"format": Format, "version": version, "catalog": map[string]any{"companies": []any{}, "agencies": []any{}}, "storage": map[string]any{"tables": tables}, "metadata": map[string]any{"sequences": []any{map[string]any{"name": "company_vacancies", "seq": 9000}}, "schema": []string{"CI NOT SQL"}}}
		data, _ := json.Marshal(root)
		before := append([]byte(nil), data...)
		plan, e := PrepareLegacyRestore(data, nil, time.Date(2026, 10, 27, 8, 0, 0, 0, time.UTC))
		if e != nil {
			t.Fatal(e)
		}
		if len(plan.Tables) != 52 || len(plan.Sequences) != 0 || plan.SourceVersion != version || !reflect.DeepEqual(data, before) {
			t.Fatal("legacy plan trusted metadata or changed input")
		}
		if _, e = ValidateModernBackup(data); e == nil {
			t.Fatal("modern validator accepted unsigned legacy")
		}
		if _, e = PrepareV39Restore(data); e == nil {
			t.Fatal("v39 planner accepted legacy")
		}
		if _, e = (Repository{}).ValidateRestore(context.Background(), data); e == nil || ErrorStatus(e) != 400 {
			t.Fatal("runtime validation reached SQL", e)
		}
		if _, e = (Repository{}).Restore(context.Background(), data); e == nil || ErrorStatus(e) != 400 {
			t.Fatal("runtime restore reached SQL", e)
		}
	}
}
