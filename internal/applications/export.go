package applications

import (
	"context"
	"database/sql"
	"strconv"

	"itmap/internal/profiles"
)

type ExportApplication struct {
	ID           string          `json:"id"`
	Date         string          `json:"date"`
	Status       string          `json:"status"`
	Company      string          `json:"company"`
	VacancyID    string          `json:"vacancyId"`
	VacancyTitle string          `json:"vacancyTitle"`
	Resume       ResumeReference `json:"resume"`
}

type ExportVacancy struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

type ExportData struct {
	Applications []ExportApplication    `json:"applications"`
	Vacancies    []ExportVacancy        `json:"vacancies"`
	Resumes      []profiles.NamedResume `json:"resumes"`
}

// Export читает отклики, вакансии и личные резюме из одного снимка.
func (r Repository) Export(ctx context.Context, user string) (ExportData, error) {
	if r.DB == nil || user == "" {
		return ExportData{}, ErrInvalidInput
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return ExportData{}, err
	}
	defer tx.Rollback()
	resumes, err := profiles.ReadResumeCollection(ctx, tx, user)
	if err != nil {
		return ExportData{}, err
	}
	data := ExportData{Applications: []ExportApplication{}, Vacancies: []ExportVacancy{}, Resumes: resumes.Items}
	rows, err := tx.QueryContext(ctx, `SELECT a.id,a.created_at,COALESCE(a.status,''),a.company_name,
		a.vacancy_id,COALESCE(v.title,''),COALESCE(v.description_md,''),COALESCE(resume.resume_md,'')
		FROM company_applications a
		LEFT JOIN company_vacancies v ON v.id=a.vacancy_id
		LEFT JOIN user_profiles resume ON resume.user_name=CONCAT('`+applicationResumePrefix+`',a.id,':',a.user_name)
		WHERE a.user_name=? ORDER BY a.created_at DESC,a.id DESC`, user)
	if err != nil {
		return ExportData{}, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var entry ExportApplication
		var vacancy sql.NullInt64
		var description, snapshot string
		if err = rows.Scan(&entry.ID, &entry.Date, &entry.Status, &entry.Company, &vacancy, &entry.VacancyTitle, &description, &snapshot); err != nil {
			return ExportData{}, err
		}
		ref, err := readResumeReference(snapshot, entry.Date)
		if err != nil {
			return ExportData{}, err
		}
		// До выбора из нескольких резюме у кандидата было только первое.
		entry.Resume = ResumeReference{ID: 1, Name: resumes.Items[0].Name}
		if ref != nil {
			entry.Resume = *ref
		}
		if vacancy.Valid {
			entry.VacancyID = strconv.FormatInt(vacancy.Int64, 10)
			if !seen[entry.VacancyID] {
				seen[entry.VacancyID] = true
				data.Vacancies = append(data.Vacancies, ExportVacancy{entry.VacancyID, entry.VacancyTitle, description})
			}
		}
		data.Applications = append(data.Applications, entry)
	}
	if err = rows.Err(); err != nil {
		return ExportData{}, err
	}
	if err = rows.Close(); err != nil {
		return ExportData{}, err
	}
	if err = tx.Commit(); err != nil {
		return ExportData{}, err
	}
	return data, nil
}
