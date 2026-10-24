module itmap

go 1.23

// Production MariaDB support is compiled with -tags mariadb.
// Resolve checksums in networked CI and commit go.sum before staging acceptance.
require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/go-sql-driver/mysql v1.9.3
)
