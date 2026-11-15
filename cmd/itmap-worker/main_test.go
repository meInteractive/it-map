package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"strings"
	"testing"

	"itmap/internal/storage"
	"itmap/internal/workerengine"
)

type testConnector struct{}
type testDriver struct{}

func (testConnector) Connect(context.Context) (driver.Conn, error) { return nil, errors.New("unused") }
func (testConnector) Driver() driver.Driver                        { return testDriver{} }
func (testDriver) Open(string) (driver.Conn, error)                { return nil, errors.New("unused") }

func testEnvironment() map[string]string {
	return map[string]string{
		"ITMAP_WORKER_ENABLED": "1", "ITMAP_PARSER_EXECUTION_ENABLED": "1", "ITMAP_WORKER_STAGING_REAL_PROVIDERS": "1",
		"APP_ENV": "staging", "DB_HOST": "127.0.0.1", "DB_PORT": "3306", "DB_NAME": "itmap_worker_staging", "DB_USER": "itmap_worker_app",
		"DB_PASSWORD": "synthetic-password", "SESSION_SECRET": strings.Repeat("s", 32),
	}
}
func getter(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func TestRejectsUnsafeConfigurationBeforeOpeningDatabase(t *testing.T) {
	for _, key := range []string{"ITMAP_WORKER_ENABLED", "ITMAP_PARSER_EXECUTION_ENABLED", "ITMAP_WORKER_STAGING_REAL_PROVIDERS", "ITMAP_WORKER_HEALTH_ENABLED", "ITMAP_WORKER_LAUNCH_IDLE_SECONDS", "ITMAP_WORKER_RECONCILE_IDLE_SECONDS", "ITMAP_WORKER_PIPELINE_IDLE_SECONDS", "ITMAP_WORKER_ERROR_MIN_SECONDS", "ITMAP_WORKER_ERROR_MAX_SECONDS", "ITMAP_WORKER_MAX_JOBS", "ITMAP_WORKER_SHUTDOWN_GRACE_SECONDS", "APP_ENV", "DB_HOST", "DB_PORT", "DB_NAME", "DB_USER", "DB_PASSWORD", "SESSION_SECRET"} {
		t.Run(key, func(t *testing.T) {
			env := testEnvironment()
			env[key] = "invalid"
			if key == "DB_PASSWORD" {
				env[key] = ""
			}
			opened := false
			err := run(context.Background(), getter(env), func(context.Context, storage.Settings, string) (*sql.DB, error) { opened = true; return nil, nil }, func(context.Context, *sql.DB, settings) error { return nil })
			if !errors.Is(err, errConfig) || opened {
				t.Fatal("неверные настройки открыли соединение", err)
			}
		})
	}
}

func TestRunClosesPoolAndRedactsErrors(t *testing.T) {
	env := testEnvironment()
	env["ITMAP_WORKER_MAX_JOBS"] = "2"
	db := sql.OpenDB(testConnector{})
	called := false
	err := run(context.Background(), getter(env), func(_ context.Context, s storage.Settings, password string) (*sql.DB, error) {
		if s.Database != "itmap_worker_staging" || password != env["DB_PASSWORD"] {
			t.Fatal("неверные параметры соединения")
		}
		return db, nil
	}, func(_ context.Context, got *sql.DB, s settings) error {
		called = true
		if got != db || s.secret != env["SESSION_SECRET"] || s.runtime.MaxJobs != 2 {
			t.Fatal("неверная композиция")
		}
		return errors.New("provider-secret-should-not-escape")
	})
	if !called || !errors.Is(err, errStartup) || strings.Contains(err.Error(), "provider-secret") {
		t.Fatal("ошибка не скрыта", err)
	}
	if db.PingContext(context.Background()) == nil {
		t.Fatal("пул не закрыт")
	}
}

func TestCancelledRunDoesNotOpenDatabase(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := run(ctx, getter(testEnvironment()), func(context.Context, storage.Settings, string) (*sql.DB, error) { called = true; return nil, nil }, func(context.Context, *sql.DB, settings) error { return nil })
	if !errors.Is(err, context.Canceled) || called {
		t.Fatal("отменённый worker открыл БД", err)
	}
}

func TestGenerationChangeReturnsSafeRestartReason(t *testing.T) {
	db := sql.OpenDB(testConnector{})
	err := run(context.Background(), getter(testEnvironment()), func(context.Context, storage.Settings, string) (*sql.DB, error) { return db, nil }, func(context.Context, *sql.DB, settings) error {
		return errors.Join(workerengine.ErrGenerationChanged, errors.New("CI private diagnostic"))
	})
	if !errors.Is(err, errGeneration) || exitReason(err) != "generation_changed" || strings.Contains(err.Error(), "CI private") {
		t.Fatal("generation failure was not safely classified", err)
	}
	if exitReason(errors.New("CI private")) != "startup_or_runtime_failure" {
		t.Fatal("unexpected error leaked to log category")
	}
}

func TestStagingServiceRestartAndIsolationContract(t *testing.T) {
	data, err := os.ReadFile("../../deploy/itmap-worker-staging.service.example")
	if err != nil {
		t.Fatal(err)
	}
	required := []string{"User=itmap-worker-staging", "Group=itmap-worker-staging", "EnvironmentFile=/etc/itmap/worker-staging.env", "ExecStart=/opt/itmap/worker-staging/itmap-worker", "Restart=on-failure", "TimeoutStopSec=90s", "NoNewPrivileges=true", "ProtectSystem=strict"}
	for _, line := range required {
		if !strings.Contains("\n"+string(data)+"\n", "\n"+line+"\n") {
			t.Fatalf("service contract missing %s", line)
		}
	}
}
