//go:build mariadb

package dbretry

import (
	"errors"
	mysql "github.com/go-sql-driver/mysql"
)

func isDeadlock(err error) bool {
	var e *mysql.MySQLError
	return errors.As(err, &e) && e.Number == 1213
}
