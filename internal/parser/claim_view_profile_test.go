package parser

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
)

var errSyntheticPaidRead = errors.New("synthetic paid ledger read")

type claimViewConnector struct{}
type claimViewDriver struct{}
type claimViewConn struct{}
type claimViewTx struct{}
type claimViewRows struct{ done bool }

func (claimViewConnector) Connect(context.Context) (driver.Conn, error) { return claimViewConn{}, nil }
func (claimViewConnector) Driver() driver.Driver                        { return claimViewDriver{} }
func (claimViewDriver) Open(string) (driver.Conn, error)                { return claimViewConn{}, nil }
func (claimViewConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (claimViewConn) Close() error              { return nil }
func (claimViewConn) Begin() (driver.Tx, error) { return claimViewTx{}, nil }
func (claimViewConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "SELECT candidate_id FROM scheduler_jobs") {
		return &claimViewRows{}, nil
	}
	if strings.Contains(query, "FROM apify_runs") {
		return nil, errSyntheticPaidRead
	}
	return nil, errors.New("unexpected query")
}
func (claimViewTx) Commit() error        { return nil }
func (claimViewTx) Rollback() error      { return nil }
func (*claimViewRows) Columns() []string { return []string{"candidate_id"} }
func (*claimViewRows) Close() error      { return nil }
func (r *claimViewRows) Next(dst []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dst[0] = int64(42)
	return nil
}

func TestClaimViewLoadsPaidHistoryOnlyForScan(t *testing.T) {
	db := sql.OpenDB(claimViewConnector{})
	defer db.Close()
	for _, includeScans := range []bool{false, true} {
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		view, err := readClaimView(context.Background(), tx, "synthetic", includeScans)
		_ = tx.Rollback()
		if !view.busy[42] {
			t.Fatal("candidate ownership omitted")
		}
		if includeScans && !errors.Is(err, errSyntheticPaidRead) {
			t.Fatal("scan did not fail closed on paid history", err)
		}
		if !includeScans && err != nil {
			t.Fatal("pipeline unexpectedly depends on scan history", err)
		}
	}
}
