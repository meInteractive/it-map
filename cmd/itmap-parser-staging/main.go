// Команда запускает только синтетический парсер на изолированной локальной БД.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"itmap/internal/auth"
	"itmap/internal/httpapi"
	"itmap/internal/parser"
	"itmap/internal/parser/stagingmock"
	"itmap/internal/storage"
	"itmap/internal/vacancies"
	"itmap/internal/workerengine"
	"itmap/internal/workerloop"
)

type settings struct{ environment, enabled, host, port, database, user, password, secret, sitePassword string }

func (s settings) validate() error {
	if s.environment != "staging" || s.enabled != "1" || s.host != "127.0.0.1" || s.port != "3306" || s.database != "itmap_parser_staging" || s.user != "itmap_parser_app" || s.password == "" || len(s.secret) < 32 || s.sitePassword == "" {
		return errors.New("staging mock: неверная конфигурация изолированного запуска")
	}
	return nil
}

func run() error {
	s := settings{environment: os.Getenv("APP_ENV"), enabled: os.Getenv("ITMAP_STAGING_MOCK"), host: os.Getenv("DB_HOST"), port: os.Getenv("DB_PORT"), database: os.Getenv("DB_NAME"), user: os.Getenv("DB_USER"), password: os.Getenv("DB_PASSWORD"), secret: os.Getenv("SESSION_SECRET"), sitePassword: os.Getenv("SITE_PASSWORD")}
	if err := s.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := storage.Open(ctx, storage.Settings{Host: s.host, Port: s.port, Database: s.database, User: s.user}, s.password)
	if err != nil {
		return err
	}
	defer db.Close()
	if len(os.Args) == 2 && os.Args[1] == "seed" {
		if err := stagingmock.Seed(ctx, db, s.secret, "itmap_parser_staging"); err != nil {
			return err
		}
		slog.Info("staging mock synthetic data initialized")
		return nil
	}
	if len(os.Args) == 2 && (os.Args[1] == "worker" || os.Args[1] == "worker-lost-ack") {
		var generation string
		if err := db.QueryRowContext(ctx, "SELECT generation FROM app_runtime_generation WHERE id=1").Scan(&generation); err != nil {
			return errors.New("staging mock: поколение базы недоступно")
		}
		providers := &stagingmock.Providers{LoseStartAck: os.Args[1] == "worker-lost-ack"}
		worker, err := stagingmock.Worker(parser.RunRepository{DB: db, Generation: generation}, s.secret, providers)
		if err != nil {
			return errors.New("staging mock: worker недоступен")
		}
		workerCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		err = (workerengine.Engine{Worker: worker, OnResult: func(profile workerloop.ProfileName, outcome workerloop.ProfileOutcome) {
			if outcome == workerloop.ProfileFailed || outcome == workerloop.ProfileTimeout {
				slog.Warn("staging mock worker step failed", "profile", profile, "outcome", outcome)
			}
		}}).Run(workerCtx)
		slog.Info("staging mock worker stopped", "synthetic_actor_starts", providers.Starts())
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	if len(os.Args) != 1 {
		return errors.New("staging mock: неизвестная команда")
	}
	manager, err := auth.NewManager(s.secret, map[string]string{"eugene": s.sitePassword})
	if err != nil {
		return errors.New("staging mock: неверные настройки авторизации")
	}
	providers := &stagingmock.Providers{}
	control := parser.NewCycleControlWithFactory(db, func(r parser.RunRepository) (parser.Worker, error) { return stagingmock.Worker(r, s.secret, providers) })
	handler := httpapi.NewHandlerWithOptions(nil, httpapi.Options{DB: db, Auth: manager, SchedulerCycle: control, SchedulerProbe: control, VacancyDetails: vacancies.Repository{DB: db}})
	server := &http.Server{Addr: "127.0.0.1:18081", Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 120 * time.Second}
	return server.ListenAndServe()
}

func main() {
	if err := run(); err != nil {
		slog.Error("staging mock stopped", "error", err)
		os.Exit(1)
	}
}
