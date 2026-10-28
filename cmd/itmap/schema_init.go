package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"itmap/internal/config"
	"itmap/internal/dbschema"
	"itmap/internal/storage"
	"os"
	"strings"
)

// Explicit local administrative command: it never starts HTTP or needs site /
// provider credentials. DB_PASSWORD is supplied through the environment only.
func schemaInitCommand(ctx context.Context, args []string, password string, out, diagnostics io.Writer) int {
	flags := flag.NewFlagSet("schema-init", flag.ContinueOnError)
	flags.SetOutput(diagnostics)
	host := flags.String("host", "127.0.0.1", "local MariaDB host")
	port := flags.String("port", "3306", "local MariaDB port")
	database := flags.String("database", "", "target database (required)")
	user := flags.String("user", "", "database user (required)")
	flags.Usage = func() {
		fmt.Fprintln(diagnostics, "Usage: itmap schema-init --database NAME --user USER [--host 127.0.0.1] [--port 3306]\nRun offline before HTTP startup. Password: DB_PASSWORD environment variable. Only trusted missing schema objects are created; populated partial schemas are refused.")
		flags.PrintDefaults()
	}
	if e := flags.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(diagnostics, "schema-init does not accept positional arguments")
		return 2
	}
	environment, present := os.LookupEnv("APP_ENV")
	if !present {
		environment = "development"
	} else {
		environment = strings.TrimSpace(environment)
	}
	if e := config.ValidateEnvironment(environment); e != nil {
		fmt.Fprintln(diagnostics, e)
		return 2
	}
	dsn, e := storage.MakeDSN(storage.Settings{Host: *host, Port: *port, Database: *database, User: *user}, password)
	if e != nil {
		fmt.Fprintln(diagnostics, e)
		return 2
	}
	supported := false
	for _, name := range sql.Drivers() {
		if name == "mysql" {
			supported = true
		}
	}
	if !supported {
		fmt.Fprintln(diagnostics, "mysql driver unavailable: compile with -tags mariadb")
		return 2
	}
	db, e := sql.Open("mysql", dsn)
	if e != nil {
		fmt.Fprintln(diagnostics, "cannot initialize database connection")
		return 1
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if e = db.PingContext(ctx); e != nil {
		fmt.Fprintln(diagnostics, "cannot connect to local database")
		return 1
	}
	result, e := dbschema.Initialize(ctx, db)
	if e != nil {
		fmt.Fprintln(diagnostics, e)
		return 1
	}
	if e = json.NewEncoder(out).Encode(result); e != nil {
		return 1
	}
	return 0
}
