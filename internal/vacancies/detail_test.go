package vacancies

import (
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestEffectiveLetterPreservesLegacyPrecedence(t *testing.T) {
	personal := sql.NullString{String: "Personal", Valid: true}
	markdown := sql.NullString{String: "Private markdown", Valid: true}
	cases := []struct {
		shared, markdown string
		override, exists bool
		scope, want      string
	}{
		{"Shared", "Text", false, false, "shared", "Shared"},
		{"Shared", "Text", false, true, "shared", "Shared"},
		{"Shared", "", false, true, "personal", "Personal"},
		{"Shared", "Text", true, true, "personal", "Personal"},
		{"Shared", "", true, false, "shared", "Shared"},
	}
	for _, c := range cases {
		got, md, scope := EffectiveLetter(c.shared, c.markdown, personal, markdown, c.override, c.exists)
		if got != c.want || scope != c.scope || ((scope == "personal") != (md == "Private markdown")) {
			t.Fatalf("letter wrong: %+v got %q/%q/%q", c, got, md, scope)
		}
	}
}
func TestDetailResponseMarshalsLegacyShape(t *testing.T) {
	d := Detail{Vacancy: Vacancy{ID: 10, CompanyName: "Sample", Role: "Manager", TelegramSources: []TelegramSource{}},
		URL: "https://example.org", Revision: strings.Repeat("a", 64), TelegramSources: []DetailTelegramSource{{ChannelID: 1, ChannelName: "Tech", MessageURL: "https://t.me/sample/1"}}, PublicationSources: []PublicationSource{}}
	b, err := json.Marshal(DetailResponse{Vacancy: d, Entities: []DirectoryEntity{}})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Vacancy map[string]json.RawMessage `json:"vacancy"`
	}
	if err = json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "companyName", "url", "descriptionMd", "revision", "telegramSources", "publicationSources", "roles", "coverLetterScope"} {
		if _, found := decoded.Vacancy[key]; !found {
			t.Errorf("missing %q: %s", key, b)
		}
	}
	rev, err := DetailRevision(d, map[string]any{"secret": "other private user's data is not included"})
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(rev) {
		t.Fatalf("invalid token: %s", rev)
	}
	changed := d
	changed.DescriptionMD = "Changed"
	next, _ := DetailRevision(changed, nil)
	same, _ := DetailRevision(d, nil)
	if same == next {
		t.Fatal("revision does not change when vacancy changes")
	}
}
func TestDetailEntitiesOnlyNeededOrganisations(t *testing.T) {
	data := []byte(`{"companies":[{"name":"A","rank":1,"middle":[100,200],"jobs":[{"url":"hidden"}]},{"name":"B","rank":2}],"agencies":[{"name":"Agency","applicationUrl":"https://example.org"}]}`)
	entities, err := detailEntities(data, "Agency", "A")
	if err != nil {
		t.Fatal(err)
	}
	if len(entities) != 2 || entities[0].Name != "A" || entities[1].EntityType != "agency" {
		t.Fatalf("wrong entities: %+v", entities)
	}
	b, _ := json.Marshal(entities)
	if strings.Contains(string(b), `"url":"hidden"`) {
		t.Fatal("leaked catalog job")
	}
}
