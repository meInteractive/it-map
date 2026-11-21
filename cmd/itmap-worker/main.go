// Команда запускает worker только при явно включённой конфигурации.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"itmap/internal/aiproviders"
	"itmap/internal/apifysettings"
	"itmap/internal/parser"
	"itmap/internal/storage"
	"itmap/internal/workerconfig"
	"itmap/internal/workerengine"
	"itmap/internal/workerloop"
)

var errConfig = errors.New("worker configuration invalid")
var errStartup = errors.New("worker startup failed")
var errGeneration = errors.New("worker generation changed")

type settings struct {
	database                    storage.Settings
	runtime                     workerconfig.Config
	password, secret, openAIKey string
	health                      bool
	releaseControl              bool
}

// load проверяет все допуски до открытия соединения. Значения учётных данных
// остаются в памяти и не возвращаются в сообщениях об ошибках.
func load(getenv func(string) string) (settings, error) {
	runtime, err := workerconfig.Parse(getenv)
	if err != nil {
		return settings{}, errConfig
	}
	database := storage.Settings{Host: getenv("DB_HOST"), Port: getenv("DB_PORT"), Database: getenv("DB_NAME"), User: getenv("DB_USER")}
	password := getenv("DB_PASSWORD")
	if password == "" || len(getenv("SESSION_SECRET")) < 32 {
		return settings{}, errConfig
	}
	health := getenv("ITMAP_WORKER_HEALTH_ENABLED")
	if health != "" && health != "0" && health != "1" {
		return settings{}, errConfig
	}
	control := getenv("ITMAP_WORKER_RELEASE_CONTROL_ENABLED")
	if control != "" && control != "0" && (control != "1" || getenv("APP_ENV") != "production") {
		return settings{}, errConfig
	}
	switch getenv("APP_ENV") {
	case "staging":
		if getenv("ITMAP_WORKER_STAGING_REAL_PROVIDERS") != "1" || database.Host != "127.0.0.1" || database.Port != "3306" || database.Database != "itmap_worker_staging" || database.User != "itmap_worker_app" {
			return settings{}, errConfig
		}
	case "production":
		// Explicit opt-in is separate from the worker and parser enable flags.
		// A local, least-privileged database account and health endpoint are required.
		if getenv("ITMAP_WORKER_PRODUCTION_ENABLED") != "1" || health != "1" || database.User == "root" {
			return settings{}, errConfig
		}
	default:
		return settings{}, errConfig
	}
	// Validate exactly as storage.Open will, without opening a connection or
	// returning a DSN (which contains credentials) in any error or log.
	if _, err := storage.MakeDSN(database, password); err != nil {
		return settings{}, errConfig
	}
	return settings{runtime: runtime, database: database, password: password,
		secret: getenv("SESSION_SECRET"), openAIKey: getenv("OPENAI_API_KEY"), health: health == "1", releaseControl: control == "1"}, nil
}

type opener func(context.Context, storage.Settings, string) (*sql.DB, error)
type workerRunner func(context.Context, *sql.DB, settings) error

// run не выводит сырые ошибки драйвера, данные провайдеров и секреты.
func run(ctx context.Context, getenv func(string) string, open opener, execute workerRunner) (result error) {
	if ctx == nil || getenv == nil || open == nil || execute == nil {
		return errConfig
	}
	s, err := load(getenv)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	boot, cancel := context.WithTimeout(ctx, 20*time.Second)
	db, err := open(boot, s.database, s.password)
	expired := boot.Err()
	cancel()
	if err != nil || expired != nil || db == nil {
		if db != nil {
			_ = db.Close()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errStartup
	}
	defer func() {
		if err := db.Close(); err != nil {
			result = errStartup
		}
	}()
	err = execute(ctx, db, s)
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if errors.Is(err, workerengine.ErrGenerationChanged) {
		return errGeneration
	}
	if err != nil {
		return errStartup
	}
	return nil
}

func runWorker(ctx context.Context, db *sql.DB, s settings) error {
	settingsRepo := apifysettings.NewRepository(db, s.secret)
	boot, cancel := context.WithTimeout(ctx, 10*time.Second)
	generation, err := settingsRepo.Generation(boot)
	cancel()
	if err != nil {
		return errStartup
	}
	w, err := parser.NewWorker(parser.RunRepository{DB: db, Generation: generation}, settingsRepo, aiproviders.NewRepository(db, s.secret, s.openAIKey))
	if err != nil {
		return errStartup
	}
	monitor := workerengine.NewMonitor(workerengine.MonitorMaxAge(s.runtime))
	defer monitor.Stop()
	workerCtx, stop := context.WithCancel(ctx)
	defer stop()
	go func() {
		<-workerCtx.Done()
		monitor.Stop()
	}()
	var pause *workerengine.PauseGate
	if s.releaseControl {
		control, listener, gate, err := openReleaseControl()
		if err != nil {
			return errStartup
		}
		pause = gate
		monitor.SetPauseGate(gate)
		defer control.Close()
		go func() {
			if err := control.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
				stop()
			}
		}()
	}

	var server *http.Server
	var serverDone chan error
	if s.health {
		listener, err := net.Listen("tcp", "127.0.0.1:18082")
		if err != nil {
			return errStartup
		}
		server = &http.Server{Handler: monitor.HandlerWithActivity(workerengine.NewQueueReader(db, generation), workerengine.NewRecoveryReader(db, generation), workerengine.NewHHScheduleReader(db, generation), workerengine.NewActivityReader(db, generation)), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second}
		serverDone = make(chan error, 1)
		go func() {
			err := server.Serve(listener)
			serverDone <- err
			if !errors.Is(err, http.ErrServerClosed) {
				stop()
			}
		}()
	}
	err = (workerengine.Engine{Worker: w, Config: &s.runtime, Pause: pause, ReconcileStaleApify: true, OnResult: func(profile workerloop.ProfileName, outcome workerloop.ProfileOutcome) {
		monitor.Record(profile, outcome)
		if outcome == workerloop.ProfileFailed || outcome == workerloop.ProfileTimeout {
			slog.Warn("worker step failed", "profile", profile, "outcome", outcome)
		}
	}}).Run(workerCtx)
	if server != nil {
		_ = server.Close()
		if serveErr := <-serverDone; !errors.Is(serveErr, http.ErrServerClosed) {
			return errStartup
		}
	}
	return err
}

// exitReason is a fixed category; it never includes driver or provider errors.
func exitReason(err error) string {
	if errors.Is(err, errGeneration) {
		return "generation_changed"
	}
	return "startup_or_runtime_failure"
}

func main() {
	if len(os.Args) != 1 {
		slog.Error("worker does not accept arguments")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv, storage.Open, runWorker); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("worker stopped", "reason", exitReason(err))
		os.Exit(1)
	}
}
