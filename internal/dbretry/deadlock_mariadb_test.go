//go:build mariadb

package dbretry

import (
	"context"
	"errors"
	"fmt"
	mysql "github.com/go-sql-driver/mysql"
	"testing"
)

func TestTransactionRetryBoundary(t *testing.T) {
	for _, v := range []struct {
		name     string
		err      error
		attempts int
	}{{"deadlock", &mysql.MySQLError{Number: 1213}, 3}, {"wrapped deadlock", fmt.Errorf("transaction: %w", &mysql.MySQLError{Number: 1213}), 3}, {"lock timeout", &mysql.MySQLError{Number: 1205}, 1}, {"unknown commit", errors.New("CI connection lost during COMMIT"), 1}, {"SQL constraint", &mysql.MySQLError{Number: 1062}, 1}} {
		t.Run(v.name, func(t *testing.T) {
			calls := 0
			_, e := Run(context.Background(), func() (int, error) { calls++; return 0, v.err })
			if calls != v.attempts || !errors.Is(e, v.err) {
				t.Fatal("transaction retry crossed rollback guarantee boundary")
			}
		})
	}
	t.Run("successful retry result is returned once", func(t *testing.T) {
		calls := 0
		got, e := Run(context.Background(), func() (int, error) {
			calls++
			if calls == 1 {
				return 0, &mysql.MySQLError{Number: 1213}
			}
			return 17, nil
		})
		if e != nil || got != 17 || calls != 2 {
			t.Fatal("deadlock recovery lost committed result")
		}
	})
	t.Run("cancelled context never starts another attempt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		calls := 0
		_, e := Run(ctx, func() (int, error) { calls++; cancel(); return 0, &mysql.MySQLError{Number: 1213} })
		if !errors.Is(e, context.Canceled) || calls != 1 {
			t.Fatal("cancelled retry performed another transaction")
		}
	})
}
