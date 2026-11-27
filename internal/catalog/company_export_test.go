package catalog

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestCompanyWorkbook(t *testing.T) {
	var source sourceCatalog
	decoder := json.NewDecoder(strings.NewReader(`{"companies":[{"name":"=CI < &","rating":4.3,"offices":[{"address":"CI office","lat":55.75}],"reviews":{"plus":"CI review"}}],"agencies":[{"name":"CI agency","agencyTypes":["IT","HR"]}]}`))
	decoder.UseNumber()
	if err := decoder.Decode(&source); err != nil {
		t.Fatal(err)
	}
	data, err := companyWorkbook(source, map[string]int64{"=CI < &": 7}, map[string]int64{"CI agency": 9}, [][]any{{"Компания", int64(7), "=CI < &", int64(1), "CI contact"}})
	if err != nil {
		t.Fatal(err)
	}
	book, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	member := func(name string) string {
		t.Helper()
		part, err := book.Open(name)
		if err != nil {
			t.Fatal(err)
		}
		defer part.Close()
		raw, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	workbook := member("xl/workbook.xml")
	for _, name := range []string{"Компании", "Офисы", "Отзывы", "Агентства", "Контакты", "Инструкция"} {
		if !strings.Contains(workbook, `name="`+name+`"`) {
			t.Fatalf("нет листа %s", name)
		}
	}
	for i, checks := range [][]string{
		{`<c r="A2" t="n"><v>7</v>`, `<c r="B2" t="inlineStr"><is><t xml:space="preserve">=CI &lt; &amp;`, `<c r="F2" t="n"><v>4.3</v>`, `BJ1`},
		{"CI office", `<c r="I2" t="n"><v>55.75</v>`},
		{"CI review"},
		{"CI agency", "IT; HR"},
		{"CI contact"},
		{"Формула рейтинга v5"},
	} {
		sheet := member("xl/worksheets/sheet" + string(rune('1'+i)) + ".xml")
		for _, check := range checks {
			if !strings.Contains(sheet, check) {
				t.Fatalf("лист %d: нет %q", i+1, check)
			}
		}
	}
}
