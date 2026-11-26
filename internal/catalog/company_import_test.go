package catalog

import (
	"encoding/json"
	"testing"
)

func TestCompanyImportPlan(t *testing.T) {
	var input CompanyImport
	if err := json.Unmarshal([]byte(`{"companies":[{"name":"CI Company","companyId":12,"hh":"https://hh.ru/employer/100","rating":4.2,"vpnAiChecked":46000,"middleFrom":100,"middleTo":200},{"name":" ci company "},{"name":"CI invalid","rating":8},{"name":"CI source duplicate","hh":"https://www.hh.ru/employer/100/?a=1"}],"offices":[{"companyName":"CI Company","address":"CI office","lat":55,"lon":37},{"companyName":"CI Company","address":"CI invalid","lat":55}],"reviews":[{"companyId":12,"reviewsPlus":"CI review"}],"agencies":[{"name":"CI Agency","positionLevels":"lead; senior"},{"name":"CI wrong agency","positionLevels":"missing option"}]}`), &input); err != nil {
		t.Fatal(err)
	}
	companies, agencies := newImportOwners(nil, nil), newImportOwners(nil, nil)
	report := importReport{counts: map[string]int{}}
	companies.importEntities(input.Companies, "Компании", "companyId", "create", false, &report)
	companies.importOffices(input.Offices, "create", &report)
	companies.importReviewRows(input.Reviews, "create", &report)
	agencies.importEntities(input.Agencies, "Агентства", "agencyId", "create", true, &report)
	for key, want := range map[string]int{"added": 1, "duplicates": 2, "invalid": 1, "officesAdded": 1, "officesSkipped": 1, "reviewsUpdated": 1, "agenciesAdded": 1, "agenciesSkipped": 1} {
		if got := report.counts[key]; got != want {
			t.Fatalf("%s=%d, expected %d: %+v", key, got, want, report.issues)
		}
	}
	entity := companies.entities[0]
	if obj(entity["reviews"])["plus"] != "CI review" || len(asSlice(entity["offices"])) != 1 {
		t.Fatal("не сохранены связанные листы")
	}
	var updates []ImportRow
	if err := json.Unmarshal([]byte(`[{"name":"CI Company","rating":"0","middleFrom":190,"middleTo":""},{"name":"CI Company","rating":"5"}]`), &updates); err != nil {
		t.Fatal(err)
	}
	companies.importEntities(updates, "Компании", "companyId", "update", false, &report)
	entity = companies.entities[0]
	rangeValue := asSlice(entity["middle"])
	if entity["rating"] != float64(0) || rangeValue[0] != float64(190) || rangeValue[1] != float64(200) || report.counts["updated"] != 1 {
		t.Fatal("обновление затёрло прежнее значение или потеряло нулевую оценку")
	}
}
