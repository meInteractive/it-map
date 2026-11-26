package vacancies

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/xml"
	"fmt"
	"io"
)

const sheetNS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
const officeNS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
const packageNS = "http://schemas.openxmlformats.org/package/2006/relationships"

type vacancyWorkbook struct {
	buffer    bytes.Buffer
	zip       *zip.Writer
	sheet     io.Writer
	rowNumber int
	err       error
}

func newVacancyWorkbook() (*vacancyWorkbook, error) {
	b := &vacancyWorkbook{}
	b.zip = zip.NewWriter(&b.buffer)
	parts := []struct{ name, content string }{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`},
		{"_rels/.rels", `<Relationships xmlns="` + packageNS + `"><Relationship Id="rId1" Type="` + officeNS + `/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<workbook xmlns="` + sheetNS + `" xmlns:r="` + officeNS + `"><sheets><sheet name="Вакансии" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships xmlns="` + packageNS + `"><Relationship Id="rId1" Type="` + officeNS + `/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`},
	}
	for _, part := range parts {
		w, err := b.zip.Create(part.name)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w, part.content); err != nil {
			return nil, err
		}
	}
	var err error
	b.sheet, err = b.zip.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return nil, err
	}
	b.write(`<worksheet xmlns="` + sheetNS + `"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><cols><col min="1" max="3" width="28" customWidth="1"/><col min="4" max="4" width="55" customWidth="1"/><col min="5" max="11" width="22" customWidth="1"/><col min="12" max="12" width="85" customWidth="1"/><col min="13" max="19" width="28" customWidth="1"/></cols><sheetData>`)
	headers := make([]any, len(exportHeaders))
	for i, title := range exportHeaders {
		headers[i] = title
	}
	b.row(headers...)
	return b, b.err
}

func (b *vacancyWorkbook) write(s string) {
	if b.err == nil {
		_, b.err = io.WriteString(b.sheet, s)
	}
}

func (b *vacancyWorkbook) row(values ...any) {
	b.rowNumber++
	b.write(fmt.Sprintf(`<row r="%d">`, b.rowNumber))
	for i, value := range values {
		ref := fmt.Sprintf("%c%d", 'A'+i, b.rowNumber)
		if number, ok := value.(sql.NullInt64); ok && number.Valid {
			b.write(fmt.Sprintf(`<c r="%s" t="n"><v>%d</v></c>`, ref, number.Int64))
			continue
		}
		text, _ := value.(string)
		b.write(`<c r="` + ref + `" t="inlineStr"><is><t xml:space="preserve">`)
		if b.err == nil {
			b.err = xml.EscapeText(b.sheet, []byte(text))
		}
		b.write("</t></is></c>")
	}
	b.write("</row>")
}

func (b *vacancyWorkbook) finish() ([]byte, error) {
	b.write(fmt.Sprintf(`</sheetData><autoFilter ref="A1:S%d"/></worksheet>`, b.rowNumber))
	if b.err != nil {
		return nil, b.err
	}
	if err := b.zip.Close(); err != nil {
		return nil, err
	}
	return b.buffer.Bytes(), nil
}
