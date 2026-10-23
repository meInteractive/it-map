package catalog

import (
	"encoding/json"
	"testing"
)

func TestMatchesFromJSON(t *testing.T) {
	fixture := []byte(`{"companies":[{"name":"Альфа","sector":"Банк","roles":"Разработка","offices":[{"metro":"Арбатская","address":"Москва"}],"jobs":[{"title":"Engineering Manager"}],"revenue":{"legal":"ООО Тест","metric":"Выручка"},"vpnAi":{"ai":"LLM"}},{"name":"Бета","sector":"Ритейл","roles":"Аналитика"}],"agencies":[{"name":"Агентство"}]}`)
	for _, tc := range []struct {
		query, want string
		size        int
	}{{"АЛЬФА", "Альфа", 1}, {"Арбатская", "Альфа", 1}, {"engineering", "Альфа", 1}, {"LLM", "Альфа", 1}, {"РИТЕЙЛ", "Бета", 1}, {"нет-совпадения", "", 0}, {"", "Альфа", 2}} {
		got, err := MatchesFromJSON(fixture, tc.query)
		if err != nil || len(got) != tc.size {
			t.Fatalf("query %q: got %+v err=%v", tc.query, got, err)
		}
		if tc.size > 0 && got[0].Name != tc.want {
			t.Fatalf("query %q mismatch: %+v", tc.query, got)
		}
		output, _ := json.Marshal(got)
		if string(output) == "null" {
			t.Fatal("matches array must never be null")
		}
	}
	if _, err := MatchesFromJSON([]byte(`not json`), "x"); err == nil {
		t.Fatal("corrupted catalog accepted")
	}
}
