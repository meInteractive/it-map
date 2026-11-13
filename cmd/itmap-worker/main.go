// Команда запускает экспериментальный worker только в отдельной staging БД.
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

var errConfig = errors.New("worker staging configuration invalid")
var errStartup = errors.New("worker staging startup failed")

type settings struct {
	database                    storage.Settings
	password, secret, openAIKey string
	health                      bool
}

// load проверяет все допуски до открытия соединения. Значения учётных данных
// остаются в памяти и не возвращаются в сообщениях об ошибках.
func load(getenv func(string) string) (settings, error) {
	if _, err := workerconfig.Parse(getenv); err != nil {
		return settings{}, errConfig
	}
	if getenv("APP_ENV") != "staging" || getenv("ITMAP_WORKER_STAGING_REAL_PROVIDERS") != "1" ||
		getenv("DB_HOST") != "127.0.0.1" || getenv("DB_PORT") != "3306" ||
		getenv("DB_NAME") != "itmap_worker_staging" || getenv("DB_USER") != "itmap_worker_app" ||
		getenv("DB_PASSWORD") == "" || len(getenv("SESSION_SECRET")) < 32 {
		return settings{}, errConfig
	}
	if value := getenv("ITMAP_WORKER_HEALTH_ENABLED"); value != "" && value != "0" && value != "1" {
		return settings{}, errConfig
	}
	return settings{database: storage.Settings{Host: "127.0.0.1", Port: "3306", Database: "itmap_worker_staging", User: "itmap_worker_app"},
		password: getenv("DB_PASSWORD"), secret: getenv("SESSION_SECRET"), openAIKey: getenv("OPENAI_API_KEY"), health: getenv("ITMAP_WORKER_HEALTH_ENABLED") == "1"}, nil
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
	monitor := workerengine.NewMonitor(4 * time.Minute)
	defer monitor.Stop()
	workerCtx, stop := context.WithCancel(ctx)
	defer stop()
	var server *http.Server
	var serverDone chan error
	if s.health {
		listener, err := net.Listen("tcp", "127.0.0.1:18082")
		if err != nil {
			return errStartup
		}
		server = &http.Server{Handler: monitor.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 15 * time.Second}
		serverDone = make(chan error, 1)
		go func() {
			err := server.Serve(listener)
			serverDone <- err
			if !errors.Is(err, http.ErrServerClosed) {
				stop()
			}
		}()
	}
	err = (workerengine.Engine{Worker: w, OnResult: func(profile workerloop.ProfileName, outcome workerloop.ProfileOutcome) {
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

func main() {
	if len(os.Args) != 1 {
		slog.Error("worker does not accept arguments")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv, storage.Open, runWorker); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("worker stopped", "reason", "startup_or_runtime_failure")
		os.Exit(1)
	}
}
