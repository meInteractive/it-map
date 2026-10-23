module itmap

go 1.23

// The production MariaDB driver is enabled with -tags mariadb.
// It is pinned for builds on the initial Go 1.23 toolchain.
require github.com/go-sql-driver/mysql v1.9.3
