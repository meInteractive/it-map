package parser

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"testing"
)

func TestHHListingVacancyScopeAndCanonicalIdentity(t *testing.T) {
	raw := json.RawMessage(`{"title":"  Руководитель 😀  ","url":"https://spb.hh.ru/vacancy/123?utm_source=ci#x","vacancy_id":"123","employer_id":456}`)
	got, e := parseHHListingVacancy(raw, "https://hh.ru/employer/456")
	if e != nil || got.Title != "Руководитель 😀" || got.URL != "https://hh.ru/vacancy/123" || got.ExternalID != "123" {
		t.Fatal("HH projection", got, e)
	}
	for _, body := range []string{`null`, `{}`, `{"title":"CI","url":"https://hh.ru/vacancy/123","employer_id":999}`, `{"title":"CI","url":"https://ci.invalid/vacancy/123","employer_id":456}`, `{"title":"CI","url":"https://hh.ru/vacancy/123","employer_id":456,"vacancy_id":124}`, `{"title":"CI","url":"https://hh.ru/vacancy/123","employer_id":456,"archived":true}`} {
		if _, e := parseHHListingVacancy(json.RawMessage(body), "https://hh.ru/employer/456"); e == nil {
			t.Fatal("invalid HH row accepted")
		}
	}
}
func TestHHListingTitleFilterFailsOpenForUnmappedActiveRole(t *testing.T) {
	if got := hhTitleQuery([]string{"unknown"}); got != nil {
		t.Fatal("unknown role silently narrows HH")
	}
	if got := hhTitleQuery(nil); got != nil {
		t.Fatal("empty role list silently narrows HH")
	}
	got := hhTitleQuery([]string{"root-journalist", "root-journalist"})
	want := "NAME:(журналист* OR редактор* OR корреспондент* OR обозревател* OR спецкор* OR автор* OR editor OR journalist OR reporter OR writer OR columnist OR correspondent)"
	if got == nil || *got != want {
		t.Fatal("HH query changed", got)
	}
}

func TestHHListingDataMatchesUnchangedSource(t *testing.T) {
	type sample struct {
		Source string          `json:"source"`
		Item   json.RawMessage `json:"item"`
		Keys   []string        `json:"keys"`
	}
	cases := []sample{
		{"https://hh.ru/employer/456", json.RawMessage(`{"title":"  CI title 😀  ","url":"https://spb.hh.ru/vacancy/123?utm_source=ci#x","vacancy_id":123,"employer_id":"456"}`), []string{"root-it-manager", "root-journalist"}},
		{"https://hh.ru/search/vacancy?employer_id=456", json.RawMessage(`{"title":"CI","url":"https://hh.ru/vacancy/123/","employer_id":456.0}`), []string{"root-journalist", "root-journalist"}},
		{"https://hh.ru/vacancy/123", json.RawMessage(`{"title":"CI","url":"https://hh.ru/vacancy/999"}`), []string{"unmapped"}},
		{"https://hh.ru/employer/456", json.RawMessage(`{"title":"CI","url":"https://hh.ru/vacancy/123","employer_id":999}`), []string{}},
	}
	script := `import {apifyVacancy} from './server/services/apify-vacancies.js';import {hhTitleQuery} from './server/services/hh-title-filter.js';let input='';for await(const c of process.stdin)input+=c;const output=JSON.parse(input).map(x=>{let job=null;try{const j=apifyVacancy(x.item,{sourceUrl:x.source});job={title:j.title,url:j.url,id:j.externalJobId}}catch{}return {job,query:hhTitleQuery(x.keys)}});console.log(JSON.stringify(output));`
	raw, _ := json.Marshal(cases)
	cmd := exec.Command("node", "--input-type=module", "-e", script)
	cmd.Dir = "../.."
	cmd.Stdin = bytes.NewReader(raw)
	out, e := cmd.Output()
	if e != nil {
		t.Fatal("HH source oracle", e)
	}
	var expected []struct {
		Job   *struct{ Title, URL, ID string }
		Query *string
	}
	if e = json.Unmarshal(out, &expected); e != nil || len(expected) != len(cases) {
		t.Fatal("invalid HH oracle", e)
	}
	for i, c := range cases {
		got, e := parseHHListingVacancy(c.Item, c.Source)
		want := expected[i]
		if (e == nil) != (want.Job != nil) || want.Job != nil && (got.Title != want.Job.Title || got.URL != want.Job.URL || got.ExternalID != want.Job.ID) {
			t.Fatalf("HH source case %d changed: %+v %v", i, got, e)
		}
		query := hhTitleQuery(c.Keys)
		if (query == nil) != (want.Query == nil) || query != nil && *query != *want.Query {
			t.Fatalf("HH filter source case %d changed", i)
		}
	}
}
