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

	"itmap/internal/config"
	"itmap/internal/httpapi"
	"itmap/internal/storage"
)

func httpHandler(logger *slog.Logger, db *sql.DB) http.Handler {
	if db == nil {
		return httpapi.NewHandler(logger)
	}
	return httpapi.NewHandler(logger, db)
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
	addr := cfg.HTTPAddr
	server := &http.Server{
		Addr: addr, Handler: httpHandler(logger, database),
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
