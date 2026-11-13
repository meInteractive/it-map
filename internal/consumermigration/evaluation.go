//go:build consumerevaluation

// Package consumermigration содержит только тестовую реализацию миграции.
package consumermigration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"regexp"
	"time"

	"itmap/internal/consumercontrol"
	"itmap/internal/dbschema"
)

var ErrBusy = errors.New("consumer migration schema lock busy")

var generationPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// InitializeForEvaluation выполняет DDL и запись только для испытания в
// disposable MariaDB. Обычная сборка этого пакета не содержит. Старые процессы,
// marker backup и production-готовность этим методом не подтверждаются.
// DDL не атомарен: после обрыва любая промежуточная схема отклоняется, без
// автоматического удаления или повторной инициализации. Перед реальной
// миграцией нужны операторский план и отдельное разрешение владельца.
func InitializeForEvaluation(ctx context.Context, db *sql.DB) (outcome error) {
	if ctx == nil || db == nil {
		return errors.New("consumer migration input invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return errors.New("consumer migration connection unavailable")
	}
	discard := func() { _ = conn.Raw(func(any) error { return driver.ErrBadConn }) }
	defer func() {
		if err := conn.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			outcome = errors.Join(outcome, errors.New("consumer migration connection cleanup failed"))
		}
	}()
	var database, version string
	var transaction, autocommit int
	if err = conn.QueryRowContext(ctx, "SELECT DATABASE(),VERSION(),@@in_transaction,@@autocommit").Scan(&database, &version, &transaction, &autocommit); err != nil || database == "" || transaction != 0 || autocommit != 1 {
		discard()
		return errors.New("consumer migration requires idle autocommit session")
	}
	if err = dbschema.CheckServerVersion(version); err != nil {
		return errors.New("consumer migration server unsupported")
	}
	hash := sha256.Sum256([]byte(database))
	lock := "itmap.restore.v39:" + hex.EncodeToString(hash[:16])
	var id int64
	var owner sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT CONNECTION_ID(),IS_USED_LOCK(?)", lock).Scan(&id, &owner); err != nil {
		discard()
		return errors.New("consumer migration lock unavailable")
	}
	if owner.Valid && owner.Int64 == id {
		discard()
		return errors.New("consumer migration session already owns lock")
	}
	var got sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", lock).Scan(&got); err != nil || !got.Valid || (got.Int64 != 0 && got.Int64 != 1) {
		discard()
		return errors.New("consumer migration lock unavailable")
	}
	if got.Int64 == 0 {
		return ErrBusy
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		var released sql.NullInt64
		if err := conn.QueryRowContext(cleanup, "SELECT RELEASE_LOCK(?)", lock).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			discard()
			outcome = errors.Join(outcome, errors.New("consumer migration lock cleanup failed"))
		}
	}()
	inspect := func() (dbschema.Snapshot, error) { return dbschema.Inspect(ctx, conn) }
	snapshot, err := inspect()
	if err != nil {
		return errors.New("consumer migration metadata unavailable")
	}
	validate := func(create bool) (result error) {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			discard()
			return errors.New("consumer migration transaction unavailable")
		}
		defer func() {
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				discard()
				result = errors.Join(result, errors.New("consumer migration rollback failed"))
			}
		}()
		var generation string
		if err = tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&generation); err != nil {
			return errors.New("consumer migration generation unavailable")
		}
		if create {
			var rows int
			if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM go_consumer_control").Scan(&rows); err != nil || rows != 0 {
				return errors.New("consumer migration control not empty")
			}
			if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM go_consumer_schema_version").Scan(&rows); err != nil || rows != 0 {
				return errors.New("consumer migration marker not empty")
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO go_consumer_control(id,generation,revision,mode,owner_digest,lease_until) VALUES(1,?,1,'legacy',NULL,0)", generation); err != nil {
				return errors.New("consumer migration control initialization failed")
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO go_consumer_schema_version(id,version,status) VALUES(1,1,'complete')"); err != nil {
				return errors.New("consumer migration marker initialization failed")
			}
		}
		if err = consumercontrol.GuardVersionedSingleton(ctx, tx, generation); err != nil {
			return errors.New("consumer migration state incompatible")
		}
		if create {
			if err = tx.Commit(); err != nil {
				discard()
				return errors.New("consumer migration commit outcome unknown")
			}
		}
		return nil
	}
	if dbschema.CheckVersionedConsumerOverlay(snapshot, dbschema.ConsumerSchemaVersion) == nil {
		return validate(false)
	}
	if dbschema.CheckFull(snapshot) != nil {
		return errors.New("consumer migration partial or incompatible schema")
	}
	// Перед первым неатомарным DDL отдельно убедиться в наличии generation.
	preflight, err := conn.BeginTx(ctx, nil)
	if err != nil {
		discard()
		return errors.New("consumer migration preflight unavailable")
	}
	var oldGeneration string
	err = preflight.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&oldGeneration)
	if rollback := preflight.Rollback(); rollback != nil {
		discard()
		return errors.New("consumer migration preflight cleanup failed")
	}
	if err != nil || !generationPattern.MatchString(oldGeneration) {
		return errors.New("consumer migration generation incompatible")
	}
	// DDL не транзакционен; не пытаться продолжать после ошибки или повторять
	// CREATE, пока оператор не исследовал получившееся состояние.
	if _, err = conn.ExecContext(ctx, `CREATE TABLE go_consumer_control (id BIGINT NOT NULL PRIMARY KEY, generation VARCHAR(36) NOT NULL, revision BIGINT NOT NULL, mode VARCHAR(16) NOT NULL, owner_digest VARBINARY(32) NULL, lease_until BIGINT NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`); err != nil {
		return errors.New("consumer migration control DDL outcome unknown")
	}
	if _, err = conn.ExecContext(ctx, `CREATE TABLE go_consumer_schema_version (id BIGINT NOT NULL PRIMARY KEY, version BIGINT NOT NULL, status VARCHAR(16) NOT NULL) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`); err != nil {
		return errors.New("consumer migration marker DDL outcome unknown")
	}
	snapshot, err = inspect()
	if err != nil || dbschema.CheckVersionedConsumerOverlay(snapshot, dbschema.ConsumerSchemaVersion) != nil {
		return errors.New("consumer migration created incompatible schema")
	}
	return validate(true)
}
