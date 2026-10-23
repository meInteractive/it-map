package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

// Settings deliberately does not contain the DB password. It is passed to
// Open only at startup, never printed, put in a URL, or written to a log.
type Settings struct {
	Host, Port, Database, User string
	MaxOpen, MaxIdle           int
}

// MakeDSN returns a go-sql-driver/mysql DSN for an isolated localhost database.
// The password is permitted to contain @ and : (the driver parses the final @).
// DSNs must NEVER be logged or placed in errors, since they contain secrets.
func MakeDSN(s Settings, password string) (string, error) {
	if s.Host != "127.0.0.1" && s.Host != "localhost" {
		return "", errors.New("DB must use localhost")
	}
	port, err := strconv.Atoi(s.Port)
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("invalid DB_PORT")
	}
	if !identifier.MatchString(s.Database) {
		return "", errors.New("invalid DB_NAME")
	}
	if s.User == "" || strings.ContainsAny(s.User, "@:/") {
		return "", errors.New("invalid DB_USER")
	}
	if password == "" {
		return "", errors.New("DB_PASSWORD must be provided")
	}
	params := url.Values{
		"charset": {"utf8mb4"},
		// UPDATE optimistic locking must distinguish matched rows from
		// physically changed rows when timestamp precision is one second.
		"clientFoundRows": {"true"},
		"collation":       {"utf8mb4_nopad_bin"},
		"parseTime":       {"true"},
		"multiStatements": {"false"},
		"timeout":         {"5s"},
		"readTimeout":     {"15s"},
		"writeTimeout":    {"15s"},
		"loc":             {"UTC"},
	}
	return fmt.Sprintf("%s:%s@tcp(%s)/%s?%s", s.User, password, net.JoinHostPort(s.Host, s.Port), s.Database, params.Encode()), nil
}

// Open creates a bounded pool and validates connectivity plus the schema contract.
// A missing mysql driver is an actionable build error, not a reason to fabricate success.
func Open(ctx context.Context, s Settings, password string) (*sql.DB, error) {
	dsn, err := MakeDSN(s, password)
	if err != nil {
		return nil, err
	}
	supported := false
	for _, name := range sql.Drivers() {
		if name == "mysql" {
			supported = true
			break
		}
	}
	if !supported {
		return nil, errors.New("mysql driver unavailable: compile with -tags mariadb")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, errors.New("initialize DB pool failed")
	} // DSN includes a secret.
	if s.MaxOpen <= 0 {
		s.MaxOpen = 10
	}
	if s.MaxIdle <= 0 {
		s.MaxIdle = 5
	}
	if s.MaxIdle > s.MaxOpen {
		s.MaxIdle = s.MaxOpen
	}
	db.SetMaxOpenConns(s.MaxOpen)
	db.SetMaxIdleConns(s.MaxIdle)
	db.SetConnMaxIdleTime(5 * time.Minute)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("could not connect to local DB (verify credentials and server availability)")
	}
	if err := ValidateBaseline(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// WithinTx ensures rollback on errors and panics, and commits only on success.
func WithinTx(ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
	if db == nil || fn == nil {
		return errors.New("transaction requires DB and callback")
	}
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
