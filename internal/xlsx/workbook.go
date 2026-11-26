package xlsx

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const sheetNS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
const officeNS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
const packageNS = "http://schemas.openxmlformats.org/package/2006/relationships"

type Workbook struct {
	buffer                          bytes.Buffer
	zip                             *zip.Writer
	sheet                           io.Writer
	rowNumber, columns, sheetNumber int
	err                             error
}

func New(sheetNames ...string) (*Workbook, error) {
	b := &Workbook{}
	b.zip = zip.NewWriter(&b.buffer)
	var overrides, names, relationships strings.Builder
	for i, name := range sheetNames {
		n := i + 1
		fmt.Fprintf(&overrides, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, n)
		fmt.Fprintf(&names, `<sheet name="`)
		_ = xml.EscapeText(&names, []byte(name))
		fmt.Fprintf(&names, `" sheetId="%d" r:id="rId%d"/>`, n, n)
		fmt.Fprintf(&relationships, `<Relationship Id="rId%d" Type="%s/worksheet" Target="worksheets/sheet%d.xml"/>`, n, officeNS, n)
	}
	parts := []struct{ name, content string }{
		{"[Content_Types].xml", `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` + overrides.String() + `</Types>`},
		{"_rels/.rels", `<Relationships xmlns="` + packageNS + `"><Relationship Id="rId1" Type="` + officeNS + `/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<workbook xmlns="` + sheetNS + `" xmlns:r="` + officeNS + `"><sheets>` + names.String() + `</sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<Relationships xmlns="` + packageNS + `">` + relationships.String() + `</Relationships>`},
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
	return b, nil
}

func (b *Workbook) Sheet(headers []string, columnsXML string) {
	b.closeSheet()
	if b.err != nil {
		return
	}
	b.sheetNumber++
	b.rowNumber, b.columns = 0, len(headers)
	b.sheet, b.err = b.zip.Create(fmt.Sprintf("xl/worksheets/sheet%d.xml", b.sheetNumber))
	b.write(`<worksheet xmlns="` + sheetNS + `"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>` + columnsXML + `<sheetData>`)
	row := make([]any, len(headers))
	for i, header := range headers {
		row[i] = header
	}
	b.Row(row...)
}

func columnName(column int) string {
	var label [8]byte
	pos := len(label)
	for column > 0 {
		column--
		pos--
		label[pos] = byte('A' + column%26)
		column /= 26
	}
	return string(label[pos:])
}

func (b *Workbook) write(s string) {
	if b.err == nil {
		_, b.err = io.WriteString(b.sheet, s)
	}
}

func (b *Workbook) Row(values ...any) {
	b.rowNumber++
	b.write(fmt.Sprintf(`<row r="%d">`, b.rowNumber))
	for i, value := range values {
		ref := columnName(i+1) + strconv.Itoa(b.rowNumber)
		switch n := value.(type) {
		case sql.NullInt64:
			if n.Valid {
				value = json.Number(strconv.FormatInt(n.Int64, 10))
			}
		case int64:
			value = json.Number(strconv.FormatInt(n, 10))
		}
		if number, ok := value.(json.Number); ok {
			b.write(fmt.Sprintf(`<c r="%s" t="n"><v>%s</v></c>`, ref, number))
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

func (b *Workbook) closeSheet() {
	if b.sheet != nil {
		b.write(fmt.Sprintf(`</sheetData><autoFilter ref="A1:%s%d"/></worksheet>`, columnName(b.columns), b.rowNumber))
		b.sheet = nil
	}
}

func (b *Workbook) Finish() ([]byte, error) {
	b.closeSheet()
	if b.err != nil {
		return nil, b.err
	}
	if err := b.zip.Close(); err != nil {
		return nil, err
	}
	return b.buffer.Bytes(), nil
}
