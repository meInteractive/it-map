package parser

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestHHDetailRequiresDescriptionBeforeMetadata(t *testing.T) {
	body := "Задачи: развивать продукт и управлять командой. Требования: опыт управления и необходимые навыки. " + strings.Repeat("CI synthetic description. ", 12)
	raw, _ := json.Marshal(map[string]any{"title": "CI HH title", "url": "https://hh.ru/vacancy/123", "vacancy_id": "123", "description_text": body, "company": "CI Company", "salary": "CI salary"})
	got, err := parseHHDetail(raw, "https://hh.ru/vacancy/123")
	if err != nil || got.Description != body || !strings.HasPrefix(got.Markdown, "# CI HH title\n\nКомпания: CI Company\n\nЗарплата: CI salary\n\n") {
		t.Fatal("detail projection", err)
	}
	for _, description := range []string{"short", strings.Repeat("CI ", 100), "captcha " + body} {
		raw, _ = json.Marshal(map[string]any{"title": "CI HH title", "url": "https://hh.ru/vacancy/123", "description_text": description, "company": strings.Repeat("Опыт. Задачи. Условия. ", 30)})
		if _, err := parseHHDetail(raw, "https://hh.ru/vacancy/123"); err == nil {
			t.Fatal("metadata inflated incomplete description")
		}
	}
	if _, err := parseHHDetail(raw, "https://hh.ru/vacancy/999"); err == nil {
		t.Fatal("detail target substitution accepted")
	}
}

func TestHHDetailMarkdownMatchesUnchangedSource(t *testing.T) {
	item := map[string]any{"title": "  CI title 😀  ", "url": "https://hh.ru/vacancy/123", "description_html": "<p>Задачи: развивать команду.</p><p>Требования: опыт управления.</p>" + strings.Repeat("<p>CI synthetic description.</p>", 15), "company": "CI Company", "location": "CI City", "salary": "CI salary", "work_format": []string{"CI office", "CI remote"}, "skills": []string{"CI skill"}}
	raw, _ := json.Marshal(item)
	script := `import {apifyVacancy} from './server/services/apify-vacancies.js';let raw='';for await(const c of process.stdin)raw+=c;const job=apifyVacancy(JSON.parse(raw),{sourceUrl:'https://hh.ru/vacancy/123',detail:true});console.log(JSON.stringify({description:job.description,markdown:job.rawMarkdown,format:job.descriptionFormat}));`
	cmd := exec.Command("node", "--input-type=module", "-e", script)
	cmd.Dir = "../.."
	cmd.Stdin = bytes.NewReader(raw)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal("HH detail source", err)
	}
	var want struct{ Description, Markdown, Format string }
	if json.Unmarshal(output, &want) != nil {
		t.Fatal("invalid source detail")
	}
	got, err := parseHHDetail(raw, "https://hh.ru/vacancy/123")
	if err != nil || got.Description != want.Description || got.Markdown != want.Markdown || got.Format != want.Format {
		t.Fatal("HH detail Markdown changed", err)
	}
}
