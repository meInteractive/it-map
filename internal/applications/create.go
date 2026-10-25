package applications

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ErrCreationContextRequired is returned when a request is missing either the
// opaque, generation-bound context or its client-generated idempotency key.
var (
	ErrCreationContextRequired = errors.New("application creation context required")
	ErrCreationContextExpired  = errors.New("application creation context expired")
	ErrIntentConflict          = errors.New("idempotency key was used for different application data")
	ErrIncompleteReceipt       = errors.New("application receipt is incomplete")
	ErrApplicationDeleted      = errors.New("created application has been deleted")
	ErrApplicationLimit        = errors.New("application limit reached")
	ErrGenerationUnavailable   = errors.New("runtime generation not initialized")
	ErrVacancyOwnerChanged     = errors.New("vacancy owner changed")
	ErrShortcutVacancyMissing  = errors.New("shortcut vacancy missing")
	ErrAmbiguousVacancyLink    = errors.New("multiple applications linked to the same vacancy")
)

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var contextTokenPattern = regexp.MustCompile(`^ac1\.[A-Za-z0-9_-]{43}$`)

// CreateInput mirrors the v316 POST /api/applications/create payload.
// The request hash does not include either the key or creation context.
type CreateInput struct {
	CompanyName          string   `json:"companyName"`
	VacancyID            *int64   `json:"vacancyId"`
	Status               string   `json:"status"`
	Comment              string   `json:"comment"`
	RecruiterName        string   `json:"recruiterName"`
	RecruiterPhone       string   `json:"recruiterPhone"`
	RecruiterEmail       string   `json:"recruiterEmail"`
	RecruiterContactType string   `json:"recruiterContactType"`
	ApplicationSources   []string `json:"applicationSources"`
	IdempotencyKey       string   `json:"idempotencyKey"`
	CreationContext      string   `json:"creationContext"`
}

type CreationResult struct {
	CompanyResponse
	CreatedApplicationID int64 `json:"createdApplicationId"`
	Replayed             bool  `json:"replayed"`
}

func (in CreateInput) normalize() (CreateInput, error) {
	in.CompanyName = trimLegacyText(in.CompanyName)
	if in.CompanyName == "" || !uuidV4.MatchString(in.IdempotencyKey) {
		return in, ErrInvalidInput
	}
	normalized, err := (UpdateInput{Revision: strings.Repeat("0", 64), VacancyID: in.VacancyID, Status: in.Status, Comment: in.Comment, RecruiterName: in.RecruiterName, RecruiterPhone: in.RecruiterPhone, RecruiterEmail: in.RecruiterEmail, RecruiterContactType: in.RecruiterContactType, ApplicationSources: in.ApplicationSources}).normalize()
	if err != nil {
		return in, ErrInvalidInput
	}
	in.VacancyID = normalized.VacancyID
	in.RecruiterName = normalized.RecruiterName
	in.RecruiterPhone = normalized.RecruiterPhone
	in.RecruiterEmail = normalized.RecruiterEmail
	in.RecruiterContactType = normalized.RecruiterContactType
	in.ApplicationSources = normalized.ApplicationSources
	sort.Strings(in.ApplicationSources)
	return in, nil
}

// jsJSON preserves JSON.stringify's non-HTML-escaped representation. Hashing the
// same ordered array as Sites allows retries across a blue/green cutover.
func jsJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	data := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	// Go escapes U+2028/U+2029 whereas JavaScript JSON.stringify preserves
	// these characters. Receipts created on Sites must hash identically.
	data = bytes.ReplaceAll(data, []byte(`\u2028`), []byte("\u2028"))
	data = bytes.ReplaceAll(data, []byte(`\u2029`), []byte("\u2029"))
	return data, nil
}
func (in CreateInput) intentHash() (string, error) {
	data, err := jsJSON([]any{in.CompanyName, in.VacancyID, in.Status, in.Comment, in.RecruiterName, in.RecruiterPhone, in.RecruiterEmail, in.RecruiterContactType, in.ApplicationSources})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum), nil
}
func (r Repository) signature(user, generation string) (string, error) {
	if len(r.CreationSecret) == 0 || user == "" {
		return "", ErrGenerationUnavailable
	}
	payload, err := jsJSON([]string{user, generation})
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, r.CreationSecret)
	_, _ = mac.Write([]byte("application-create-context:v1:"))
	_, _ = mac.Write(payload)
	return "ac1." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func (r Repository) verifyContext(user, generation, token string) error {
	if token == "" {
		return ErrCreationContextRequired
	}
	if !contextTokenPattern.MatchString(token) {
		return ErrInvalidInput
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "ac1."))
	if err != nil || base64.RawURLEncoding.EncodeToString(decoded) != strings.TrimPrefix(token, "ac1.") {
		return ErrInvalidInput
	}
	expected, err := r.signature(user, generation)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(expected), []byte(token)) {
		return ErrCreationContextExpired
	}
	return nil
}
func (r Repository) generation(ctx context.Context) (string, error) {
	if r.DB == nil {
		return "", ErrGenerationUnavailable
	}
	var generation string
	err := r.DB.QueryRowContext(ctx, `SELECT generation FROM app_runtime_generation WHERE id=1`).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrGenerationUnavailable
	}
	if err != nil {
		return "", err
	}
	if !uuidV4.MatchString(generation) {
		return "", ErrGenerationUnavailable
	}
	return generation, nil
}

func (r Repository) CreationContext(ctx context.Context, user string) (string, error) {
	if strings.TrimSpace(user) == "" {
		return "", ErrInvalidInput
	}
	gen, err := r.generation(ctx)
	if err != nil {
		return "", err
	}
	token, err := r.signature(user, gen)
	if err != nil {
		return "", err
	}
	// A restore between signing and returning invalidates a newly issued token.
	after, err := r.generation(ctx)
	if err != nil {
		return "", err
	}
	if after != gen {
		return "", ErrCreationContextExpired
	}
	return token, nil
}

// CreateWithReceipt creates an application and durable idempotency receipt in
// the SAME transaction. The singleton generation row is locked first: it
// serializes creation and database restore generation-rotation across Go nodes.
// An existing receipt is never discarded or automatically re-used after deletion.
func (r Repository) CreateWithReceipt(ctx context.Context, user string, raw CreateInput) (CreationResult, error) {
	return r.createWithReceipt(ctx, user, raw, false)
}

// createWithReceipt optionally enables the vacancy-card shortcut. A shortcut
// can claim an existing user/vacancy application without modifying that record
// or its status history. Both modes share the exact same signed intent receipts.
func (r Repository) createWithReceipt(ctx context.Context, user string, raw CreateInput, shortcut bool) (CreationResult, error) {
	if r.DB == nil || user == "" {
		return CreationResult{}, ErrInvalidInput
	}
	if raw.IdempotencyKey == "" || raw.CreationContext == "" {
		return CreationResult{}, ErrCreationContextRequired
	}
	in, err := raw.normalize()
	if err != nil {
		return CreationResult{}, err
	}
	hash, err := in.intentHash()
	if err != nil {
		return CreationResult{}, err
	}
	gen, err := r.generation(ctx)
	if err != nil {
		return CreationResult{}, err
	}
	if err = r.verifyContext(user, gen, in.CreationContext); err != nil {
		return CreationResult{}, err
	}

	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return CreationResult{}, err
	}
	defer tx.Rollback()
	var lockedGen string
	if err = tx.QueryRowContext(ctx, `SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE`).Scan(&lockedGen); err != nil {
		return CreationResult{}, err
	}
	if lockedGen != gen {
		return CreationResult{}, ErrCreationContextExpired
	}
	var savedHash, savedGen string
	var receiptID sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT generation,request_hash,created_application_id FROM application_creation_receipts WHERE user_name=? AND idempotency_key=? FOR UPDATE`, user, in.IdempotencyKey).Scan(&savedGen, &savedHash, &receiptID)
	if err == nil {
		if savedGen != gen {
			return CreationResult{}, ErrCreationContextExpired
		}
		if savedHash != hash {
			return CreationResult{}, ErrIntentConflict
		}
		if !receiptID.Valid || receiptID.Int64 < 1 {
			return CreationResult{}, ErrIncompleteReceipt
		}
		var present int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM company_applications WHERE id=? AND user_name=?`, receiptID.Int64, user).Scan(&present)
		if errors.Is(err, sql.ErrNoRows) {
			return CreationResult{}, ErrApplicationDeleted
		}
		if err != nil {
			return CreationResult{}, err
		}
		if err = tx.Commit(); err != nil {
			return CreationResult{}, err
		}
		return r.finishCreateForMode(ctx, user, in.CompanyName, gen, present, true, shortcut)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CreationResult{}, err
	}

	// Ordinary CRM creation only accepts organizations in the catalog. The
	// vacancy-card shortcut also supports channel-owned vacancies, using the
	// locked vacancy's exact owner as its authority.
	if !shortcut {
		var catalogJSON []byte
		if err = tx.QueryRowContext(ctx, `SELECT data_json FROM app_catalog WHERE id=1`).Scan(&catalogJSON); err != nil {
			return CreationResult{}, err
		}
		known, catalogErr := isKnownForCreation(catalogJSON, in.CompanyName)
		if catalogErr != nil {
			return CreationResult{}, catalogErr
		}
		if !known {
			return CreationResult{}, ErrCompanyNotFound
		}
	}
	// The CRM primary-key row serializes count/insert across all Go writers for
	// a company. A database UNIQUE (user,vacancy) constraint is still needed
	// before running a second implementation of the writer concurrently.
	if _, err = tx.ExecContext(ctx, `INSERT IGNORE INTO company_crm(company_name) VALUES(?)`, in.CompanyName); err != nil {
		return CreationResult{}, err
	}
	var name string
	if err = tx.QueryRowContext(ctx, `SELECT company_name FROM company_crm WHERE company_name=? FOR UPDATE`, in.CompanyName).Scan(&name); err != nil {
		return CreationResult{}, err
	}
	// Locking order is generation -> company_crm -> vacancy, matching normal
	// creation and avoiding a lock-order inversion on concurrent writes.
	if shortcut {
		if in.VacancyID == nil {
			return CreationResult{}, ErrInvalidInput
		}
		var owner string
		err = tx.QueryRowContext(ctx, `SELECT company_name FROM company_vacancies WHERE id=? FOR UPDATE`, *in.VacancyID).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) {
			return CreationResult{}, ErrShortcutVacancyMissing
		}
		if err != nil {
			return CreationResult{}, err
		}
		if owner != in.CompanyName {
			return CreationResult{}, ErrVacancyOwnerChanged
		}
		// The imported schema does not guarantee uniqueness. Never silently
		// claim an arbitrary one of multiple linked applications.
		var linkedCount, linkedID int64
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MIN(id),0) FROM company_applications WHERE user_name=? AND vacancy_id=?`, user, *in.VacancyID).Scan(&linkedCount, &linkedID); err != nil {
			return CreationResult{}, err
		}
		if linkedCount > 1 {
			return CreationResult{}, ErrAmbiguousVacancyLink
		}
		if linkedCount == 1 {
			if _, err = tx.ExecContext(ctx, `INSERT INTO application_creation_receipts(user_name,idempotency_key,generation,request_hash,created_application_id) VALUES(?,?,?,?,?)`, user, in.IdempotencyKey, gen, hash, linkedID); err != nil {
				return CreationResult{}, err
			}
			if err = tx.Commit(); err != nil {
				return CreationResult{}, err
			}
			return r.finishCreateForMode(ctx, user, in.CompanyName, gen, linkedID, false, true)
		}
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM company_applications WHERE user_name=? AND company_name=?`, user, in.CompanyName).Scan(&count); err != nil {
		return CreationResult{}, err
	}
	if count >= 10 {
		return CreationResult{}, ErrApplicationLimit
	}
	if in.VacancyID != nil {
		var id int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM company_vacancies WHERE id=? AND (company_name=? OR real_company_name=?) FOR UPDATE`, *in.VacancyID, in.CompanyName, in.CompanyName).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return CreationResult{}, ErrInvalidVacancy
		}
		if err != nil {
			return CreationResult{}, err
		}
		var linked int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM company_applications WHERE user_name=? AND vacancy_id=? LIMIT 1`, user, id).Scan(&linked)
		if err == nil {
			return CreationResult{}, ErrDuplicateVacancy
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return CreationResult{}, err
		}
	}
	sources, err := json.Marshal(in.ApplicationSources)
	if err != nil {
		return CreationResult{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO company_applications(user_name,company_name,vacancy_id,status,comment_md,recruiter_name,recruiter_phone,recruiter_email,recruiter_contact_type,application_sources) VALUES(?,?,?,?,?,?,?,?,?,?)`, user, in.CompanyName, in.VacancyID, in.Status, in.Comment, in.RecruiterName, in.RecruiterPhone, in.RecruiterEmail, in.RecruiterContactType, string(sources))
	if err != nil {
		return CreationResult{}, err
	}
	id, err := result.LastInsertId()
	if err != nil || id < 1 {
		return CreationResult{}, ErrIncompleteReceipt
	}
	if in.Status != "" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO company_application_status_history(application_id,from_status,to_status) VALUES(?,'',?)`, id, in.Status); err != nil {
			return CreationResult{}, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO application_creation_receipts(user_name,idempotency_key,generation,request_hash,created_application_id) VALUES(?,?,?,?,?)`, user, in.IdempotencyKey, gen, hash, id); err != nil {
		return CreationResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return CreationResult{}, err
	}
	return r.finishCreateForMode(ctx, user, in.CompanyName, gen, id, false, shortcut)
}
func isKnownForCreation(data []byte, name string) (bool, error) {
	// Explicitly typed fields preserve the original companies/agencies JSON names.
	var groups struct {
		Companies []struct {
			Name string `json:"name"`
		} `json:"companies"`
		Agencies []struct {
			Name string `json:"name"`
		} `json:"agencies"`
	}
	if err := json.Unmarshal(data, &groups); err != nil {
		return false, err
	}
	if groups.Companies == nil || groups.Agencies == nil {
		return false, errors.New("catalog incomplete")
	}
	for _, v := range groups.Companies {
		if v.Name == name {
			return true, nil
		}
	}
	for _, v := range groups.Agencies {
		if v.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// finishCreate verifies the generation and durable identity again AFTER the
// response is read. Restoring a backup must never report a pre-restore write
// as a fresh success if the old receipt has disappeared.
func (r Repository) finishCreate(ctx context.Context, user, company, generation string, id int64, replayed bool) (CreationResult, error) {
	before, err := r.generation(ctx)
	if err != nil {
		return CreationResult{}, err
	}
	if before != generation {
		return CreationResult{}, ErrCreationContextExpired
	}
	response, err := r.CompanyApplications(ctx, user, company)
	if err != nil {
		return CreationResult{}, err
	}
	after, err := r.generation(ctx)
	if err != nil {
		return CreationResult{}, err
	}
	if after != generation {
		return CreationResult{}, ErrCreationContextExpired
	}
	var exists int64
	err = r.DB.QueryRowContext(ctx, `SELECT id FROM company_applications WHERE id=? AND user_name=?`, id, user).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return CreationResult{}, ErrApplicationDeleted
	}
	if err != nil {
		return CreationResult{}, err
	}
	return CreationResult{CompanyResponse: response, CreatedApplicationID: exists, Replayed: replayed}, nil
}
