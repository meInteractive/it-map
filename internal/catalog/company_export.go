package catalog

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"itmap/internal/backup"
	"itmap/internal/rucollation"
	"itmap/internal/xlsx"
)

// Заголовки, проекции и инструкция соответствуют companyImportExportXlsx.
//
//go:embed company_export_layout.json
var companyExportLayout []byte

type companyExportSheet struct {
	Name            string
	Headers, Fields []string
	Numeric         []int
}

type companyExportFormat struct {
	Sheets       []companyExportSheet
	Instructions [][]string
}

func (r Repository) ExportCompaniesXLSX(ctx context.Context) ([]byte, error) {
	if r.DB == nil {
		return nil, errors.New("catalog export storage unavailable")
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	source, err := loadExportCatalog(ctx, tx)
	if err != nil {
		return nil, err
	}
	companyIDs, err := exportIDs(ctx, tx, "SELECT company_name,id FROM company_identity")
	if err != nil {
		return nil, err
	}
	agencyIDs, err := exportIDs(ctx, tx, "SELECT agency_name,id FROM agency_identity")
	if err != nil {
		return nil, err
	}
	contacts, err := exportContacts(ctx, tx, source, companyIDs, agencyIDs)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return companyWorkbook(source, companyIDs, agencyIDs, contacts)
}

func loadExportCatalog(ctx context.Context, tx *sql.Tx) (sourceCatalog, error) {
	var data []byte
	if err := tx.QueryRowContext(ctx, "SELECT data_json FROM app_catalog WHERE id=1").Scan(&data); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return sourceCatalog{}, err
	}
	normalized, err := backup.NormalizeStoredCatalog(data)
	if err != nil {
		return sourceCatalog{}, err
	}
	var source sourceCatalog
	decoder := json.NewDecoder(bytes.NewReader(normalized))
	decoder.UseNumber()
	if err := decoder.Decode(&source); err != nil {
		return sourceCatalog{}, err
	}
	return source, nil
}

func exportIDs(ctx context.Context, tx *sql.Tx, query string) (map[string]int64, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var name string
		var id int64
		if err = rows.Scan(&name, &id); err != nil {
			return nil, err
		}
		if _, exists := out[name]; exists {
			return nil, errors.New("ambiguous catalog identity")
		}
		out[name] = id
	}
	return out, rows.Err()
}

func exportContacts(ctx context.Context, tx *sql.Tx, source sourceCatalog, companies, agencies map[string]int64) ([][]any, error) {
	types := map[string]string{}
	for _, entity := range source.Agencies {
		types[nameOf(entity)] = "Агентство"
	}
	for _, entity := range source.Companies {
		types[nameOf(entity)] = "Компания"
	}
	rows, err := tx.QueryContext(ctx, `SELECT company_name,position,full_name,linkedin_url,facebook_url,
		setka_url,telegram,phone,email,specialization FROM hiring_contacts
		ORDER BY LOWER(company_name), position, BINARY company_name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]any
	for rows.Next() {
		var name string
		var pos int64
		var fields [8]sql.NullString
		if err = rows.Scan(&name, &pos, &fields[0], &fields[1], &fields[2], &fields[3], &fields[4], &fields[5], &fields[6], &fields[7]); err != nil {
			return nil, err
		}
		owner := types[name]
		if owner == "" {
			continue
		}
		id := companies[name]
		if owner == "Агентство" {
			id = agencies[name]
		}
		row := []any{owner, exportID(id), name, pos}
		for _, field := range fields {
			row = append(row, field.String)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func exportID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func companyWorkbook(source sourceCatalog, companies, agencies map[string]int64, contacts [][]any) ([]byte, error) {
	var layout companyExportFormat
	if err := json.Unmarshal(companyExportLayout, &layout); err != nil {
		return nil, err
	}
	var names []string
	for _, sheet := range layout.Sheets {
		names = append(names, sheet.Name)
	}
	book, err := xlsx.New(append(names, "Инструкция")...)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(source.Companies, func(i, j int) bool {
		return companies[nameOf(source.Companies[i])] < companies[nameOf(source.Companies[j])]
	})
	sort.SliceStable(source.Agencies, func(i, j int) bool {
		a, b := source.Agencies[i], source.Agencies[j]
		rank := func(entity storedEntity) float64 {
			n, _ := strconv.ParseFloat(exportText(entity["rank"]), 64)
			if n == 0 {
				return 9007199254740991
			}
			return n
		}
		if left, right := rank(a), rank(b); left != right {
			return left < right
		}
		return rucollation.Compare(rucollation.Keys(utf16.Encode([]rune(nameOf(a)))), rucollation.Keys(utf16.Encode([]rune(nameOf(b))))) < 0
	})
	for _, sheet := range layout.Sheets {
		book.Sheet(sheet.Headers, "")
		switch sheet.Name {
		case "Компании", "Отзывы":
			for _, entity := range source.Companies {
				book.Row(sheet.project(entity, companies[nameOf(entity)], "")...)
			}
		case "Офисы":
			for _, entity := range source.Companies {
				owner := nameOf(entity)
				for _, office := range asSlice(entity["offices"]) {
					book.Row(sheet.project(obj(office), companies[owner], owner)...)
				}
			}
		case "Агентства":
			for _, entity := range source.Agencies {
				book.Row(sheet.project(entity, agencies[nameOf(entity)], "")...)
			}
		case "Контакты":
			for _, row := range contacts {
				book.Row(row...)
			}
		}
	}
	book.Sheet(layout.Instructions[0], `<cols><col min="1" max="3" width="26" customWidth="1"/><col min="4" max="4" width="95" customWidth="1"/></cols>`)
	for _, line := range layout.Instructions[1:] {
		row := make([]any, len(line))
		for i, text := range line {
			row[i] = text
		}
		book.Row(row...)
	}
	return book.Finish()
}

func (s companyExportSheet) project(entity map[string]any, id int64, owner string) []any {
	row := make([]any, len(s.Fields))
	for i, field := range s.Fields {
		switch field {
		case "$id":
			row[i] = exportID(id)
		case "$owner":
			row[i] = owner
		default:
			value := exportField(entity, field)
			row[i] = exportText(value)
		}
	}
	for _, i := range s.Numeric {
		if text, ok := row[i].(string); ok && text != "" {
			if number, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) {
				row[i] = json.Number(strconv.FormatFloat(number, 'g', -1, 64))
			}
		}
	}
	return row
}

func exportField(entity map[string]any, path string) any {
	var value any = entity
	for _, key := range strings.Split(path, ".") {
		switch current := value.(type) {
		case map[string]any:
			value = current[key]
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(current) {
				return nil
			}
			value = current[i]
		default:
			return nil
		}
	}
	return value
}

func exportText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = exportText(item)
		}
		return strings.Join(parts, "; ")
	default:
		return ""
	}
}
