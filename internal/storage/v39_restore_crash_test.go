//go:build mariadb && v39restore && restorehttp

package storage

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	mysql "github.com/go-sql-driver/mysql"
	"io"
	"itmap/internal/backup"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

const restoreCrashMarker = "ITMAP_RESTORE_BEFORE_COMMIT"

type restoreCrashConnector struct{ driver.Connector }

func (c restoreCrashConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, e := c.Connector.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &restoreCrashConn{Conn: conn}, nil
}

type restoreCrashConn struct{ driver.Conn }

func (c *restoreCrashConn) QueryContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, a)
}
func (c *restoreCrashConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	return c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, q)
}
func (c *restoreCrashConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	tx, e := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, o)
	if e != nil {
		return nil, e
	}
	return &restoreCrashTx{Tx: tx, ctx: ctx}, nil
}

type restoreCrashTx struct {
	driver.Tx
	ctx context.Context
}

func (tx *restoreCrashTx) Commit() error {
	fmt.Fprintln(os.Stdout, restoreCrashMarker)
	<-tx.ctx.Done()
	_ = tx.Tx.Rollback()
	return tx.ctx.Err()
}
func restoreCrashWorker(t *testing.T, mode string) {
	if os.Getenv("GITHUB_ACTIONS") != "true" || (mode != "before_commit" && mode != "recover") {
		t.Fatal("invalid disposable restore worker")
	}
	ctx, guard := openDisposableMariaDB(t)
	body, e := io.ReadAll(io.LimitReader(os.Stdin, backup.MaxBytes+1))
	if e != nil || len(body) > backup.MaxBytes {
		t.Fatal("invalid synthetic worker input")
	}
	repo := backup.Repository{DB: guard}
	if mode == "before_commit" {
		cfg, e := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
		if e != nil {
			t.Fatal("worker config failed")
		}
		connector, e := mysql.NewConnector(cfg)
		if e != nil {
			t.Fatal("worker connector failed")
		}
		db := sql.OpenDB(restoreCrashConnector{Connector: connector})
		defer db.Close()
		repo.DB = db
	}
	if _, e = repo.RestoreV39(ctx, body); e != nil {
		t.Fatalf("worker restore failed: %T", e)
	}
	if mode != "recover" {
		t.Fatal("writer returned before kill")
	}
	fmt.Fprintln(os.Stdout, "ITMAP_RESTORE_RECOVERED")
}
func TestMariaDBV39RestoreCrashDisposable(t *testing.T) {
	if mode := os.Getenv("ITMAP_TEST_RESTORE_WORKER"); mode != "" {
		restoreCrashWorker(t, mode)
		return
	}
	f := openRestoreFixture(t)
	f.exec("INSERT INTO user_profiles(user_name,resume_md) VALUES('CI old owner','CI before crash 😀')")
	before, gen := f.snapshot(), f.generation()
	command := func(ctx context.Context, mode string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMariaDBV39RestoreCrashDisposable$", "-test.count=1", "-test.timeout=12s")
		cmd.Env = append(os.Environ(), "ITMAP_TEST_RESTORE_WORKER="+mode)
		cmd.Stdin = bytes.NewReader(f.body)
		return cmd
	}
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	cmd := command(ctx, "before_commit")
	stdout, e := cmd.StdoutPipe()
	if e != nil || cmd.Start() != nil {
		t.Fatal("cannot start restore writer")
	}
	waited := false
	defer func() {
		if !waited {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	reached := make(chan bool, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		for scan.Scan() {
			if scan.Text() == restoreCrashMarker {
				reached <- true
				return
			}
		}
		reached <- false
	}()
	select {
	case ok := <-reached:
		if !ok {
			t.Fatal("restore writer missed commit barrier")
		}
	case <-ctx.Done():
		t.Fatal("restore writer barrier timed out")
	}
	if e = cmd.Process.Kill(); e != nil {
		t.Fatal("cannot kill restore writer")
	}
	e = cmd.Wait()
	waited = true
	var exit *exec.ExitError
	if !errors.As(e, &exit) || ctx.Err() != nil || cmd.ProcessState.Success() {
		t.Fatal("expected SIGKILL before COMMIT")
	}
	if gen != f.generation() || !reflect.DeepEqual(before, f.snapshot()) {
		t.Fatal("killed writer changed data or generation")
	}
	recovery, cancelRecovery := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancelRecovery()
	out, e := command(recovery, "recover").CombinedOutput()
	if e != nil || !strings.Contains(string(out), "ITMAP_RESTORE_RECOVERED") {
		t.Fatal("fresh process did not recover restore")
	}
	if gen == f.generation() || !reflect.DeepEqual(f.snapshot(), restoreRecords(t, f.expected)) {
		t.Fatal("fresh process restore differs")
	}
}

func (c *restoreCrashConn) ExecContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, a)
}
