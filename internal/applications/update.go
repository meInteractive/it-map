package applications

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf16"
)

var (
	ErrRevisionRequired = errors.New("application revision required")
	ErrConflict         = errors.New("application changed concurrently")
	ErrInvalidInput     = errors.New("invalid application fields")
	ErrInvalidVacancy   = errors.New("vacancy does not belong to company")
	ErrDuplicateVacancy = errors.New("an application already uses this vacancy")
)

var revisionPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var emailPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// UpdateInput contains the editable v316 application fields. Nil Sources
// means the legacy default ["hh"], not "preserve existing sources".
type UpdateInput struct {
	Revision             string   `json:"revision"`
	VacancyID            *int64   `json:"vacancyId"`
	Status               string   `json:"status"`
	Comment              string   `json:"comment"`
	RecruiterName        string   `json:"recruiterName"`
	RecruiterPhone       string   `json:"recruiterPhone"`
	RecruiterEmail       string   `json:"recruiterEmail"`
	RecruiterContactType string   `json:"recruiterContactType"`
	ApplicationSources   []string `json:"applicationSources"`
}

// normalize enforces the old UI constraints before opening a write transaction.
// Input lengths are counted as JavaScript UTF-16 code units, not Go bytes.
func (in UpdateInput) normalize() (UpdateInput, error) {
	if !revisionPattern.MatchString(in.Revision) {
		return in, ErrRevisionRequired
	}
	if in.VacancyID != nil && (*in.VacancyID < 1 || *in.VacancyID > maxLegacyInteger) {
		return in, fmt.Errorf("%w: invalid vacancy ID", ErrInvalidInput)
	}
	knownStatus := false
	for _, s := range []string{"", "Откликнулся", "Получил ответ", "Скрининг", "Собес 2", "Собес 3", "Собес 4", "Офер", "Отказ"} {
		if in.Status == s {
			knownStatus = true
			break
		}
	}
	if !knownStatus || len(utf16.Encode([]rune(in.Comment))) > 1000 {
		return in, fmt.Errorf("%w: invalid status or comment", ErrInvalidInput)
	}
	in.RecruiterName = trimLegacyText(in.RecruiterName)
	in.RecruiterPhone = trimLegacyText(in.RecruiterPhone)
	in.RecruiterEmail = trimLegacyText(in.RecruiterEmail)
	if len(utf16.Encode([]rune(in.RecruiterName))) > 160 || len(utf16.Encode([]rune(in.RecruiterPhone))) > 64 || len(utf16.Encode([]rune(in.RecruiterEmail))) > 254 || (in.RecruiterEmail != "" && (!emailPattern.MatchString(in.RecruiterEmail) || strings.IndexFunc(in.RecruiterEmail, legacyWhitespace) >= 0)) {
		return in, fmt.Errorf("%w: invalid recruiter contact", ErrInvalidInput)
	}
	if in.RecruiterContactType != "" && in.RecruiterContactType != "direct" && in.RecruiterContactType != "agency" {
		return in, fmt.Errorf("%w: unsupported recruiter contact type", ErrInvalidInput)
	}
	if in.ApplicationSources == nil {
		in.ApplicationSources = []string{"hh"}
	}
	allowed := map[string]bool{"hh": true, "site": true, "habr": true, "email": true, "telegram": true, "hcm": true}
	seen := map[string]bool{}
	normalized := make([]string, 0, len(in.ApplicationSources))
	for _, source := range in.ApplicationSources {
		source = trimLegacyText(source)
		if !allowed[source] {
			return in, fmt.Errorf("%w: unknown application source", ErrInvalidInput)
		}
		if !seen[source] {
			normalized = append(normalized, source)
			seen[source] = true
		}
	}
	if len(normalized) == 0 {
		return in, fmt.Errorf("%w: application sources required", ErrInvalidInput)
	}
	in.ApplicationSources = normalized
	return in, nil
}

// Update changes one user's application and appends status history atomically.
// The row is locked BEFORE checking the same snapshot used by read.go. Locking
// the target vacancy serializes competing Go mutations of its application
// link, even though the imported MariaDB schema lacks the old unique index.
// Return value is the company name, so the caller can construct the complete
// legacy company response (applications + full vacancy drawer) after commit.
func (r Repository) Update(ctx context.Context, user string, id int64, input UpdateInput) (string, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" || id < 1 {
		return "", ErrInvalidInput
	}
	in, err := input.normalize()
	if err != nil {
		return "", err
	}
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var company, status, snapshot string
	err = tx.QueryRowContext(ctx, `SELECT a.company_name,a.status,`+recordSnapshotColumns+`
        FROM company_applications a WHERE a.id=? AND a.user_name=? FOR UPDATE`, id, user).
		Scan(&company, &status, &snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if Fingerprint(snapshot) != in.Revision {
		return "", ErrConflict
	}

	if in.VacancyID != nil {
		// Acquiring this canonical row lock serializes two Go writers that
		// would otherwise both pass a duplicate-link SELECT on this dump.
		var vacancyID int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM company_vacancies
            WHERE id=? AND (company_name=? OR real_company_name=?) FOR UPDATE`, *in.VacancyID, company, company).
			Scan(&vacancyID)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrInvalidVacancy
		}
		if err != nil {
			return "", err
		}
		var otherID int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM company_applications
            WHERE user_name=? AND vacancy_id=? AND id<>? LIMIT 1`, user, vacancyID, id).
			Scan(&otherID)
		if err == nil {
			return "", ErrDuplicateVacancy
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
	}
	sources, err := json.Marshal(in.ApplicationSources)
	if err != nil {
		return "", err
	}
	result, err := tx.ExecContext(ctx, `UPDATE company_applications a SET vacancy_id=?,status=?,comment_md=?,
        recruiter_name=?,recruiter_phone=?,recruiter_email=?,recruiter_contact_type=?,application_sources=?,
        updated_at=UTC_TIMESTAMP()
        WHERE a.user_name=? AND a.id=? AND `+recordSnapshotColumns+`=?`, in.VacancyID, in.Status, in.Comment,
		in.RecruiterName, in.RecruiterPhone, in.RecruiterEmail, in.RecruiterContactType, string(sources), user, id, snapshot)
	if err != nil {
		return "", err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if affected != 1 {
		return "", ErrConflict
	}
	if status != in.Status {
		if _, err = tx.ExecContext(ctx, `INSERT INTO company_application_status_history
            (application_id,from_status,to_status,changed_at) VALUES (?,?,?,UTC_TIMESTAMP())`, id, status, in.Status); err != nil {
			return "", err
		}
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return company, nil
}
