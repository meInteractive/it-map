package vacancies

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"itmap/internal/roles"
)

var (
	ErrDuplicateURL     = errors.New("vacancy URL already exists")
	ErrInvalidOwner     = errors.New("vacancy owner not in catalog")
	ErrInvalidRole      = errors.New("vacancy role is not a leaf or is missing")
	ErrInvalidChannel   = errors.New("vacancy Telegram channel is invalid")
	ErrChannelNotFound  = fmt.Errorf("%w: channel not found", ErrInvalidChannel)
	ErrDuplicateMessage = errors.New("Telegram message already belongs to a vacancy")
	ErrWriteBusy        = errors.New("vacancy write lock unavailable")
)

// CreateManual performs one atomic manual vacancy insert with its role, Telegram
// provenance and user relevance rows. This is a stage-1.1 bridge: MariaDB's
// converted dump lost some UNIQUE indexes. GET_LOCK serializes Go manual
// creates, but does NOT protect against other writers (e.g. the old parser).
// Before activating concurrent Go parsing, restore a reviewed DB constraint.
func (r Repository) CreateManual(ctx context.Context, user string, raw Input) ([]CompanyVacancy, error) {
	if r.DB == nil || strings.TrimSpace(user) == "" {
		return nil, errors.New("vacancy write unavailable")
	}
	in, err := NormalizeInput(raw, false)
	if err != nil {
		return nil, err
	}
	directory, err := (roles.Repository{DB: r.DB}).Read(ctx)
	if err != nil {
		return nil, err
	}
	roleIDs, label, err := resolveManualRoles(in, directory)
	if err != nil {
		return nil, err
	}
	in.Role = label

	conn, err := r.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	// All Go manual creates use the same connection-scoped lock, so two URLs
	// or Telegram messages cannot be inserted concurrently by this endpoint.
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

	if err = assertOwner(ctx, tx, in); err != nil {
		return nil, err
	}
	for _, id := range roleIDs {
		var found int64
		// Lock the role while inserting the link. Parent roles are not valid.
		err = tx.QueryRowContext(ctx, `SELECT r.id FROM vacancy_roles r WHERE r.id=? AND NOT EXISTS
            (SELECT 1 FROM vacancy_roles child WHERE child.parent_id=r.id) FOR UPDATE`, id).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidRole
		}
		if err != nil {
			return nil, err
		}
	}
	var dupe int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM company_vacancies WHERE url=? LIMIT 1`, in.URL).Scan(&dupe)
	if err == nil {
		return nil, ErrDuplicateURL
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	var telegramURL string
	if in.TelegramChannelID != nil {
		var username string
		err = tx.QueryRowContext(ctx, `SELECT normalized_username FROM telegram_channels WHERE id=? FOR UPDATE`, *in.TelegramChannelID).Scan(&username)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrChannelNotFound
		}
		if err != nil {
			return nil, err
		}
		telegramURL, err = CanonicalTelegramMessage(in.TelegramMessageURL, username)
		if err != nil {
			return nil, ErrInvalidChannel
		}
		err = tx.QueryRowContext(ctx, `SELECT vacancy_id FROM vacancy_telegram_sources WHERE message_url=? LIMIT 1`, telegramURL).Scan(&dupe)
		if err == nil {
			return nil, ErrDuplicateMessage
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	// The converted dump retains company_crm as the FK target; channel-only
	// pseudo-owners are legitimate but must be created only for known channels.
	if _, err = tx.ExecContext(ctx, `INSERT INTO company_crm(company_name) VALUES (?) ON DUPLICATE KEY UPDATE company_name=VALUES(company_name)`, in.CompanyName); err != nil {
		return nil, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO company_vacancies
        (company_name,entity_type,real_company_name,url,salary_from,salary_to,title,role,level,
         published_at,loaded_at,description_md,cover_letter_subject,cover_letter_md,created_at,updated_at)
        VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`,
		in.CompanyName, in.EntityType, nullText(in.RealCompanyName), in.URL, in.SalaryFrom, in.SalaryTo,
		in.Title, in.Role, in.Level, nullText(in.PublishedAt), nullText(in.LoadedAt), in.DescriptionMD,
		in.CoverLetterSubject, in.CoverLetterMD)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil || id < 1 {
		return nil, errors.New("vacancy insert did not return an ID")
	}
	for _, roleID := range roleIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO vacancy_role_assignments(vacancy_id,role_id) VALUES(?,?)`, id, roleID); err != nil {
			return nil, err
		}
	}
	if in.TelegramChannelID != nil {
		if _, err = tx.ExecContext(ctx, `INSERT INTO vacancy_telegram_sources(vacancy_id,channel_id,message_url,telegram_contact,created_at,updated_at)
            VALUES(?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, id, *in.TelegramChannelID, telegramURL, in.TelegramContact); err != nil {
			return nil, err
		}
	}
	if in.Relevance != nil {
		if _, err = tx.ExecContext(ctx, `INSERT INTO company_vacancy_user_state(user_name,vacancy_id,relevance,updated_at)
            VALUES(?,?,?,CURRENT_TIMESTAMP)`, user, id, *in.Relevance); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	release()
	// Return the pinned connection to the pool BEFORE the follow-up read.
	// Otherwise a pool with MaxOpenConns=1 deadlocks at this point.
	if err = conn.Close(); err != nil {
		return nil, err
	}
	// Preserve the legacy POST result {ok:true,vacancies:[...]}, including
	// user-specific fields. This read is deliberately AFTER commit.
	return r.CompanyVacancies(ctx, user, in.CompanyName)
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func assertOwner(ctx context.Context, tx *sql.Tx, in Input) error {
	var data []byte
	if err := tx.QueryRowContext(ctx, `SELECT data_json FROM app_catalog WHERE id=1`).Scan(&data); err != nil {
		return err
	}
	// Decode the two source lists explicitly: anonymous structs are not
	// addressable as reusable types in embedded JSON source fields.
	var parsed struct {
		Companies []struct {
			Name string `json:"name"`
		} `json:"companies"`
		Agencies []struct {
			Name string `json:"name"`
		} `json:"agencies"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	has := func(list []struct {
		Name string `json:"name"`
	}, name string) bool {
		for _, entry := range list {
			if entry.Name == name {
				return true
			}
		}
		return false
	}
	switch in.EntityType {
	case "company":
		if !has(parsed.Companies, in.CompanyName) {
			return ErrInvalidOwner
		}
	case "agency":
		if !has(parsed.Agencies, in.CompanyName) || (in.RealCompanyName != "" && !has(parsed.Companies, in.RealCompanyName)) {
			return ErrInvalidOwner
		}
	case "channel":
		if in.TelegramChannelID == nil {
			return ErrInvalidChannel
		}
		if in.CompanyName != "__telegram_channel__:"+fmt.Sprint(*in.TelegramChannelID) &&
			!has(parsed.Companies, in.CompanyName) && !has(parsed.Agencies, in.CompanyName) {
			return ErrInvalidOwner
		}
	default:
		return ErrInvalidOwner
	}
	return nil
}

func resolveManualRoles(in Input, catalog roles.Catalog) ([]int64, string, error) {
	chosen := in.RoleIDs
	byID := make(map[int64]roles.Role, len(catalog.Roles))
	byName := make(map[string]roles.Role, len(catalog.Roles))
	for _, role := range catalog.Roles {
		byID[role.ID] = role
		byName[strings.ToLower(role.Name)] = role
		byName[strings.ToLower(role.Path)] = role
	}
	if chosen == nil {
		for _, name := range strings.Split(in.Role, ";") {
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" {
				continue
			}
			if name == "менеджер продукта" {
				name = "менеджер продуктов"
			}
			role, ok := byName[name]
			if !ok {
				return nil, "", ErrInvalidRole
			}
			chosen = append(chosen, role.ID)
		}
	}
	uniq := make(map[int64]bool, len(chosen))
	ids := make([]int64, 0, len(chosen))
	names := make([]string, 0, len(chosen))
	for _, id := range chosen {
		role, ok := byID[id]
		if !ok || !role.IsLeaf {
			return nil, "", ErrInvalidRole
		}
		if !uniq[id] {
			uniq[id] = true
			ids = append(ids, id)
			names = append(names, role.Name)
		}
	}
	if len(ids) == 0 || len(ids) > 12 {
		return nil, "", ErrInvalidRole
	}
	return ids, strings.Join(names, "; "), nil
}
