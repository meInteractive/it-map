//go:build mariadb

package catalog

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

func TestMariaDBCompanyExportReadOnly(t *testing.T) {
	dsn := os.Getenv("ITMAP_TEST_MARIADB_DSN")
	if dsn == "" {
		if os.Getenv("ITMAP_REQUIRE_MARIADB_INTEGRATION") == "1" {
			t.Fatal("не настроена тестовая MariaDB")
		}
		t.Skip("не настроена тестовая MariaDB")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, err := (Repository{DB: db}).ExportCompaniesXLSX(ctx)
	if err != nil {
		t.Fatal(err)
	}
	book, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	sheet, err := book.Open("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer sheet.Close()
	body, err := io.ReadAll(sheet)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Название компании") ||
		(os.Getenv("ITMAP_TEST_SYNTHETIC_READS") == "1" && !strings.Contains(string(body), "CI Synthetic Employer")) {
		t.Fatal("пропущены заголовки или синтетическая компания")
	}
}
