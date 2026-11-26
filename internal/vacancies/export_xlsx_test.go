package vacancies

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/xml"
	"io"
	"itmap/internal/xlsx"
	"strings"
	"testing"
)

func vacancyExportSheet(t *testing.T, file []byte) string {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(file), int64(len(file)))
	if err != nil {
		t.Fatal(err)
	}
	sheet, err := archive.Open("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer sheet.Close()
	data, err := io.ReadAll(sheet)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestVacancyExportWorkbook(t *testing.T) {
	book, err := xlsx.New("Вакансии")
	if err != nil {
		t.Fatal(err)
	}
	book.Sheet(exportHeaders, vacancyExportColumns)
	book.Row(sql.NullInt64{Int64: 17, Valid: true}, "=1+1 < &", "Markdown\nтекст", sql.NullInt64{})
	file, err := book.Finish()
	if err != nil {
		t.Fatal(err)
	}
	sheet := vacancyExportSheet(t, file)
	var parsed struct {
		Rows []struct {
			Cells []struct {
				Kind    string `xml:"t,attr"`
				Text    string `xml:"is>t"`
				Number  string `xml:"v"`
				Formula string `xml:"f"`
			} `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := xml.Unmarshal([]byte(sheet), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Rows) != 2 || len(parsed.Rows[0].Cells) != len(exportHeaders) {
		t.Fatal("неверное число строк или колонок")
	}
	for i, header := range exportHeaders {
		if parsed.Rows[0].Cells[i].Text != header {
			t.Fatalf("неверный заголовок колонки %d", i)
		}
	}
	row := parsed.Rows[1].Cells
	if row[0].Number != "17" || row[1].Kind != "inlineStr" || row[1].Text != "=1+1 < &" ||
		row[1].Formula != "" || row[2].Text != "Markdown\nтекст" || row[3].Number != "" ||
		!strings.Contains(sheet, `autoFilter ref="A1:S2"`) {
		t.Fatal("экспорт изменил значения или типы ячеек")
	}
}
