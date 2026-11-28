package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"itmap/internal/xlsx"
)

type companySourceSheet struct {
	name    string
	headers []string
	columns string
	fields  []string
}

const sourceBaseColumns = `<cols><col min="1" max="2" width="18" customWidth="1"/><col min="3" max="3" width="30" customWidth="1"/><col min="4" max="5" width="13" customWidth="1"/>`

var vacancySourceSheet = companySourceSheet{
	name: "Компании",
	headers: []string{"Middle — от", "Middle — до", "Senior — от", "Senior — до", "Lead — от", "Lead — до", "Head — от", "Head — до",
		"Сайт компании", "HH", "Хабр", "Getmatch", "Telegram", "DreamJob", "Эйч", "rabota.ru"},
	columns: sourceBaseColumns + `<col min="6" max="13" width="16" customWidth="1"/><col min="14" max="21" width="54" customWidth="1"/></cols>`,
	fields: []string{"middle.0", "middle.1", "senior.0", "senior.1", "lead.0", "lead.1", "head.0", "head.1",
		"vacancySources.companySite", "vacancySources.hh", "vacancySources.habr", "vacancySources.getmatch", "vacancySources.telegram",
		"vacancySources.dreamJob", "vacancySources.hCareers", "vacancySources.rabota"},
}

var reviewSourceSheet = companySourceSheet{
	name: "Отзывы",
	headers: []string{"Habr", "DreamJob", "GlassDoor", "SuperJob", "Правда Сотрудников", "Nahjob", "BossNadzor", "YandexMap",
		"Plus Short", "Minus Short", "Plus", "Minus"},
	columns: sourceBaseColumns + `<col min="6" max="13" width="54" customWidth="1"/><col min="14" max="17" width="60" customWidth="1"/></cols>`,
	fields: []string{"reviewSources.habr", "reviewSources.dreamJob", "reviewSources.glassDoor", "reviewSources.superJob",
		"reviewSources.pravdaSotrudnikov", "reviewSources.nahjob", "reviewSources.bossNadzor", "reviewSources.yandexMap",
		"reviews.plusShort", "reviews.minusShort", "reviews.plus", "reviews.minus"},
}

func (r Repository) ExportVacancySourcesXLSX(ctx context.Context, user string, onlyUnranked bool) ([]byte, error) {
	return r.exportCompanySources(ctx, user, vacancySourceSheet, onlyUnranked)
}

func (r Repository) ExportReviewSourcesXLSX(ctx context.Context, user string) ([]byte, error) {
	return r.exportCompanySources(ctx, user, reviewSourceSheet, false)
}

func (r Repository) exportCompanySources(ctx context.Context, user string, sheet companySourceSheet, onlyUnranked bool) ([]byte, error) {
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
	ids, err := exportIDs(ctx, tx, "SELECT company_name,id FROM company_identity")
	if err != nil {
		return nil, err
	}
	flags, err := readCompanyFlags(ctx, tx, user)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return companySourceWorkbook(source.Companies, ids, flags, sheet, onlyUnranked)
}

func readCompanyFlags(ctx context.Context, tx *sql.Tx, user string) (map[string]entityState, error) {
	rows, err := tx.QueryContext(ctx, "SELECT company_name, favorite, hidden FROM company_user_state WHERE user_name=?", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	flags := map[string]entityState{}
	for rows.Next() {
		var name string
		var favorite, hidden int64
		if err := rows.Scan(&name, &favorite, &hidden); err != nil {
			return nil, err
		}
		if _, exists := flags[name]; exists {
			return nil, errors.New("ambiguous catalog personal state")
		}
		flags[name] = entityState{Favorite: favorite != 0, Hidden: hidden != 0}
	}
	return flags, rows.Err()
}

func companySourceWorkbook(companies []storedEntity, ids map[string]int64, flags map[string]entityState, sheet companySourceSheet, onlyUnranked bool) ([]byte, error) {
	book, err := xlsx.New(sheet.name)
	if err != nil {
		return nil, err
	}
	headers := []string{"ID компании в БД", "Место в рейтинге", "Название компании", "Избранная", "Скрытая"}
	book.Sheet(append(headers, sheet.headers...), sheet.columns)
	// Нормализация каталога уже сортирует компании по рейтингу и названию.
	for _, company := range companies {
		rank, _ := strconv.ParseInt(exportText(company["rank"]), 10, 64)
		if onlyUnranked && rank > 0 {
			continue
		}
		name := nameOf(company)
		row := []any{exportID(ids[name]), exportID(rank), name, nil, nil}
		if flags[name].Favorite {
			row[3] = "да"
		}
		if flags[name].Hidden {
			row[4] = "да"
		}
		for _, field := range sheet.fields {
			row = append(row, exportField(company, field))
		}
		book.Row(row...)
	}
	return book.Finish()
}
