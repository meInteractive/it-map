package vacancies

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"itmap/internal/roles"
)

const MaxImportRows = 2000
const manualVacancyCreateLock = "itmap:manual-vacancy:create"

type ImportRequest struct {
	Mode string            `json:"mode"`
	Rows []json.RawMessage `json:"rows"`
}

type ImportError string

func (e ImportError) Error() string { return string(e) }

type ImportIssue struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

type ImportResult struct {
	OK             bool          `json:"ok"`
	Imported       int           `json:"imported"`
	Updated        int           `json:"updated"`
	Duplicates     int           `json:"duplicates"`
	MissingSkipped int           `json:"missingSkipped"`
	Invalid        int           `json:"invalid"`
	LimitSkipped   int           `json:"limitSkipped"`
	Total          int           `json:"total"`
	Errors         []ImportIssue `json:"errors"`
}

type vacancyImportRow struct {
	Input
	CompanyID             json.RawMessage `json:"companyId"`
	AgencyID              json.RawMessage `json:"agencyId"`
	AgencyName            string          `json:"agencyName"`
	RealCompanyID         json.RawMessage `json:"realCompanyId"`
	Score                 json.RawMessage `json:"relevance"`
	From                  json.RawMessage `json:"salaryFrom"`
	To                    json.RawMessage `json:"salaryTo"`
	LetterScope           string          `json:"coverLetterScope"`
	HasSubject, HasLetter bool
}

func vacancyImportInteger(raw json.RawMessage) (*int64, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return nil, nil
	}
	if strings.HasPrefix(text, `"`) {
		var decoded string
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, err
		}
		text = strings.TrimSpace(decoded)
	}
	if text == "" {
		return nil, nil
	}
	n, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(n) || math.Abs(n) > 9007199254740991 || math.Trunc(n) != n {
		return nil, errors.New("ожидается целое число")
	}
	result := int64(n)
	return &result, nil
}

type vacancyImportOwners struct {
	companies, agencies       map[int64]string
	companyNames, agencyNames map[string]bool
}

func (owners vacancyImportOwners) input(raw json.RawMessage, directory roles.Catalog) (vacancyImportRow, error) {
	var row vacancyImportRow
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &row); err != nil || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return row, errors.New("некорректная строка вакансии")
	}
	_, row.HasSubject = fields["coverLetterSubject"]
	_, row.HasLetter = fields["coverLetterMd"]
	if strings.EqualFold(strings.TrimSpace(row.LetterScope), "personal") {
		return row, errors.New("личное письмо нельзя импортировать в общие поля")
	}
	for _, value := range []struct {
		raw    json.RawMessage
		target **int64
		label  string
	}{
		{row.Score, &row.Relevance, "Релевантность"}, {row.From, &row.SalaryFrom, "Зарплата от"}, {row.To, &row.SalaryTo, "Зарплата до"},
	} {
		n, err := vacancyImportInteger(value.raw)
		if err != nil {
			return row, fmt.Errorf("%s: %w", value.label, err)
		}
		*value.target = n
	}
	agencyID, err := vacancyImportInteger(row.AgencyID)
	if err != nil {
		return row, errors.New("некорректный ID агентства")
	}
	ownerType := strings.ToLower(strings.TrimSpace(row.EntityType))
	agency := ownerType == "agency" || strings.Contains(ownerType, "агент") || agencyID != nil || strings.TrimSpace(row.AgencyName) != ""
	if agency {
		row.EntityType = "agency"
		row.CompanyName = strings.TrimSpace(row.AgencyName)
		if agencyID != nil && owners.agencies[*agencyID] != "" {
			row.CompanyName = owners.agencies[*agencyID]
		}
		if !owners.agencyNames[row.CompanyName] {
			return row, errors.New("агентство с таким ID или названием не найдено")
		}
		realID, err := vacancyImportInteger(row.RealCompanyID)
		if err != nil {
			return row, errors.New("некорректный ID реального работодателя")
		}
		if realID != nil && owners.companies[*realID] != "" {
			row.RealCompanyName = owners.companies[*realID]
		}
	} else {
		row.EntityType = "company"
		id, err := vacancyImportInteger(row.CompanyID)
		if err != nil || id == nil || *id <= 0 {
			return row, errors.New("некорректный ID компании")
		}
		row.CompanyName = owners.companies[*id]
		if row.CompanyName == "" {
			return row, errors.New("компания с таким ID не найдена")
		}
	}
	// В шаблоне Excel нет колонок для записи связей Telegram.
	row.TelegramChannelID, row.TelegramMessageURL, row.TelegramContact = nil, "", ""
	normalized, err := NormalizeInput(row.Input, true)
	if err != nil {
		if !validDate(strings.TrimSpace(row.PublishedAt)) || !validDate(strings.TrimSpace(row.LoadedAt)) {
			return row, errors.New("укажите даты в формате ГГГГ-ММ-ДД")
		}
		return row, errors.New("некорректная ссылка, зарплата, оценка, уровень или длина текста")
	}
	normalized.URL = vacancyImportURLKey(normalized.URL)
	if normalized.Role != "" || len(normalized.RoleIDs) > 0 {
		ids, label, err := resolveManualRoles(normalized, directory)
		if err != nil {
			return row, errors.New("выберите существующие конечные роли вакансий")
		}
		normalized.RoleIDs, normalized.Role = ids, label
	}
	row.Input = normalized
	return row, nil
}

func vacancyImportURLKey(text string) string {
	u, err := url.Parse(text)
	if err != nil || u.Hostname() == "" {
		return text
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port != "" && !(u.Scheme == "https" && port == "443") && !(u.Scheme == "http" && port == "80") {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	u.Host = host
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String()
}

type importedVacancy struct {
	id                           int64
	owner, kind, subject, letter string
}

func (r Repository) ImportExcel(ctx context.Context, user string, req ImportRequest) (ImportResult, error) {
	if req.Mode == "" {
		req.Mode = "create"
	}
	switch {
	case req.Mode != "create" && req.Mode != "update":
		return ImportResult{}, ImportError("Указан неизвестный режим импорта.")
	case len(req.Rows) == 0:
		return ImportResult{}, ImportError("В Excel-файле нет вакансий для импорта.")
	case len(req.Rows) > MaxImportRows:
		return ImportResult{}, ImportError("За один раз можно импортировать не более 2000 вакансий.")
	case r.DB == nil || strings.TrimSpace(user) == "":
		return ImportResult{}, errors.New("vacancy import storage unavailable")
	}
	directory, err := (roles.Repository{DB: r.DB}).Read(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	conn, err := r.DB.Conn(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	defer conn.Close()
	var lock sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,5)", manualVacancyCreateLock).Scan(&lock); err != nil {
		return ImportResult{}, err
	}
	if lock.Int64 != 1 {
		return ImportResult{}, ErrWriteBusy
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(release, "DO RELEASE_LOCK(?)", manualVacancyCreateLock)
	}()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return ImportResult{}, err
	}
	defer tx.Rollback()
	var generation string
	if err = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation); err != nil {
		return ImportResult{}, err
	}
	data, owners, err := readVacancyImportOwners(ctx, tx)
	if err != nil {
		return ImportResult{}, err
	}
	existing, ambiguous, err := readImportedVacancies(ctx, tx)
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{OK: true, Total: len(req.Rows), Errors: []ImportIssue{}}
	seen := map[string]bool{}
	hasAgency := false
	for i, raw := range req.Rows {
		row, issue := owners.input(raw, directory)
		if issue == nil && seen[row.URL] {
			issue = errors.New("ссылка повторяется в импортируемом файле")
		}
		item, found := existing[row.URL]
		if issue == nil {
			seen[row.URL] = true
			if ambiguous[row.URL] {
				issue = errors.New("в базе несколько вакансий с этой ссылкой")
			}
			if found && (item.owner != row.CompanyName || item.kind != row.EntityType) {
				issue = errors.New("ссылка уже принадлежит другой организации")
			}
		}
		if issue != nil {
			result.Invalid++
			if len(result.Errors) < 50 {
				result.Errors = append(result.Errors, ImportIssue{i + 2, issue.Error()})
			}
			continue
		}
		if found && req.Mode == "create" {
			result.Duplicates++
			continue
		}
		if !found && req.Mode == "update" {
			result.MissingSkipped++
			continue
		}
		if err = writeImportedVacancy(ctx, tx, user, row, item); err != nil {
			return ImportResult{}, err
		}
		if found {
			result.Updated++
		} else {
			result.Imported++
		}
		hasAgency = hasAgency || row.EntityType == "agency"
	}
	if hasAgency {
		if err = refreshImportedAgencySalaries(ctx, tx, data); err != nil {
			return ImportResult{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return ImportResult{}, err
	}
	return result, nil
}
