package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"itmap/internal/v39"
)

var ErrRestoreBusy = errors.New("another restore owns this database")

// RestoreSession owns one connection and the database restore advisory lock.
// Counter DDL finishes before this handle is returned. No data TX is started.
// A later restore must finish its data TX before closing the session; Close
// never issues UNLOCK TABLES, which could implicitly commit that future TX.
type RestoreSession struct {
	conn        *sql.Conn
	plan        *RestorePlan
	lock        string
	owned       bool
	sqlMode     string
	modeChanged bool
	done        chan struct{}
	once        sync.Once
	closeErr    error
}

func (s *RestoreSession) Close() error {
	s.once.Do(func() {
		close(s.done)
		if s.owned {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			var released sql.NullInt64
			if e := s.conn.QueryRowContext(ctx, `SELECT RELEASE_LOCK(?)`, s.lock).Scan(&released); e != nil || !released.Valid || released.Int64 != 1 {
				s.closeErr = errors.New("cannot release restore ownership")
				discardRestoreConnection(s.conn)
			}
		}
		if s.modeChanged {
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			if _, e := s.conn.ExecContext(cleanup, "SET SESSION sql_mode=?", s.sqlMode); e != nil {
				s.closeErr = errors.Join(s.closeErr, errors.New("cannot reset restore SQL mode"))
				discardRestoreConnection(s.conn)
			}
			cancel()
		}
		if e := s.conn.Close(); e != nil && !errors.Is(e, sql.ErrConnDone) {
			s.closeErr = errors.Join(s.closeErr, e)
		}
	})
	return s.closeErr
}
func discardRestoreConnection(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
}

type counterTarget struct {
	table, column string
	next          uint64
}

func restoreCounterTargets(plan *RestorePlan) ([]counterTarget, error) {
	trusted := v39.Trusted()
	auto := map[string]bool{}
	for _, o := range trusted.Objects {
		if o.Type == "table" && strings.Contains(strings.ToUpper(o.SQL), "AUTOINCREMENT") {
			auto[o.Name] = true
		}
	}
	next := map[string]uint64{}
	for _, s := range plan.Sequences {
		if !auto[s.Name] || s.Seq < 0 || s.Seq > maxSafeInteger || s.Seq != math.Trunc(s.Seq) {
			return nil, invalid("Некорректные счётчики идентификаторов.")
		}
		next[s.Name] = uint64(s.Seq) + 1
	}
	byName := map[string]Table{}
	for _, t := range plan.Tables {
		byName[t.Name] = t
	}
	targets := []counterTarget{}
	for _, t := range trusted.Tables {
		if !auto[t.Name] {
			continue
		}
		column := ""
		for _, c := range t.Columns {
			if c.PrimaryKey && c.Type == "INTEGER" {
				column = c.Name
			}
		}
		if column == "" {
			return nil, errors.New("trusted auto counter has no integer key")
		}
		wanted := next[t.Name]
		if wanted < 1 {
			wanted = 1
		}
		for _, row := range byName[t.Name].Rows {
			var id float64
			if e := json.Unmarshal(row[column], &id); e != nil || string(row[column]) == "null" || id != math.Trunc(id) || math.Abs(id) > maxSafeInteger {
				return nil, invalid("Непредставимый ID в бэкапе.")
			}
			if id >= 0 && uint64(id)+1 > wanted {
				wanted = uint64(id) + 1
			}
		}
		targets = append(targets, counterTarget{t.Name, column, wanted})
	}
	return targets, nil
}
func advanceRestoreCounter(ctx context.Context, conn *sql.Conn, target counterTarget) (err error) {
	// Re-read metadata while WRITE-locked: read→ALTER without this fence can
	// overwrite a concurrent allocation with a lower AUTO_INCREMENT value.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if _, e := conn.ExecContext(cleanup, "UNLOCK TABLES"); e != nil {
			discardRestoreConnection(conn)
			err = errors.Join(err, errors.New("cannot release counter table lock"))
		}
	}()
	if _, err = conn.ExecContext(ctx, "LOCK TABLES "+quote(target.table)+" WRITE"); err != nil {
		return err
	}
	var next sql.NullString
	if err = conn.QueryRowContext(ctx, `SELECT AUTO_INCREMENT FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?`, target.table).Scan(&next); err != nil {
		return err
	}
	n, e := strconv.ParseUint(next.String, 10, 64)
	if e != nil || !next.Valid || n < 1 || n > maxSafeInteger+1 {
		return invalid("Непредставимый счётчик назначения.")
	}
	var max sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT MAX("+quote(target.column)+") FROM "+quote(target.table)).Scan(&max); err != nil {
		return err
	}
	wanted := target.next
	if n > wanted {
		wanted = n
	}
	if max.Valid && max.Int64 >= 0 {
		if max.Int64 > maxSafeInteger {
			return invalid("Непредставимый ID назначения.")
		}
		if uint64(max.Int64)+1 > wanted {
			wanted = uint64(max.Int64) + 1
		}
	}
	if wanted > n {
		_, err = conn.ExecContext(ctx, "ALTER TABLE "+quote(target.table)+" AUTO_INCREMENT = "+strconv.FormatUint(wanted, 10))
	}
	return err
}

// OpenV39RestoreSession validates the signed v39 file and a complete prepared
// schema, exclusively claims restore ownership, then conservatively raises ID
// counters outside any data TX. Failures can leave only safe counter gaps.
// Business rows/generation are untouched. Runtime HTTP is not wired to it yet.
func (r Repository) OpenV39RestoreSession(ctx context.Context, data []byte) (_ *RestoreSession, err error) {
	plan, err := PrepareV39Restore(data)
	if err != nil {
		return nil, err
	}
	if err = validateRestoreScalars(plan); err != nil {
		return nil, err
	}
	targets, err := restoreCounterTargets(plan)
	if err != nil {
		return nil, err
	}
	if r.DB == nil {
		return nil, errors.New("restore storage unavailable")
	}
	conn, err := r.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	s := &RestoreSession{conn: conn, plan: plan, done: make(chan struct{})}
	defer func() {
		if err != nil {
			_ = s.Close()
		}
	}()
	var database string
	var transaction int
	if err = conn.QueryRowContext(ctx, `SELECT DATABASE(),@@in_transaction`).Scan(&database, &transaction); err != nil {
		return nil, err
	}
	if database == "" || transaction != 0 {
		return nil, errors.New("restore preflight requires an idle connection")
	}
	hash := sha256.Sum256([]byte(database))
	s.lock = "itmap.restore.v39:" + hex.EncodeToString(hash[:16])
	var claimed sql.NullInt64
	if err = conn.QueryRowContext(ctx, `SELECT GET_LOCK(?,0)`, s.lock).Scan(&claimed); err != nil {
		return nil, err
	}
	if !claimed.Valid || claimed.Int64 != 1 {
		return nil, ErrRestoreBusy
	}
	s.owned = true
	if _, err = completeRestoreInventory(ctx, conn); err != nil {
		return nil, err
	}
	for _, target := range targets {
		if target.next > 1 {
			if err = advanceRestoreCounter(ctx, conn, target); err != nil {
				return nil, err
			}
		}
	}
	// Explicit zero IDs are legal in SQLite and must not allocate a new ID.
	// Preserve the original session setting when this dedicated handle closes.
	if err = conn.QueryRowContext(ctx, "SELECT @@sql_mode").Scan(&s.sqlMode); err != nil {
		return nil, err
	}
	s.modeChanged = true
	if _, err = conn.ExecContext(ctx, "SET SESSION sql_mode=?", s.sqlMode+",NO_AUTO_VALUE_ON_ZERO"); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	go func() {
		select {
		case <-ctx.Done():
			_ = s.Close()
		case <-s.done:
		}
	}()
	return s, nil
}
