package catalog

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestCompanySourceWorkbooks(t *testing.T) {
	companies := []storedEntity{
		{"name": "Синтетическая", "rank": json.Number("1"), "middle": []any{json.Number("200000"), nil},
			"vacancySources": map[string]any{"hh": "https://example.test/?a=1&b=2", "rabota": "https://example.test/rabota"},
			"reviews":        map[string]any{"plus": "=1+1", "minus": "Долгий найм"}},
		{"name": "Без рейтинга"},
	}
	ids := map[string]int64{"Синтетическая": 81}
	flags := map[string]entityState{"Синтетическая": {Favorite: true, Hidden: true}}
	for _, tc := range []struct {
		sheet        companySourceSheet
		onlyUnranked bool
		want, absent []string
	}{
		{vacancySourceSheet, false, []string{`ref="A1:U3"`, `r="A2" t="n"><v>81</v>`, `r="B2" t="n"><v>1</v>`, `r="F2" t="n"><v>200000</v>`,
			`r="D2" t="inlineStr"><is><t xml:space="preserve">да`, `r="E2" t="inlineStr"><is><t xml:space="preserve">да`,
			"https://example.test/?a=1&amp;b=2", "https://example.test/rabota"}, nil},
		{vacancySourceSheet, true, []string{`ref="A1:U2"`, "Без рейтинга"}, []string{"Синтетическая"}},
		{reviewSourceSheet, false, []string{`ref="A1:Q3"`, "YandexMap", `r="P2" t="inlineStr"><is><t xml:space="preserve">=1+1`, "Долгий найм"}, []string{"<f>", "rabota.ru"}},
	} {
		data, err := companySourceWorkbook(companies, ids, flags, tc.sheet, tc.onlyUnranked)
		if err != nil {
			t.Fatal(err)
		}
		book, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		workbook := sourceExportPart(t, book, "xl/workbook.xml")
		if !strings.Contains(workbook, `name="`+tc.sheet.name+`"`) {
			t.Fatalf("неверное имя листа: %s", workbook)
		}
		sheet := sourceExportPart(t, book, "xl/worksheets/sheet1.xml")
		for _, want := range tc.want {
			if !strings.Contains(sheet, want) {
				t.Errorf("%s: отсутствует %q", tc.sheet.name, want)
			}
		}
		for _, absent := range tc.absent {
			if strings.Contains(sheet, absent) {
				t.Errorf("%s: лишние данные %q", tc.sheet.name, absent)
			}
		}
	}
}

func sourceExportPart(t *testing.T, book *zip.Reader, name string) string {
	t.Helper()
	file, err := book.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
