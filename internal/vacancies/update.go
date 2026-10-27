package vacancies

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"itmap/internal/dbretry"
	"regexp"
	"strings"
	"time"

	"itmap/internal/roles"
)

var (
	ErrRevisionRequired = errors.New("current vacancy revision required")
	ErrVersionConflict  = errors.New("vacancy changed since detail was loaded")
	ErrLegacyDuplicates = errors.New("ambiguous legacy duplicate records")
)
var revisionFormat = regexp.MustCompile(`^[0-9a-f]{64}$`)

// UpdateRequest holds the whole form and the source's field-presence semantics.
// The original frontend omits letter fields when unchanged and omits the
// Telegram source when no channel is selected, preserving existing sources.
type UpdateRequest struct {
	Input
	Revision         string `json:"revision"`
	CoverLetterScope string `json:"coverLetterScope"`
	LetterProvided   bool   `json:"-"`
	TelegramProvided bool   `json:"-"`
}

func ParseUpdateRequest(data []byte) (UpdateRequest, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return UpdateRequest{}, ErrInvalidVacancyInput
	}
	var r UpdateRequest
	if err := json.Unmarshal(data, &r); err != nil {
		return UpdateRequest{}, ErrInvalidVacancyInput
	}
	_, sub := raw["coverLetterSubject"]
	_, md := raw["coverLetterMd"]
	r.LetterProvided = sub || md
	if sub != md {
		return UpdateRequest{}, ErrInvalidVacancyInput
	}
	if r.LetterProvided {
		if r.CoverLetterScope != "shared" && r.CoverLetterScope != "personal" {
			return UpdateRequest{}, ErrInvalidVacancyInput
		}
		if string(raw["coverLetterSubject"]) == "null" || string(raw["coverLetterMd"]) == "null" {
			return UpdateRequest{}, ErrInvalidVacancyInput
		}
	}
	_, r.TelegramProvided = raw["telegramChannelId"]
	if r.TelegramProvided && string(raw["telegramChannelId"]) == "null" {
		r.Input.TelegramChannelID = nil
	}
	return r, nil
}

// ValidateRevision returns HTTP 428 for missing or malformed versions.
func ValidateRevision(revision string) error {
	if !revisionFormat.MatchString(revision) {
		return ErrRevisionRequired
	}
	return nil
}

// UpdateManual writes a whole vacancy form using exactly the revision supplied
// by GET /api/vacancies/{id}. All checks and writes happen under an InnoDB row
// lock in one transaction. GET_LOCK serializes URL changes against the Go POST
// endpoint until a reviewed unique constraint can be restored in MariaDB.
// Editing agency vacancies is guarded until agency salary projection is ported.
func (r Repository) UpdateManual(ctx context.Context, user string, id int64, req UpdateRequest) ([]CompanyVacancy, error) {
	return dbretry.Run(ctx, func() ([]CompanyVacancy, error) { return r.updateManualOnce(ctx, user, id, req) })
}

func (r Repository) updateManualOnce(ctx context.Context, user string, id int64, req UpdateRequest) ([]CompanyVacancy, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" || id <= 0 {
		return nil, errors.New("vacancy update unavailable")
	}
	if err := ValidateRevision(req.Revision); err != nil {
		return nil, err
	}
	catalog, err := (roles.Repository{DB: r.DB}).Read(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := r.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var acquired sql.NullInt64
	if err = conn.QueryRowContext(ctx, `SELECT GET_LOCK('itmap:manual-vacancy:create', 5)`).Scan(&acquired); err != nil {
		return nil, err
	}
	if !acquired.Valid || acquired.Int64 != 1 {
		return nil, ErrWriteBusy
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(releaseCtx, `DO RELEASE_LOCK('itmap:manual-vacancy:create')`)
	}
	defer release()
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var locked int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM company_vacancies WHERE id=? FOR UPDATE`, id).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	current, err := readDetailTx(ctx, tx, user, id, catalog)
	if err != nil {
		return nil, err
	}
	if current.Vacancy.Revision != req.Revision {
		return nil, ErrVersionConflict
	}
	v := current.Vacancy
	// The form always includes the owner, but preserve backwards compatibility
	// with clients that omit it during an edit.
	if req.CompanyName == "" {
		req.CompanyName = v.CompanyName
	}
	if req.EntityType == "" {
		req.EntityType = v.EntityType
	}
	if req.RoleIDs == nil && req.Role == "" && len(v.Roles) != 0 {
		req.RoleIDs = make([]int64, 0, len(v.Roles))
		for _, role := range v.Roles {
			req.RoleIDs = append(req.RoleIDs, role.ID)
		}
	}
	if req.Role == "" && req.RoleIDs == nil {
		req.Role = v.Role
	}
	if !req.TelegramProvided && len(v.TelegramSources) > 0 {
		src := v.TelegramSources[0]
		req.TelegramChannelID = &src.ChannelID
		req.TelegramMessageURL = src.MessageURL
		req.TelegramContact = src.TelegramContact
	}
	if req.LetterProvided {
		if req.CoverLetterScope == "personal" && (strings.TrimSpace(req.CoverLetterSubject) == "") != (strings.TrimSpace(req.CoverLetterMD) == "") {
			return nil, ErrInvalidVacancyInput
		}
	} else {
		// The effective letter in the drawer can be personal. Never assign it
		// to the shared record when unrelated form fields are edited.
		req.CoverLetterSubject = v.CoverLetterSubject
		req.CoverLetterMD = v.CoverLetterMD
	}
	in, err := NormalizeInput(req.Input, false)
	if err != nil {
		return nil, err
	}
	if v.EntityType == "agency" || in.EntityType == "agency" {
		return nil, ErrAgencySalaryProjectionPending
	}
	if req.TelegramProvided && req.TelegramChannelID == nil && in.EntityType == "channel" {
		return nil, ErrInvalidChannel
	}
	if err = assertOwner(ctx, tx, in); err != nil {
		return nil, err
	}
	roleIDs, roleLabel, err := resolveManualRoles(in, catalog)
	if err != nil {
		return nil, err
	}
	for _, roleID := range roleIDs {
		var leaf int64
		err = tx.QueryRowContext(ctx, `SELECT r.id FROM vacancy_roles r WHERE r.id=? AND NOT EXISTS (SELECT 1 FROM vacancy_roles child WHERE child.parent_id=r.id) FOR UPDATE`, roleID).Scan(&leaf)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidRole
		}
		if err != nil {
			return nil, err
		}
	}
	var dupe int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM company_vacancies WHERE url=? AND id<>? LIMIT 1`, in.URL, id).Scan(&dupe)
	if err == nil {
		return nil, ErrDuplicateURL
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// Keep the channel source attached to this vacancy; only the requested
	// channel is modified. Other imported Telegram sources remain unchanged.
	var telegramURL string
	if req.TelegramProvided && in.TelegramChannelID != nil {
		var username string
		err = tx.QueryRowContext(ctx, `SELECT normalized_username FROM telegram_channels WHERE id=? FOR UPDATE`, *in.TelegramChannelID).Scan(&username)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidChannel
		}
		if err != nil {
			return nil, err
		}
		telegramURL, err = CanonicalTelegramMessage(in.TelegramMessageURL, username)
		if err != nil {
			return nil, ErrInvalidChannel
		}
		err = tx.QueryRowContext(ctx, `SELECT vacancy_id FROM vacancy_telegram_sources WHERE message_url=? AND vacancy_id<>? LIMIT 1`, telegramURL, id).Scan(&dupe)
		if err == nil {
			return nil, ErrDuplicateMessage
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	personalSubject, personalMD := in.CoverLetterSubject, in.CoverLetterMD
	var oldSharedSubject, oldSharedMD string
	err = tx.QueryRowContext(ctx, `SELECT cover_letter_subject,cover_letter_md FROM company_vacancies WHERE id=?`, id).Scan(&oldSharedSubject, &oldSharedMD)
	if err != nil {
		return nil, err
	}
	if !req.LetterProvided || req.CoverLetterScope != "shared" {
		in.CoverLetterSubject = oldSharedSubject
		in.CoverLetterMD = oldSharedMD
	}
	if in.CompanyName != v.CompanyName {
		// A catalog entry may have no CRM row until its first mutation. Create
		// the reviewed FK parent in this transaction before moving the vacancy
		// and its application/task links. Preserve existing global CRM fields.
		if _, err = tx.ExecContext(ctx, `INSERT INTO company_crm(company_name) VALUES(?) ON DUPLICATE KEY UPDATE company_name=VALUES(company_name)`, in.CompanyName); err != nil {
			return nil, err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE company_vacancies SET company_name=?,entity_type=?,real_company_name=?,url=?,salary_from=?,salary_to=?,title=?,role=?,level=?,published_at=?,loaded_at=?,description_md=?,cover_letter_subject=?,cover_letter_md=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`,
		in.CompanyName, in.EntityType, nullText(in.RealCompanyName), in.URL, in.SalaryFrom, in.SalaryTo, in.Title, roleLabel, in.Level, nullText(in.PublishedAt), nullText(in.LoadedAt), in.DescriptionMD, in.CoverLetterSubject, in.CoverLetterMD, id)
	if err != nil {
		return nil, err
	}
	if count, e := result.RowsAffected(); e != nil || count != 1 {
		return nil, errors.New("vacancy update affected unexpected number of rows")
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM vacancy_role_assignments WHERE vacancy_id=?`, id); err != nil {
		return nil, err
	}
	for _, roleID := range roleIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO vacancy_role_assignments(vacancy_id,role_id) VALUES(?,?)`, id, roleID); err != nil {
			return nil, err
		}
	}
	if req.LetterProvided && req.CoverLetterScope == "personal" {
		var count int64
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM vacancy_cover_letters WHERE user_name=? AND vacancy_id=?`, user, id).Scan(&count); err != nil {
			return nil, err
		}
		if count > 1 {
			return nil, ErrLegacyDuplicates
		}
		if personalMD == "" {
			if _, err = tx.ExecContext(ctx, `DELETE FROM vacancy_cover_letters WHERE user_name=? AND vacancy_id=?`, user, id); err != nil {
				return nil, err
			}
		} else if count == 1 {
			if _, err = tx.ExecContext(ctx, `UPDATE vacancy_cover_letters SET subject=?,markdown=?,revision=revision+1 WHERE user_name=? AND vacancy_id=?`, personalSubject, personalMD, user, id); err != nil {
				return nil, err
			}
		} else {
			if _, err = tx.ExecContext(ctx, `INSERT INTO vacancy_cover_letters(user_name,vacancy_id,subject,markdown,override_shared,revision) VALUES(?,?,?,?,1,1)`, user, id, personalSubject, personalMD); err != nil {
				return nil, err
			}
		}
	}
	if in.CompanyName != v.CompanyName {
		if _, err = tx.ExecContext(ctx, `UPDATE company_applications SET company_name=?,updated_at=CURRENT_TIMESTAMP WHERE vacancy_id=?`, in.CompanyName, id); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE company_tasks SET company_name=? WHERE vacancy_id=?`, in.CompanyName, id); err != nil {
			return nil, err
		}
	}
	if req.TelegramProvided {
		if in.TelegramChannelID == nil {
			if _, err = tx.ExecContext(ctx, `DELETE FROM vacancy_telegram_sources WHERE vacancy_id=?`, id); err != nil {
				return nil, err
			}
		} else {
			var matches int64
			if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM vacancy_telegram_sources WHERE vacancy_id=? AND channel_id=?`, id, *in.TelegramChannelID).Scan(&matches); err != nil {
				return nil, err
			}
			if matches > 1 {
				return nil, ErrLegacyDuplicates
			}
			if matches == 1 {
				_, err = tx.ExecContext(ctx, `UPDATE vacancy_telegram_sources SET message_url=?,telegram_contact=?,updated_at=CURRENT_TIMESTAMP WHERE vacancy_id=? AND channel_id=?`, telegramURL, in.TelegramContact, id, *in.TelegramChannelID)
			} else {
				_, err = tx.ExecContext(ctx, `INSERT INTO vacancy_telegram_sources(vacancy_id,channel_id,message_url,telegram_contact,created_at,updated_at) VALUES(?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, id, *in.TelegramChannelID, telegramURL, in.TelegramContact)
			}
			if err != nil {
				return nil, err
			}
		}
	}
	// Update all duplicate legacy user-state rows, preserving favorite/hidden.
	var stateCount int64
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM company_vacancy_user_state WHERE user_name=? AND vacancy_id=?`, user, id).Scan(&stateCount); err != nil {
		return nil, err
	}
	if stateCount > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE company_vacancy_user_state SET relevance=?,updated_at=CURRENT_TIMESTAMP WHERE user_name=? AND vacancy_id=?`, in.Relevance, user, id)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,updated_at) VALUES(?,?,?,CURRENT_TIMESTAMP)`, user, id, in.Relevance)
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	release()
	if err = conn.Close(); err != nil {
		return nil, err
	}
	// The old editor expects the updated company's whole vacancy list.
	return r.CompanyVacancies(ctx, user, in.CompanyName)
}
