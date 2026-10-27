//go:build mariadb && v39restore && restorehttp

package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	mysql "github.com/go-sql-driver/mysql"
	"itmap/internal/applications"
	"itmap/internal/backup"
	"itmap/internal/parser"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type restoreBarrier struct {
	mu               sync.Mutex
	fired            bool
	entered, release chan struct{}
	once             sync.Once
}

func (h *restoreBarrier) unblock() { h.once.Do(func() { close(h.release) }) }

type restoreBarrierConnector struct {
	driver.Connector
	hook *restoreBarrier
}

func (c restoreBarrierConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, e := c.Connector.Connect(ctx)
	if e != nil {
		return nil, e
	}
	return &restoreBarrierConn{Conn: conn, hook: c.hook}, nil
}

type restoreBarrierConn struct {
	driver.Conn
	hook *restoreBarrier
}

func (c *restoreBarrierConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, o)
}
func (c *restoreBarrierConn) QueryContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	rows, e := c.Conn.(driver.QueryerContext).QueryContext(ctx, q, a)
	if e != nil {
		return nil, e
	}
	if q == "SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE" {
		c.hook.mu.Lock()
		first := !c.hook.fired
		c.hook.fired = true
		c.hook.mu.Unlock()
		if first {
			close(c.hook.entered)
			select {
			case <-c.hook.release:
			case <-ctx.Done():
				rows.Close()
				return nil, ctx.Err()
			}
		}
	}
	return rows, nil
}
func (c *restoreBarrierConn) ExecContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, a)
}
func (c *restoreBarrierConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	return c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, q)
}
func restoreBarrierPool(f *restoreFixture) (*sql.DB, *restoreBarrier) {
	cfg, e := mysql.ParseDSN(os.Getenv("ITMAP_TEST_MARIADB_WRITE_DSN"))
	if e != nil || requireDisposableTarget(cfg.Net, cfg.Addr, cfg.DBName, cfg.User) != nil {
		f.t.Fatal("unsafe concurrent restore target")
	}
	connector, e := mysql.NewConnector(cfg)
	if e != nil {
		f.t.Fatal("restore connector failed")
	}
	h := &restoreBarrier{entered: make(chan struct{}), release: make(chan struct{})}
	db := sql.OpenDB(restoreBarrierConnector{Connector: connector, hook: h})
	db.SetMaxOpenConns(1)
	f.t.Cleanup(func() { h.unblock(); db.Close() })
	return db, h
}
func waitRestoreBarrier(f *restoreFixture, h *restoreBarrier) {
	f.t.Helper()
	select {
	case <-h.entered:
	case <-f.ctx.Done():
		f.t.Fatal("generation barrier timed out")
	}
}
func waitGenerationWaiter(f *restoreFixture) {
	f.t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if f.count("SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE ID<>CONNECTION_ID() AND INFO='SELECT generation FROM app_runtime_generation WHERE id=1 FOR UPDATE'") > 0 {
			return
		}
		select {
		case <-ticker.C:
		case <-f.ctx.Done():
			f.t.Fatal("no real generation lock waiter observed")
		}
	}
}
func TestMariaDBV39RestoreConcurrentDisposable(t *testing.T) {
	for _, first := range []string{"restore", "create"} {
		t.Run(first+" holds generation first", func(t *testing.T) {
			f := openRestoreFixture(t)
			f.restore(f.body)
			old := f.generation()
			apps := applications.Repository{DB: f.db, CreationSecret: []byte(strings.Repeat("c", 64))}
			token, e := apps.CreationContext(f.ctx, "Demo")
			if e != nil {
				t.Fatal("context unavailable")
			}
			input := applications.CreateInput{CompanyName: "Synthetic company", Status: "Откликнулся", Comment: "CI restore race", IdempotencyKey: "11111111-1111-4111-8111-111111111112", CreationContext: token}
			t.Cleanup(func() {
				_, _ = f.db.ExecContext(context.Background(), "DELETE FROM application_creation_receipts WHERE user_name='Demo' AND idempotency_key=?", input.IdempotencyKey)
			})
			runRepo := parser.RunRepository{DB: f.db, Generation: old}
			run, e := runRepo.Begin(f.ctx, parser.RunOptions{RequestID: "CI restore run", Trigger: "manual"})
			if e != nil {
				t.Fatal("scheduler owner unavailable")
			}
			t.Cleanup(func() {
				_, _ = f.db.ExecContext(context.Background(), "DELETE FROM scheduler_runs WHERE id=?", run.Run.ID)
			})
			pool, hook := restoreBarrierPool(f)
			restoreRepo := backup.Repository{DB: f.db}
			createRepo := apps
			if first == "restore" {
				restoreRepo.DB = pool
			} else {
				createRepo.DB = pool
			}
			restored := make(chan error, 1)
			created := make(chan error, 1)
			startRestore := func() { go func() { _, e := restoreRepo.RestoreV39(f.ctx, f.body); restored <- e }() }
			startCreate := func() { go func() { _, e := createRepo.CreateWithReceipt(f.ctx, "Demo", input); created <- e }() }
			if first == "restore" {
				startRestore()
			} else {
				startCreate()
			}
			waitRestoreBarrier(f, hook)
			if first == "restore" {
				startCreate()
			} else {
				startRestore()
			}
			waitGenerationWaiter(f)
			select {
			case <-created:
				t.Fatal("create crossed held generation lock")
			default:
			}
			select {
			case <-restored:
				t.Fatal("restore crossed held generation lock")
			default:
			}
			hook.unblock()
			restoreErr, createErr := <-restored, <-created
			if restoreErr != nil {
				t.Fatalf("restore race failed: %T", restoreErr)
			}
			if first == "restore" && !errors.Is(createErr, applications.ErrCreationContextExpired) {
				t.Fatalf("old create was not fenced: %T", createErr)
			}
			if first == "create" && createErr != nil && !errors.Is(createErr, applications.ErrCreationContextExpired) {
				t.Fatalf("serialized create failed: %T", createErr)
			}
			if f.generation() == old || !reflect.DeepEqual(f.snapshot(), restoreRecords(t, f.expected)) {
				t.Fatal("concurrent restore retained partial app data")
			}
			if _, e = apps.CreateWithReceipt(f.ctx, "Demo", input); !errors.Is(e, applications.ErrCreationContextExpired) {
				t.Fatal("old signed intent survived restore")
			}
			fresh, e := apps.CreationContext(f.ctx, "Demo")
			if e != nil {
				t.Fatal("fresh context unavailable")
			}
			input.CreationContext = fresh
			if first == "create" {
				if _, e = apps.CreateWithReceipt(f.ctx, "Demo", input); !errors.Is(e, applications.ErrCreationContextExpired) {
					t.Fatal("old durable receipt reused after restore")
				}
				if f.count("SELECT COUNT(*) FROM application_creation_receipts WHERE user_name='Demo'") != 1 {
					t.Fatal("restore deleted old durable receipt")
				}
			} else if f.count("SELECT COUNT(*) FROM application_creation_receipts WHERE user_name='Demo'") != 0 {
				t.Fatal("fenced create wrote a receipt")
			}
			if e = runRepo.AssertActive(f.ctx, run.Run.ID); !errors.Is(e, parser.ErrRunLeaseLost) {
				t.Fatal("old scheduler retained active fence")
			}
			if e = runRepo.Finish(f.ctx, run.Run.ID, "completed"); !errors.Is(e, parser.ErrRunGeneration) {
				t.Fatal("old scheduler wrote across restore")
			}
		})
	}
}
