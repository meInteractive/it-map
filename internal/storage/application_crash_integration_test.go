//go:build mariadb && receiptwrites

package storage

import (
	"bufio"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/applications"
)

const receiptCrashMarker = "ITMAP_RECEIPT_CRASH_BARRIER"
const receiptRecoveryMarker = "ITMAP_RECEIPT_RECOVERED"

// Only the test executable gets this driver wrapper. Real repository writes
// reach the real driver unchanged; the barrier stops at the COMMIT boundary.
type receiptCrashConnector struct {
	driver.Connector
	phase string
}

func (c receiptCrashConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &receiptCrashConn{Conn: conn, phase: c.phase}, nil
}

type receiptCrashConn struct {
	driver.Conn
	phase string
}

func (c *receiptCrashConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}
func (c *receiptCrashConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	return c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, q)
}
func (c *receiptCrashConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &receiptCrashTx{Tx: tx, phase: c.phase, ctx: ctx}, nil
}

type receiptCrashTx struct {
	driver.Tx
	phase string
	ctx   context.Context
}

func (tx *receiptCrashTx) Commit() error {
	if tx.phase == "before_commit" {
		fmt.Fprintln(os.Stdout, receiptCrashMarker)
		<-tx.ctx.Done()
		tx.Tx.Rollback()
		return tx.ctx.Err()
	}
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, receiptCrashMarker)
	<-tx.ctx.Done()
	return tx.ctx.Err()
}

func receiptCrashWorker(t *testing.T, mode string) {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		t.Fatal("receipt crash worker requires disposable GitHub CI")
	}
	if mode != "before_commit" && mode != "after_commit" && mode != "recover" {
		t.Fatal("invalid receipt crash worker mode")
	}
	ctx, guardedDB := openDisposableMariaDB(t)
	token := os.Getenv("ITMAP_TEST_RECEIPT_WORKER_CONTEXT")
	if token == "" {
		t.Fatal("receipt crash worker requires the original context")
	}
	repo := applications.Repository{DB: guardedDB, CreationSecret: []byte(strings.Repeat("c", 64))}
	if mode != "recover" {
		cfg, err := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
		if err != nil {
			t.Fatal("cannot configure guarded crash connection")
		}
		connector, err := mysql.NewConnector(cfg)
		if err != nil {
			t.Fatal("cannot initialize guarded crash connector")
		}
		db := sql.OpenDB(receiptCrashConnector{Connector: connector, phase: mode})
		t.Cleanup(func() { db.Close() })
		repo.DB = db
	}
	result, err := repo.CreateWithReceipt(ctx, receiptUser, receiptInput(token))
	if err != nil {
		t.Fatalf("receipt worker operation failed: %T", err)
	}
	if mode != "recover" {
		t.Fatal("crash worker returned before being killed")
	}
	fmt.Fprintf(os.Stdout, "%s %d %t\n", receiptRecoveryMarker, result.CreatedApplicationID, result.Replayed)
}

func (f *receiptFixture) crashCommand(ctx context.Context, mode string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMariaDBApplicationCrashDisposable$", "-test.count=1", "-test.timeout=12s")
	cmd.Env = append(os.Environ(), "ITMAP_TEST_RECEIPT_WORKER_MODE="+mode,
		"ITMAP_TEST_RECEIPT_WORKER_CONTEXT="+f.input.CreationContext)
	return cmd
}

func (f *receiptFixture) killWriter(phase string) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	cmd := f.crashCommand(ctx, phase)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		f.t.Fatal("cannot observe crash worker barrier")
	}
	if err := cmd.Start(); err != nil {
		f.t.Fatal("cannot start synthetic crash writer")
	}
	waited := false
	defer func() {
		if !waited {
			cmd.Process.Kill()
			cmd.Wait()
		}
	}()
	reached := make(chan bool, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		for scan.Scan() {
			if scan.Text() == receiptCrashMarker {
				reached <- true
				return
			}
		}
		reached <- false
	}()
	select {
	case ok := <-reached:
		if !ok {
			f.t.Fatal("writer exited without reaching the required commit barrier")
		}
	case <-ctx.Done():
		f.t.Fatal("writer did not reach the required commit barrier")
	}
	if err := cmd.Process.Kill(); err != nil {
		f.t.Fatal("cannot forcibly terminate the synthetic writer")
	}
	err = cmd.Wait()
	waited = true
	var exit *exec.ExitError
	if !errors.As(err, &exit) || cmd.ProcessState.Success() || ctx.Err() != nil {
		f.t.Fatal("expected a killed writer, not normal completion or deadline cancellation")
	}
}

func TestMariaDBApplicationCrashDisposable(t *testing.T) {
	if mode := os.Getenv("ITMAP_TEST_RECEIPT_WORKER_MODE"); mode != "" {
		receiptCrashWorker(t, mode)
		return
	}
	for _, phase := range []string{"before_commit", "after_commit"} {
		t.Run(phase, func(t *testing.T) {
			f := openReceiptFixture(t)
			f.killWriter(phase)
			if phase == "before_commit" {
				f.assertCounts(0, 0, 0)
			} else {
				f.assertCounts(1, 1, 1)
			}
			// A separate, newly started Go process repeats the SAME frozen intent.
			ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
			defer cancel()
			output, err := f.crashCommand(ctx, "recover").CombinedOutput()
			if err != nil {
				t.Fatal("fresh writer process could not recover the original intent")
			}
			var recoveredID int64
			var replayed bool
			found := false
			for _, line := range strings.Split(string(output), "\n") {
				fields := strings.Fields(line)
				if len(fields) != 3 || fields[0] != receiptRecoveryMarker {
					continue
				}
				if found {
					t.Fatal("fresh writer emitted multiple recovery results")
				}
				recoveredID, err = strconv.ParseInt(fields[1], 10, 64)
				if err != nil {
					t.Fatal("invalid synthetic recovery identity")
				}
				replayed, err = strconv.ParseBool(fields[2])
				if err != nil {
					t.Fatal("invalid synthetic recovery replay flag")
				}
				found = true
			}
			if !found || recoveredID <= 1 || replayed != (phase == "after_commit") {
				t.Fatal("fresh process did not distinguish rollback from committed replay")
			}
			f.assertCounts(1, 1, 1)
			result, err := f.repo.CreateWithReceipt(f.ctx, receiptUser, f.input)
			if err != nil || !result.Replayed || result.CreatedApplicationID != recoveredID {
				t.Fatalf("recovered process result lost durable identity: %T", err)
			}
			row, err := f.repo.ByID(f.ctx, receiptUser, recoveredID)
			if err != nil || row.Comment != f.input.Comment || row.Status != f.input.Status || len(row.History) != 1 {
				t.Fatal("process recovery changed fields or duplicated initial history")
			}
			f.assertCounts(1, 1, 1)
		})
	}
}
