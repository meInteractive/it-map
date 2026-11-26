//go:build mariadb && catalogreads

package storage

import (
	"encoding/json"
	"strings"
	"testing"

	"itmap/internal/catalog"
)

func TestMariaDBCatalogImportDisposable(t *testing.T) {
	f := openCatalogSQL(t)
	if _, err := f.writer.ExecContext(f.ctx, `INSERT INTO app_catalog(id,data_json) VALUES(1,'{"companies":[],"agencies":[]}')`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := f.writer.ExecContext(f.ctx, "DELETE FROM hiring_contacts WHERE company_name IN ('CI New Company','CI New Agency')"); err != nil {
			t.Error(err)
		}
	})
	run := func(body string) catalog.CompanyImportResult {
		t.Helper()
		var input catalog.CompanyImport
		if err := json.Unmarshal([]byte(body), &input); err != nil {
			t.Fatal(err)
		}
		out, err := (catalog.Repository{DB: f.writer}).ImportCompanies(f.ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	create := `{"mode":"create","companies":[{"name":"CI New Company","companyId":20,"rating":4.2,"sector":"CI retained","middleFrom":100,"middleTo":200}],"offices":[{"companyName":"CI New Company","address":"CI office","lat":55,"lon":37}],"reviews":[{"companyId":20,"reviewsPlus":"CI review"}],"agencies":[{"name":"CI New Agency","agencyId":21}],"contacts":[{"entityType":"Компания","entityId":20,"entityName":"CI New Company","position":1,"fullName":"CI Contact"},{"entityType":"Агентство","entityId":21,"entityName":"CI New Agency","position":1,"fullName":"CI Agency Contact"}]}`
	got := run(create)
	for key, value := range map[string]int{"added": 1, "officesAdded": 1, "reviewsUpdated": 1, "agenciesAdded": 1, "contactsAdded": 2, "invalid": 0} {
		if got[key] != value {
			t.Fatalf("%s: got %v want %d, report %v", key, got[key], value, got)
		}
	}
	duplicate := run(`{"mode":"create","companies":[{"name":" ci new company "}],"contacts":[{"entityType":"company","entityName":"CI New Company","position":1,"fullName":"CI Contact"}]}`)
	if duplicate["duplicates"] != 1 || duplicate["contactsSkipped"] != 1 {
		t.Fatalf("неверный повтор: %v", duplicate)
	}
	update := run(`{"mode":"update","companies":[{"name":"CI New Company","rating":0,"middleFrom":150},{"name":"CI New Company","rating":9}],"offices":[{"companyName":"CI New Company","address":"CI office","lat":56,"lon":38}],"contacts":[{"entityType":"company","entityName":"CI New Company","position":1,"specialization":"CI updated"}]}`)
	if update["updated"] != 1 || update["invalid"] != 1 || update["officesUpdated"] != 1 || update["contactsUpdated"] != 1 {
		t.Fatalf("неверный отчёт: %v", update)
	}
	var data string
	if err := f.reader.QueryRowContext(f.ctx, "SELECT data_json FROM app_catalog WHERE id=1").Scan(&data); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"sector":"CI retained"`, `"rating":0`, `"middle":[150,200]`, `"lat":56`, `"plus":"CI review"`} {
		if !strings.Contains(data, fragment) {
			t.Fatalf("каталог потерял %q", fragment)
		}
	}
	var name, specialization string
	if err := f.reader.QueryRowContext(f.ctx, "SELECT full_name,specialization FROM hiring_contacts WHERE company_name='CI New Company' AND position=1").Scan(&name, &specialization); err != nil || name != "CI Contact" || specialization != "CI updated" {
		t.Fatal("контакт не обновлён с сохранением ФИО", err)
	}
}
