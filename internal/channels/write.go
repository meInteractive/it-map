package channels

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"
)

var ErrNotFound = errors.New("channel not found")
var ErrWriteBusy = errors.New("channel write lock unavailable")
var ErrSoleSource = errors.New("Нельзя удалить канал: он является единственным источником одной или нескольких вакансий. Добавьте другой источник или перенесите вакансии в компанию/агентство.")

type CreateResult struct {
	OK        bool      `json:"ok"`
	ChannelID int64     `json:"channelId"`
	Channels  []Channel `json:"channels"`
}
type UpdateResult struct {
	OK       bool      `json:"ok"`
	Channels []Channel `json:"channels"`
}
type DeleteResult struct {
	OK                bool   `json:"ok"`
	ChannelID         int64  `json:"channelId"`
	Name              string `json:"name"`
	DetachedVacancies int    `json:"detachedVacancies"`
}

// beginWrite serializes all Go channel mutations. MariaDB's binary name index
// does not enforce the source ASCII LOWER precheck. It also prevents two channel
// deletes from both observing the other source and removing a vacancy's last
// sources. Existing vacancy editors coordinate via the parent row locks below.
func (r Repository) beginWrite(ctx context.Context) (*sql.Conn, func(), error) {
	if r.DB == nil {
		return nil, nil, errors.New("channel storage unavailable")
	}
	conn, e := r.DB.Conn(ctx)
	if e != nil {
		return nil, nil, e
	}
	var acquired sql.NullInt64
	if e = conn.QueryRowContext(ctx, `SELECT GET_LOCK(CONCAT('itmap:channel:',LEFT(SHA2(DATABASE(),256),32)),5)`).Scan(&acquired); e != nil || !acquired.Valid || acquired.Int64 != 1 {
		if e != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		conn.Close()
		if e != nil {
			return nil, nil, e
		}
		return nil, nil, ErrWriteBusy
	}
	release := func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		var released sql.NullInt64
		e := conn.QueryRowContext(cleanup, `SELECT RELEASE_LOCK(CONCAT('itmap:channel:',LEFT(SHA2(DATABASE(),256),32)))`).Scan(&released)
		if e != nil || !released.Valid || released.Int64 != 1 {
			// Returning a session still holding a named lock to the pool would
			// break serialization on reentrant GET_LOCK. Discard it physically.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
	}
	return conn, release, nil
}

func (r Repository) independent(ctx context.Context, tx *sql.Tx, input NormalizedInput, excluded *int64) error {
	if r.CatalogSources == nil {
		return errors.New("channel catalog policy unavailable")
	}
	var catalog []byte
	e := tx.QueryRowContext(ctx, `SELECT data_json FROM app_catalog WHERE id=1 FOR UPDATE`).Scan(&catalog)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	urls, e := r.CatalogSources(catalog)
	if e != nil {
		return e
	}
	for _, url := range urls {
		if NormalizeUsername(url) == input.NormalizedUsername {
			return &ValidationError{Message: "Этот Telegram-канал уже указан как источник компании или агентства."}
		}
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,name,normalized_username FROM telegram_channels ORDER BY id FOR UPDATE`)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, username string
		if e = rows.Scan(&id, &name, &username); e != nil {
			return e
		}
		if excluded != nil && id == *excluded {
			continue
		}
		if username == input.NormalizedUsername || asciiLower(name) == asciiLower(input.Name) {
			return &ValidationError{Message: "Такой независимый Telegram-канал уже добавлен."}
		}
	}
	return rows.Err()
}
func checkedValue(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func (r Repository) Create(ctx context.Context, raw Input) (CreateResult, error) {
	input, e := NormalizeInput(raw, nil)
	if e != nil {
		return CreateResult{}, e
	}
	conn, release, e := r.beginWrite(ctx)
	if e != nil {
		return CreateResult{}, e
	}
	defer release()
	tx, e := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return CreateResult{}, e
	}
	defer tx.Rollback()
	if e = r.independent(ctx, tx, input, nil); e != nil {
		return CreateResult{}, e
	}
	result, e := tx.ExecContext(ctx, `INSERT INTO telegram_channels(name,url,normalized_username,username,last_successful_check_at,comment,created_at,updated_at) VALUES(?,?,?,?,?,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, input.Name, input.URL, input.NormalizedUsername, input.Username, checkedValue(input.LastSuccessfulCheckAt), input.Comment)
	if e != nil {
		return CreateResult{}, e
	}
	id, e := result.LastInsertId()
	if e != nil || id <= 0 {
		return CreateResult{}, errors.New("channel insert returned no ID")
	}
	items, e := listChannels(ctx, tx)
	if e != nil {
		return CreateResult{}, e
	}
	if e = tx.Commit(); e != nil {
		return CreateResult{}, e
	}
	return CreateResult{OK: true, ChannelID: id, Channels: items}, nil
}
func readChannelForUpdate(ctx context.Context, tx *sql.Tx, id int64) (Channel, error) {
	var c Channel
	var checked sql.NullString
	e := tx.QueryRowContext(ctx, `SELECT id,name,url,username,normalized_username,last_successful_check_at,comment FROM telegram_channels WHERE id=? FOR UPDATE`, id).Scan(&c.ID, &c.Name, &c.URL, &c.Username, &c.NormalizedUsername, &checked, &c.Comment)
	if errors.Is(e, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if e != nil {
		return c, e
	}
	if checked.Valid {
		c.LastSuccessfulCheckAt = &checked.String
	}
	return c, nil
}
func (r Repository) Update(ctx context.Context, id int64, raw Input) (UpdateResult, error) {
	if id < 0 {
		return UpdateResult{}, ErrNotFound
	}
	conn, release, e := r.beginWrite(ctx)
	if e != nil {
		return UpdateResult{}, e
	}
	defer release()
	tx, e := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return UpdateResult{}, e
	}
	defer tx.Rollback()
	// Match create's catalog-before-channel lock order; the named lock also
	// serializes partial PUT fallback reads against other channel mutations.
	var ignored []byte
	e = tx.QueryRowContext(ctx, `SELECT data_json FROM app_catalog WHERE id=1 FOR UPDATE`).Scan(&ignored)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return UpdateResult{}, e
	}
	current, e := readChannelForUpdate(ctx, tx, id)
	if e != nil {
		return UpdateResult{}, e
	}
	input, e := NormalizeInput(raw, &current)
	if e != nil {
		return UpdateResult{}, e
	}
	if e = r.independent(ctx, tx, input, &id); e != nil {
		return UpdateResult{}, e
	}
	result, e := tx.ExecContext(ctx, `UPDATE telegram_channels SET name=?,url=?,normalized_username=?,username=?,last_successful_check_at=?,comment=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, input.Name, input.URL, input.NormalizedUsername, input.Username, checkedValue(input.LastSuccessfulCheckAt), input.Comment, id)
	if e != nil {
		return UpdateResult{}, e
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return UpdateResult{}, errors.New("channel update affected unexpected rows")
	}
	items, e := listChannels(ctx, tx)
	if e != nil {
		return UpdateResult{}, e
	}
	if e = tx.Commit(); e != nil {
		return UpdateResult{}, e
	}
	return UpdateResult{OK: true, Channels: items}, nil
}

func linkedVacancies(ctx context.Context, tx *sql.Tx, id int64) ([]int64, error) {
	rows, e := tx.QueryContext(ctx, `SELECT vacancy_id FROM vacancy_telegram_sources WHERE channel_id=? ORDER BY vacancy_id`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var v int64
		if e = rows.Scan(&v); e != nil {
			return nil, e
		}
		ids = append(ids, v)
	}
	return ids, rows.Err()
}

// deleteAttempt locks existing vacancy parents BEFORE the channel, matching
// manual vacancy edit's parent->channel order. A new FK link inserted during
// discovery causes a rollback/retry; after the channel lock no new link can
// enter. Parent locks fence owner/source edits, and the shared named lock fences
// deletion of another channel supplying the same vacancy.
func deleteAttempt(ctx context.Context, conn *sql.Conn, id int64) (DeleteResult, bool, error) {
	tx, e := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if e != nil {
		return DeleteResult{}, false, e
	}
	defer tx.Rollback()
	before, e := linkedVacancies(ctx, tx, id)
	if e != nil {
		return DeleteResult{}, false, e
	}
	locked := map[int64]bool{}
	for _, v := range before {
		if locked[v] {
			continue
		}
		var found int64
		e = tx.QueryRowContext(ctx, `SELECT id FROM company_vacancies WHERE id=? FOR UPDATE`, v).Scan(&found)
		if errors.Is(e, sql.ErrNoRows) {
			continue
		}
		if e != nil {
			return DeleteResult{}, false, e
		}
		locked[v] = true
	}
	current, e := readChannelForUpdate(ctx, tx, id)
	if e != nil {
		return DeleteResult{}, false, e
	}
	after, e := linkedVacancies(ctx, tx, id)
	if e != nil {
		return DeleteResult{}, false, e
	}
	for _, v := range after {
		if !locked[v] {
			return DeleteResult{}, true, nil
		}
	}
	result, e := tx.ExecContext(ctx, `DELETE FROM telegram_channels WHERE id=? AND NOT EXISTS (
 SELECT 1 FROM vacancy_telegram_sources source JOIN company_vacancies vacancy ON vacancy.id=source.vacancy_id
 WHERE source.channel_id=telegram_channels.id AND vacancy.entity_type='channel' AND NOT EXISTS (
 SELECT 1 FROM vacancy_telegram_sources other WHERE other.vacancy_id=vacancy.id AND other.channel_id<>telegram_channels.id))`, id)
	if e != nil {
		return DeleteResult{}, false, e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return DeleteResult{}, false, e
	}
	if n == 0 {
		return DeleteResult{}, false, ErrSoleSource
	}
	if n != 1 {
		return DeleteResult{}, false, errors.New("channel delete affected unexpected rows")
	}
	if e = tx.Commit(); e != nil {
		return DeleteResult{}, false, e
	}
	return DeleteResult{OK: true, ChannelID: id, Name: current.Name, DetachedVacancies: len(after)}, false, nil
}
func (r Repository) Delete(ctx context.Context, id int64) (DeleteResult, error) {
	if id < 0 {
		return DeleteResult{}, ErrNotFound
	}
	conn, release, e := r.beginWrite(ctx)
	if e != nil {
		return DeleteResult{}, e
	}
	defer release()
	for attempt := 0; attempt < 3; attempt++ {
		result, retry, e := deleteAttempt(ctx, conn, id)
		if !retry {
			return result, e
		}
	}
	return DeleteResult{}, ErrWriteBusy
}
