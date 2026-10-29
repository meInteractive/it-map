package dbschema

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"
)

var ErrBusy = errors.New("schema initialization or restore already active")
var ErrPopulated = errors.New("partial schema contains rows or allocated IDs; initialization refused")
var mariaVersion = regexp.MustCompile(`(?:^|-)([0-9]+)\.([0-9]+)\.([0-9]+)-MariaDB`)

type InitResult struct {
	Version int  `json:"version"`
	Applied int  `json:"applied"`
	Ready   bool `json:"ready"`
	Seeded  bool `json:"seeded"`
}

func supportedVersion(version string) bool {
	return mariaAtLeast(version, 10, 3, 32)
}

func mariaAtLeast(version string, major, minor, patch int) bool {
	match := mariaVersion.FindStringSubmatch(version)
	if len(match) != 4 {
		return false
	}
	for i, required := range []int{major, minor, patch} {
		actual, e := strconv.Atoi(match[i+1])
		if e != nil {
			return false
		}
		if actual != required {
			return actual > required
		}
	}
	return true
}
func discard(conn *sql.Conn) { _ = conn.Raw(func(any) error { return driver.ErrBadConn }) }

// Initialize is an explicit offline administrative operation. No HTTP startup
// or restore calls it automatically. It creates only trusted missing objects in
// an empty/empty-partial database, never drops/rewrites objects or business rows.
// Every DDL statement commits independently; a failed pass must be re-inspected
// on the next invocation. Generation is seeded only AFTER exact full schema.
func Initialize(ctx context.Context, db *sql.DB) (result InitResult, err error) {
	if len(Trusted().Metadata) == 0 {
		return result, ErrUnverified
	}
	if db == nil {
		return result, errors.New("schema initialization storage unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	conn, e := db.Conn(ctx)
	if e != nil {
		return result, errors.New("cannot acquire schema initialization connection")
	}
	defer conn.Close()
	var database, version string
	var transaction, autocommit int
	if e = conn.QueryRowContext(ctx, "SELECT DATABASE(),VERSION(),@@in_transaction,@@autocommit").Scan(&database, &version, &transaction, &autocommit); e != nil {
		return result, errors.New("cannot inspect schema initialization session")
	}
	if database == "" || transaction != 0 || autocommit != 1 {
		return result, errors.New("schema initialization requires an idle autocommit connection")
	}
	if !supportedVersion(version) {
		return result, errors.New("schema initialization requires MariaDB 10.3.32 or newer")
	}
	hash := sha256.Sum256([]byte(database))
	// Same ownership key as backup.RestoreSession: metadata/DDL initialization
	// cannot overlap another initializer or a prepared-schema restore.
	lock := "itmap.restore.v39:" + hex.EncodeToString(hash[:16])
	var claimed sql.NullInt64
	if e = conn.QueryRowContext(ctx, "SELECT GET_LOCK(?,0)", lock).Scan(&claimed); e != nil {
		return result, errors.New("cannot acquire schema initialization ownership")
	}
	if !claimed.Valid || claimed.Int64 != 1 {
		return result, ErrBusy
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var released sql.NullInt64
		if e := conn.QueryRowContext(cleanup, "SELECT RELEASE_LOCK(?)", lock).Scan(&released); e != nil || !released.Valid || released.Int64 != 1 {
			discard(conn)
			err = errors.Join(err, errors.New("cannot release schema initialization ownership"))
		}
	}()
	snapshot, e := Inspect(ctx, conn)
	if e != nil {
		return result, e
	}
	actions, e := Preflight(snapshot)
	if e != nil {
		return result, e
	}
	if len(actions) > 0 {
		if e = emptyTables(ctx, conn, snapshot); e != nil {
			return result, e
		}
		var mode, zone, client, results, collation string
		if e = conn.QueryRowContext(ctx, "SELECT @@sql_mode,@@time_zone,@@character_set_client,@@character_set_results,@@collation_connection").Scan(&mode, &zone, &client, &results, &collation); e != nil {
			return result, errors.New("cannot inspect schema initialization SQL mode")
		}
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			for _, s := range []struct{ q, value string }{{"SET SESSION sql_mode=?", mode}, {"SET SESSION time_zone=?", zone}, {"SET SESSION character_set_client=?", client}, {"SET SESSION character_set_results=?", results}, {"SET SESSION collation_connection=?", collation}} {
				if _, e := conn.ExecContext(cleanup, s.q, s.value); e != nil {
					discard(conn)
					err = errors.Join(err, errors.New("cannot reset schema initialization session"))
					break
				}
			}
		}()
		for _, q := range []string{"SET SESSION sql_mode='STRICT_ALL_TABLES,NO_AUTO_VALUE_ON_ZERO'", "SET SESSION time_zone='+00:00'", "SET NAMES utf8mb4 COLLATE utf8mb4_nopad_bin"} {
			if _, e = conn.ExecContext(ctx, q); e != nil {
				return result, errors.New("cannot prepare schema initialization session")
			}
		}
		var fk, checks int
		if e = conn.QueryRowContext(ctx, "SELECT @@foreign_key_checks,@@check_constraint_checks").Scan(&fk, &checks); e != nil || fk != 1 || checks != 1 {
			return result, errors.New("schema initialization requires enabled FK and CHECK enforcement")
		}
		for _, a := range actions {
			// Present tables are rechecked before each step. An unexpected writer can
			// stop initialization, but never causes a DROP, data rewrite or row loss.
			if e = emptyTables(ctx, conn, snapshot); e != nil {
				return result, e
			}
			if _, e = conn.ExecContext(ctx, a.SQL); e != nil {
				return result, errors.New("trusted schema DDL failed; inspect and retry initialization")
			}
			result.Applied++
			if a.Kind == "table" {
				name := a.Table
				snapshot["tables"] = append(snapshot["tables"], []*string{&name})
			}
		}
	}
	snapshot, e = Inspect(ctx, conn)
	if e != nil {
		return result, e
	}
	if e = CheckFull(snapshot); e != nil {
		return result, e
	}
	var generation string
	e = conn.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation)
	if errors.Is(e, sql.ErrNoRows) {
		// A complete but populated database without generation is not implicitly
		// converted into a new generation. An operator must resolve it separately.
		if e = emptyTables(ctx, conn, snapshot); e != nil {
			return result, e
		}
		var uuid [16]byte
		if _, e = rand.Read(uuid[:]); e != nil {
			return result, errors.New("cannot generate initial database generation")
		}
		uuid[6] = (uuid[6] & 15) | 64
		uuid[8] = (uuid[8] & 63) | 128
		generation = fmt.Sprintf("%x-%x-%x-%x-%x", uuid[:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:])
		tx, e := conn.BeginTx(ctx, nil)
		if e != nil {
			return result, errors.New("cannot start generation initialization")
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO app_runtime_generation(id,generation) VALUES(1,?)", generation); e != nil {
			tx.Rollback()
			return result, errors.New("cannot initialize database generation")
		}
		if e = tx.Commit(); e != nil {
			return result, errors.New("generation initialization commit failed; inspect and retry")
		}
		result.Seeded = true
	} else if e != nil {
		return result, errors.New("cannot inspect initial database generation")
	} else if utf8.RuneCountInString(generation) != 36 {
		return result, errors.New("invalid existing database generation")
	}
	result.Version = 1
	result.Ready = true
	return result, nil
}
func emptyTables(ctx context.Context, conn *sql.Conn, snapshot Snapshot) error {
	// Empty rows alone cannot prove a pristine partial bootstrap: previously
	// allocated/deleted IDs must not be reused by a rebuilding DDL statement.
	var allocated int
	if e := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND AUTO_INCREMENT IS NOT NULL AND AUTO_INCREMENT<>1").Scan(&allocated); e != nil {
		return errors.New("cannot verify pristine schema counters")
	}
	if allocated != 0 {
		return ErrPopulated
	}

	// Names came from a strict metadata preflight or a trusted CREATE action.
	for _, row := range snapshot["tables"] {
		name := cell(row, 0)
		var found int
		q := "SELECT 1 FROM `" + name + "` LIMIT 1"
		e := conn.QueryRowContext(ctx, q).Scan(&found)
		if e == nil {
			return ErrPopulated
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return errors.New("cannot verify empty schema before initialization")
		}
	}
	return nil
}
