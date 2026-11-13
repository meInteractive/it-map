// Package consumerpreflight проверяет только снимок candidate-схемы и singleton.
// Он не выполняет миграцию и не выдаёт разрешение на backup или запуск worker.
package consumerpreflight

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

var (
	ErrInput     = errors.New("consumer preflight input invalid")
	ErrStorage   = errors.New("consumer preflight storage unavailable")
	ErrSession   = errors.New("consumer preflight requires an idle autocommit session")
	ErrBusy      = errors.New("schema initialization or restore already active")
	ErrMetadata  = errors.New("consumer candidate metadata incompatible")
	ErrSingleton = errors.New("consumer singleton incompatible")
	ErrCleanup   = errors.New("consumer preflight cleanup failed")
)

func discard(conn *sql.Conn) { _ = conn.Raw(func(any) error { return driver.ErrBadConn }) }

var generationPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type schemaKind uint8

const (
	baselineSchema schemaKind = iota
	candidateSchema
	versionedSchema
)

// CheckBaseline диагностирует исходную схему и generation под блокировками.
// Это не доказательство migration marker или разрешение миграции и запуска.
func CheckBaseline(ctx context.Context, db *sql.DB, expectedGeneration string) error {
	return check(ctx, db, expectedGeneration, baselineSchema)
}

// CheckVersionedCandidate диагностирует полную схему с marker и singleton под
// advisory и row locks. Результат не разрешает миграцию, backup или worker.
func CheckVersionedCandidate(ctx context.Context, db *sql.DB, expectedGeneration string) error {
	return check(ctx, db, expectedGeneration, versionedSchema)
}

// CheckCandidate читает метаданные и singleton до и после row lock на одной
// транзакции. Advisory lock совпадает с offline schema-init и restore; он
// сериализует только участвующие процедуры и не запрещает произвольный DDL.
// Проверка не читает migration marker, не защищает состояние после возврата,
// не подтверждает backup/restore и не разрешает выполнять платную работу.
// Нет DDL, DML, создания строки или автоматического восстановления. В runtime
// и production-командах метод не подключён.
func CheckCandidate(ctx context.Context, db *sql.DB, expectedGeneration string) (result error) {
	return check(ctx, db, expectedGeneration, candidateSchema)
}

func check(ctx context.Context, db *sql.DB, expectedGeneration string, schema schemaKind) (result error) {
	if ctx == nil || db == nil || !generationPattern.MatchString(expectedGeneration) {
		return ErrInput
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return ErrStorage
	}
	defer func() {
		if err := conn.Close(); err != nil && !errors.Is(err, sql.ErrConnDone) {
			result = errors.Join(result, ErrCleanup)
		}
	}()
	var database string
	var transaction, autocommit int
	if err = conn.QueryRowContext(ctx, "SELECT DATABASE(),@@in_transaction,@@autocommit").Scan(&database, &transaction, &autocommit); err != nil {
		discard(conn)
		return ErrStorage
	}
	if database == "" || transaction != 0 || autocommit != 1 {
		discard(conn)
		return ErrSession
	}
	hash := sha256.Sum256([]byte(database))
	lock := "itmap.restore.v39:" + hex.EncodeToString(hash[:16])
	var connectionID int64
	var owner sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT CONNECTION_ID(), IS_USED_LOCK(?)", lock).Scan(&connectionID, &owner); err != nil {
		discard(conn)
		return ErrStorage
	}
	if owner.Valid && owner.Int64 == connectionID {
		// Повторный GET_LOCK увеличивает счётчик; такую сессию нельзя вернуть в pool.
		discard(conn)
		return ErrSession
	}
	var claimed sql.NullInt64
	if err = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", lock).Scan(&claimed); err != nil {
		// Ответ мог потеряться после получения lock; не возвращать эту сессию в pool.
		discard(conn)
		return ErrStorage
	}
	if !claimed.Valid {
		discard(conn)
		return ErrStorage
	}
	if claimed.Int64 == 0 {
		return ErrBusy
	}
	if claimed.Int64 != 1 {
		discard(conn)
		return ErrStorage
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		var released sql.NullInt64
		if err := conn.QueryRowContext(cleanup, "SELECT RELEASE_LOCK(?)", lock).Scan(&released); err != nil || !released.Valid || released.Int64 != 1 {
			discard(conn)
			result = errors.Join(result, ErrCleanup)
		}
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return ErrStorage
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			discard(conn)
			result = errors.Join(result, ErrCleanup)
		}
	}()
	checkMetadata := func() error {
		snapshot, err := dbschema.Inspect(ctx, tx)
		if err != nil {
			return ErrStorage
		}
		switch schema {
		case candidateSchema:
			err = dbschema.CheckConsumerOverlay(snapshot, dbschema.ConsumerOverlayVersion)
		case versionedSchema:
			err = dbschema.CheckVersionedConsumerOverlay(snapshot, dbschema.ConsumerSchemaVersion)
		default:
			err = dbschema.CheckFull(snapshot)
		}
		if err != nil {
			return ErrMetadata
		}
		return nil
	}
	if err = checkMetadata(); err != nil {
		return err
	}
	guard := func() error {
		if schema == versionedSchema {
			return consumercontrol.GuardVersionedSingleton(ctx, tx, expectedGeneration)
		}
		if schema == candidateSchema {
			return consumercontrol.GuardSingleton(ctx, tx, expectedGeneration)
		}
		var actual string
		if err := tx.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE").Scan(&actual); err != nil || actual != expectedGeneration {
			return ErrSingleton
		}
		return nil
	}
	if err = guard(); err != nil {
		return ErrSingleton
	}
	if err = checkMetadata(); err != nil {
		return err
	}
	if err = guard(); err != nil {
		return ErrSingleton
	}
	return nil
}
