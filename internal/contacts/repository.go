package contacts

import (
	"context"
	"database/sql"
	"errors"
)

var ErrNotFound = errors.New("contact not found")
var ErrCompanyNotFound = errors.New("contact company not found")

type Contact struct {
	ID             int64  `json:"id"`
	CompanyName    string `json:"companyName"`
	Position       int    `json:"position"`
	FullName       string `json:"fullName"`
	LinkedinURL    string `json:"linkedinUrl"`
	FacebookURL    string `json:"facebookUrl"`
	SetkaURL       string `json:"setkaUrl"`
	Telegram       string `json:"telegram"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	Specialization string `json:"specialization"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}
type Result struct {
	OK       bool      `json:"ok"`
	Contacts []Contact `json:"contacts"`
}
type Repository struct {
	DB           *sql.DB
	CatalogNames func([]byte) ([]string, error)
}

func (r Repository) known(ctx context.Context, tx *sql.Tx, name string, lock bool) error {
	if r.CatalogNames == nil {
		return errors.New("contact catalog policy unavailable")
	}
	query := "SELECT data_json FROM app_catalog WHERE id=1"
	if lock {
		query += " FOR UPDATE"
	}
	var data []byte
	e := tx.QueryRowContext(ctx, query).Scan(&data)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	names, e := r.CatalogNames(data)
	if e != nil {
		return e
	}
	for _, n := range names {
		if n == name && name != "" {
			return nil
		}
	}
	return ErrCompanyNotFound
}
func list(ctx context.Context, tx *sql.Tx, company string, lock bool) ([]Contact, error) {
	q := `SELECT id,company_name,position,full_name,linkedin_url,facebook_url,setka_url,telegram,phone,email,specialization,created_at,updated_at FROM hiring_contacts WHERE company_name=? ORDER BY position`
	if lock {
		q += " FOR UPDATE"
	}
	rows, e := tx.QueryContext(ctx, q, company)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Contact{}
	for rows.Next() {
		var c Contact
		if e = rows.Scan(&c.ID, &c.CompanyName, &c.Position, &c.FullName, &c.LinkedinURL, &c.FacebookURL, &c.SetkaURL, &c.Telegram, &c.Phone, &c.Email, &c.Specialization, &c.CreatedAt, &c.UpdatedAt); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (r Repository) List(ctx context.Context, name string) ([]Contact, error) {
	if r.DB == nil {
		return nil, errors.New("contact storage unavailable")
	}
	tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = r.known(ctx, tx, name, false); e != nil {
		return nil, e
	}
	out, e := list(ctx, tx, name, false)
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return out, nil
}
func lockOwner(ctx context.Context, tx *sql.Tx, company string, create bool) error {
	if create {
		if _, e := tx.ExecContext(ctx, `INSERT INTO company_crm(company_name) VALUES(?) ON DUPLICATE KEY UPDATE company_name=VALUES(company_name)`, company); e != nil {
			return e
		}
	}
	var name string
	return tx.QueryRowContext(ctx, `SELECT company_name FROM company_crm WHERE company_name=? FOR UPDATE`, company).Scan(&name)
}
func finish(ctx context.Context, tx *sql.Tx, company string) (Result, error) {
	out, e := list(ctx, tx, company, false)
	if e != nil {
		return Result{}, e
	}
	if e = tx.Commit(); e != nil {
		return Result{}, e
	}
	return Result{OK: true, Contacts: out}, nil
}
func positionAvailable(items []Contact, position int, exclude int64) error {
	for _, c := range items {
		if c.ID != exclude && c.Position == position {
			return &ValidationError{Message: "Эта позиция контакта уже занята."}
		}
	}
	return nil
}
func (r Repository) Create(ctx context.Context, company string, raw Input) (Result, error) {
	if r.DB == nil {
		return Result{}, errors.New("contact storage unavailable")
	}
	tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return Result{}, e
	}
	defer tx.Rollback()
	if e = r.known(ctx, tx, company, true); e != nil {
		return Result{}, e
	}
	in, e := NormalizeInput(raw, company, nil)
	if e != nil {
		return Result{}, e
	}
	if e = lockOwner(ctx, tx, company, true); e != nil {
		return Result{}, e
	}
	items, e := list(ctx, tx, company, true)
	if e != nil {
		return Result{}, e
	}
	if len(items) >= 3 {
		return Result{}, &ValidationError{Message: "У организации уже добавлены три контакта ответственных за найм."}
	}
	if e = positionAvailable(items, in.Position, -1); e != nil {
		return Result{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO hiring_contacts(company_name,position,full_name,linkedin_url,facebook_url,setka_url,telegram,phone,email,specialization,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, company, in.Position, in.FullName, in.LinkedinURL, in.FacebookURL, in.SetkaURL, in.Telegram, in.Phone, in.Email, in.Specialization); e != nil {
		return Result{}, e
	}
	return finish(ctx, tx, company)
}

// Discovery is followed by owner->contact locking, matching Create. Restore can
// replace an ID with another owner: recheck inside the transaction and retry.
func (r Repository) mutate(ctx context.Context, id int64, raw Input, remove bool) (Result, error) {
	if r.DB == nil {
		return Result{}, errors.New("contact storage unavailable")
	}
	if id < 0 {
		return Result{}, ErrNotFound
	}
	for attempt := 0; attempt < 3; attempt++ {
		out, retry, e := r.mutationAttempt(ctx, id, raw, remove)
		if !retry {
			return out, e
		}
	}
	return Result{}, errors.New("contact owner changed during discovery")
}
func (r Repository) mutationAttempt(ctx context.Context, id int64, raw Input, remove bool) (Result, bool, error) {
	tx, e := r.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return Result{}, false, e
	}
	defer tx.Rollback()
	var owner string
	e = tx.QueryRowContext(ctx, "SELECT company_name FROM hiring_contacts WHERE id=?", id).Scan(&owner)
	if errors.Is(e, sql.ErrNoRows) {
		return Result{}, false, ErrNotFound
	}
	if e != nil {
		return Result{}, false, e
	}
	if e = lockOwner(ctx, tx, owner, false); errors.Is(e, sql.ErrNoRows) {
		return Result{}, true, nil
	} else if e != nil {
		return Result{}, false, e
	}
	var currentOwner string
	var position int
	e = tx.QueryRowContext(ctx, "SELECT company_name,position FROM hiring_contacts WHERE id=? FOR UPDATE", id).Scan(&currentOwner, &position)
	if errors.Is(e, sql.ErrNoRows) {
		return Result{}, false, ErrNotFound
	}
	if e != nil {
		return Result{}, false, e
	}
	if owner != currentOwner {
		return Result{}, true, nil
	}
	if remove {
		_, e = tx.ExecContext(ctx, "DELETE FROM hiring_contacts WHERE id=?", id)
	} else {
		var in NormalizedInput
		in, e = NormalizeInput(raw, owner, &position)
		if e != nil {
			return Result{}, false, e
		}
		var items []Contact
		items, e = list(ctx, tx, owner, true)
		if e != nil {
			return Result{}, false, e
		}
		if e = positionAvailable(items, in.Position, id); e != nil {
			return Result{}, false, e
		}
		_, e = tx.ExecContext(ctx, `UPDATE hiring_contacts SET position=?,full_name=?,linkedin_url=?,facebook_url=?,setka_url=?,telegram=?,phone=?,email=?,specialization=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, in.Position, in.FullName, in.LinkedinURL, in.FacebookURL, in.SetkaURL, in.Telegram, in.Phone, in.Email, in.Specialization, id)
	}
	if e != nil {
		return Result{}, false, e
	}
	out, e := finish(ctx, tx, owner)
	return out, false, e
}
func (r Repository) Update(ctx context.Context, id int64, in Input) (Result, error) {
	return r.mutate(ctx, id, in, false)
}
func (r Repository) Delete(ctx context.Context, id int64) (Result, error) {
	return r.mutate(ctx, id, nil, true)
}
