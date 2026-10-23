package catalog

import (
	"encoding/json"
	"strings"
	"testing"
)

const compactFixture = `{"companies":[{"name":"Компания А","rank":1,"entityType":"company","sector":"IT","middle":[100,200],"offices":[{"address":"Москва","metro":"Китай-город","lat":55.5,"lon":37.5}],"jobs":[{"title":"Backend","url":"https://example.test/jobs/1"}],"vacancySources":{"hh":"https://hh.ru/1"},"reviews":{"plus":"Приватная подробность"}},{"name":"Компания Б","rank":2,"offices":[],"jobs":[]}],"agencies":[{"name":"Агентство Т","rank":3,"jobs":[],"offices":[],"functionalSpecializations":["ИТ"],"reviewsScore":70.5,"revenue":{"amount":2000}}]}`

func testProjectionCatalog(t *testing.T) sourceCatalog {
	t.Helper()
	c, e := parseCatalog([]byte(compactFixture))
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestCompactListProjection(t *testing.T) {
	c := testProjectionCatalog(t)
	s := catalogState{Identities: map[string]entityState{entityKey(CompanyType, "Компания А"): {ID: 11, Favorite: true, Hidden: false}, entityKey(AgencyType, "Агентство Т"): {ID: 25, Hidden: true}}, Vacancies: map[string]bool{"Компания Б": true}, Applications: map[string]applicationState{"Компания Б": {Status: "Отказ"}}, Initialized: map[string]bool{}}
	got := projectList(c, CompanyType, ListView, s)
	if len(got) != 2 || got[0]["id"] != int64(11) || got[0]["favorite"] != true || got[0]["hidden"] != false {
		t.Fatalf("wrong personal state: %+v", got)
	}
	if got[0]["hasVacancies"] != true || got[1]["hasVacancies"] != true || got[1]["applicationStatus"] != "Отказ" {
		t.Fatalf("wrong vacancy/application flags: %+v", got)
	}
	if got[0]["hasVacancySource"] != true {
		t.Fatal("vacancy-source flag missing")
	}
	if got[0]["id"] == nil || got[1]["id"] != nil {
		t.Fatalf("bad/missing IDs: %+v", got)
	}
	// Full private company details must NEVER be embedded into the compact list.
	b, _ := json.Marshal(got)
	if string(b) == "" || strings.Contains(string(b), "Приватная подробность") || strings.Contains(string(b), "example.test/jobs") || strings.Contains(string(b), "https://hh.ru/") {
		t.Fatal("private details leaked into list")
	}
	// Initial catalog jobs no longer count once the identity was initialized.
	s.Initialized["Компания А"] = true
	if projectList(c, CompanyType, ListView, s)[0]["hasVacancies"] != false {
		t.Fatal("initialized catalog jobs counted twice")
	}
}
func TestCompactMapAndAgencyProjection(t *testing.T) {
	c := testProjectionCatalog(t)
	s := catalogState{Identities: map[string]entityState{}, Vacancies: map[string]bool{}, Applications: map[string]applicationState{}, Initialized: map[string]bool{}}
	mapped := projectList(c, CompanyType, MapView, s)
	o := mapped[0]["offices"].([]map[string]any)[0]
	if o["lat"] != 55.5 || o["lon"] != 37.5 {
		t.Fatalf("map lost coords: %+v", o)
	}
	if _, found := mapped[0]["rating"]; found {
		t.Fatal("map leaks list-only fields")
	}
	if _, found := mapped[0]["middle"]; found {
		t.Fatal("map includes salaries")
	}
	a := projectList(c, AgencyType, ListView, s)[0]
	if a["reviewsScore"] != 70.5 || a["revenue"].(map[string]any)["amount"] != float64(2000) {
		t.Fatalf("agency projection %+v", a)
	}
	if a["entityType"] != "agency" {
		t.Fatalf("bad entity type %+v", a)
	}
}
func TestDetailProjectionRemovesEmbeddedJobs(t *testing.T) {
	c := testProjectionCatalog(t)
	d := projectDetail(c.Companies[0], CompanyType, int64(11), true, false, 4)
	if _, exists := d["jobs"]; exists {
		t.Fatal("detail includes embedded jobs")
	}
	if d["vacancyCount"] != int64(4) || d["id"] != int64(11) || d["favorite"] != true {
		t.Fatalf("detail state %+v", d)
	}
	if obj(d["reviews"])["plus"] != "Приватная подробность" {
		t.Fatal("lost detail field")
	}
}
func TestParseCatalogRequiresBothCollections(t *testing.T) {
	for _, bad := range []string{`not json`, `{"companies":[]}`, `{"agencies":[]}`, `null`} {
		if _, err := parseCatalog([]byte(bad)); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
