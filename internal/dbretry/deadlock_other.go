//go:build !mariadb

package dbretry

// No production SQL driver is linked into the offline migration shell.
func isDeadlock(error) bool { return false }
