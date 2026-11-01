package hhpublic

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func fixtureJob(title, description string) string {
	raw, _ := json.Marshal(map[string]any{"@type": "JobPosting", "identifier": map[string]any{"value": "123"}, "url": "https://hh.ru/vacancy/123", "title": title, "description": description, "hiringOrganization": map[string]any{"name": "CI Company"}, "datePosted": "2026-11-01T08:00:00Z"})
	return `<script type="application/ld+json">` + string(raw) + `</script>`
}
func TestPublicHHParsesOnlyOneMatchingActivePosting(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	html := fixtureJob("<b>CI &amp; title</b>", "<p>Задачи</p><ul><li>CI &lt;one&gt;</li></ul><script>private</script>")
	got := ParseJob(html, "123", now)
	if got == nil || got.Title != "CI & title" || got.Description != "Задачи\n\n- CI <one>" || got.URL != "https://hh.ru/vacancy/123" || got.DatePosted != "2026-11-01" {
		t.Fatalf("public projection %+v", got)
	}
	for _, input := range []string{html + html, strings.Replace(html, `"value":"123"`, `"value":"999"`, 1), strings.Replace(html, `"@type":"JobPosting"`, `"@type":"Article"`, 1), strings.Replace(html, `"datePosted"`, `"validThrough"`, 1), "<h1>Access denied</h1>"} {
		if ParseJob(input, "123", now) != nil {
			t.Fatal("ambiguous/expired/incorrect posting accepted")
		}
	}
}
func TestHHHTMLTextDropsExecutableContentAndKeepsSourceText(t *testing.T) {
	got := HTMLText("<!-- CI --><p>A&nbsp;&#x1F600;</p><style>secret</style><div>B<br>C &unknown;</div>")
	if got != "A 😀\nB\nC &unknown;" {
		t.Fatalf("HTML text %q", got)
	}
}

func TestPublicHHMatchesUnchangedSource(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	valid := fixtureJob("<b>CI &amp; 😀</b>", "<p>Задачи</p><ul><li>CI</li></ul>")
	cases := []string{valid, valid + valid, strings.Replace(valid, `"value":"123"`, `"value":"999"`, 1), strings.Replace(valid, `"datePosted"`, `"validThrough"`, 1), `<script type="application/ld+json">{"@graph":[{"@type":"JobPosting","identifier":{"value":123},"title":"CI","description":"CI full text"}]}</script>`}
	script := `import {parsePublicHhJob} from './server/integrations/hh-public.js';let raw='';for await(const c of process.stdin)raw+=c;console.log(JSON.stringify(JSON.parse(raw).map(html=>{const j=parsePublicHhJob(html,'123',{now:Date.parse('2026-11-01T12:00:00Z')});return j?{vacancy_id:j.vacancy_id,url:j.url,title:j.title,description_text:j.description_text,company:j.company??'',date_posted:j.date_posted??''}:null})));`
	raw, _ := json.Marshal(cases)
	cmd := exec.Command("node", "--input-type=module", "-e", script)
	cmd.Dir = "../../.."
	cmd.Stdin = bytes.NewReader(raw)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal("public HH source oracle", err)
	}
	var expected []*Job
	if json.Unmarshal(output, &expected) != nil || len(expected) != len(cases) {
		t.Fatal("invalid source projection")
	}
	for i, html := range cases {
		got := ParseJob(html, "123", now)
		if !reflect.DeepEqual(got, expected[i]) {
			t.Fatalf("source case %d changed: %+v != %+v", i, got, expected[i])
		}
	}
}
