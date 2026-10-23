package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"itmap/internal/auth"
	"itmap/internal/catalog"
	"itmap/internal/channels"
	"itmap/internal/config"
	"itmap/internal/httpapi"
	"itmap/internal/roles"
	"itmap/internal/storage"
	"itmap/internal/vacancies"
)

func httpHandler(logger *slog.Logger, db *sql.DB, manager *auth.Manager) http.Handler {
	opts := httpapi.Options{Auth: manager, WebDir: os.Getenv("WEB_DIST_DIR")}
	if db != nil {
		opts.DB = db
		repo := catalog.Repository{DB: db}
		opts.Catalog = repo
		opts.Compact = repo
		opts.Channels = channels.Repository{DB: db}
		opts.Roles = roles.Repository{DB: db}
		opts.Vacancies = vacancies.Repository{DB: db}
		opts.VacancyDetails = vacancies.Repository{DB: db}
		opts.VacancyFlags = vacancies.Repository{DB: db}
	}
	return httpapi.NewHandlerWithOptions(logger, opts)
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	var database *sql.DB
	if cfg.DBEnabled {
		bootCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		database, err = storage.Open(bootCtx, storage.Settings{
			Host: cfg.DBHost, Port: cfg.DBPort, Database: cfg.DBName, User: cfg.DBUser,
		}, os.Getenv("DB_PASSWORD"))
		if err != nil {
			logger.Error("database not ready", "error", err)
			os.Exit(1)
		}
		defer database.Close()
		logger.Info("MariaDB baseline verified; business API migration still incomplete")
	}
	var manager *auth.Manager
	if cfg.AuthEnabled {
		manager, err = auth.NewManager(os.Getenv("SESSION_SECRET"), map[string]string{
			"eugene": os.Getenv("SITE_PASSWORD"), "nastya": os.Getenv("NASTYA_PASSWORD"), "demo": os.Getenv("DEMO_PASSWORD"),
		})
		if err != nil {
			logger.Error("authentication unavailable", "error", err)
			os.Exit(1)
		}
	}
	addr := cfg.HTTPAddr
	server := &http.Server{
		Addr: addr, Handler: httpHandler(logger, database, manager),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
		}
	}()
	logger.Info("starting migration shell; not production ready", "addr", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("http server failed", "error", err)
		os.Exit(1)
	}
}
