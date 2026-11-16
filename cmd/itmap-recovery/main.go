// Command itmap-recovery inspects or explicitly reconciles known Apify runs.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"itmap/internal/apifyruns"
	"itmap/internal/apifysettings"
	"itmap/internal/storage"
)

const maxOperations = 10

var errUnavailable = errors.New("recovery unavailable")

type candidate struct {
	id, profile string
	charged     sql.NullFloat64
	record      apifyruns.Record
}
type recoveryBackend interface {
	List(context.Context) ([]candidate, error)
	Inspect(context.Context, candidate) error
	Apply(context.Context, candidate) error
	Close() error
}
type factory func(context.Context, func(string) string) (recoveryBackend, error)

func run(ctx context.Context, args []string, getenv func(string) string, open factory, out io.Writer) int {
	if ctx == nil || getenv == nil || open == nil || out == nil || len(args) != 1 || (args[0] != "inspect" && args[0] != "apply") || getenv("ITMAP_RECOVERY_ENABLED") != "1" {
		return 2
	}
	if args[0] == "apply" && (getenv("ITMAP_RECOVERY_APPLY") != "1" || getenv("ITMAP_RECOVERY_QUIESCED") != "1") {
		fmt.Fprintln(out, "recovery_apply_not_enabled")
		return 2
	}
	if ctx.Err() != nil {
		fmt.Fprintln(out, "recovery_interrupted")
		return 1
	}
	backend, err := open(ctx, getenv)
	if err != nil || backend == nil {
		fmt.Fprintln(out, "recovery_unavailable")
		return 1
	}
	defer backend.Close()
	rows, err := backend.List(ctx)
	if err != nil {
		fmt.Fprintln(out, "recovery_unavailable")
		return 1
	}
	if len(rows) > maxOperations {
		fmt.Fprintln(out, "recovery_limit_exceeded")
		return 1
	}
	confirmed, blocked := 0, 0
	for _, row := range rows {
		if ctx.Err() != nil {
			fmt.Fprintln(out, "recovery_interrupted")
			return 1
		}
		if args[0] == "inspect" {
			err = backend.Inspect(ctx, row)
		} else {
			err = backend.Apply(ctx, row)
		}
		if errors.Is(err, apifyruns.ErrRecoveryCommitUnknown) {
			fmt.Fprintf(out, "confirmed=%d blocked=%d commit_unknown=1\n", confirmed, blocked)
			return 1
		}
		if err != nil {
			blocked++
		} else {
			confirmed++
		}
		if ctx.Err() != nil {
			fmt.Fprintf(out, "confirmed=%d blocked=%d interrupted=1\n", confirmed, blocked)
			return 1
		}
	}
	fmt.Fprintf(out, "confirmed=%d blocked=%d commit_unknown=0\n", confirmed, blocked)
	if blocked > 0 {
		return 1
	}
	return 0
}

type sqlBackend struct {
	db            *sql.DB
	generation    string
	settings      apifysettings.Repository
	externalToken string
}

func openBackend(ctx context.Context, getenv func(string) string) (recoveryBackend, error) {
	if len(getenv("SESSION_SECRET")) < 32 {
		return nil, errUnavailable
	}
	cfg := storage.Settings{Host: getenv("DB_HOST"), Port: getenv("DB_PORT"), Database: getenv("DB_NAME"), User: getenv("DB_USER")}
	boot, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	db, err := storage.Open(boot, cfg, getenv("DB_PASSWORD"))
	if err != nil {
		return nil, errUnavailable
	}
	settings := apifysettings.NewRepository(db, getenv("SESSION_SECRET"))
	generation, err := settings.Generation(boot)
	if err != nil {
		db.Close()
		return nil, errUnavailable
	}
	return &sqlBackend{db: db, generation: generation, settings: settings, externalToken: getenv("ITMAP_RECOVERY_EXTERNAL_TOKEN")}, nil
}
func (b *sqlBackend) Close() error { return b.db.Close() }
func (b *sqlBackend) current(ctx context.Context) bool {
	generation, err := b.settings.Generation(ctx)
	return err == nil && generation == b.generation
}
func (b *sqlBackend) List(ctx context.Context) ([]candidate, error) {
	if !b.current(ctx) {
		return nil, errUnavailable
	}
	rows, err := b.db.QueryContext(ctx, `SELECT id,profile,state,run_id,dataset_id,account_id,charged_usd FROM apify_runs WHERE generation=? AND (charged_usd IS NULL OR state IN ('starting','running','uncertain') OR BINARY run_id NOT REGEXP BINARY '^[A-Za-z0-9]+$' OR BINARY dataset_id NOT REGEXP BINARY '^[A-Za-z0-9]+$' OR BINARY account_id NOT REGEXP BINARY '^[A-Za-z0-9]+$' OR id='') ORDER BY id LIMIT 11`, b.generation)
	if err != nil {
		return nil, errUnavailable
	}
	defer rows.Close()
	result := []candidate{}
	for rows.Next() {
		var c candidate
		if rows.Scan(&c.id, &c.profile, &c.record.State, &c.record.RunID, &c.record.DatasetID, &c.record.AccountID, &c.charged) != nil {
			return nil, errUnavailable
		}
		c.record.ID = c.id
		result = append(result, c)
	}
	if rows.Err() != nil {
		return nil, errUnavailable
	}
	if err := rows.Close(); err != nil || !b.current(ctx) {
		return nil, errUnavailable
	}
	return result, nil
}
func (b *sqlBackend) Inspect(ctx context.Context, c candidate) error {
	if c.charged.Valid || !b.current(ctx) {
		return errUnavailable
	}
	resolve := b.settings.Resolve
	if b.externalToken != "" {
		resolve = func(context.Context, string) (string, error) { return b.externalToken, nil }
	}
	reader := apifyruns.RecoveryReader{Resolve: resolve}
	_, err := reader.Verify(ctx, c.record, c.profile)
	if err == nil && !b.current(ctx) {
		return errUnavailable
	}
	return err
}
func (b *sqlBackend) Apply(ctx context.Context, c candidate) error {
	service := apifyruns.Service{Repository: apifyruns.Repository{DB: b.db, Settings: b.settings}}
	if b.externalToken != "" {
		return service.ReconcileKnownRecoveryWithToken(ctx, b.generation, c.id, b.externalToken)
	}
	return service.ReconcileKnownRecovery(ctx, b.generation, c.id)
}
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, openBackend, os.Stdout))
}
