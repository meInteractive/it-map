module itmap

go 1.23.0

// Production MariaDB support is compiled with -tags mariadb.
// Resolve checksums in networked CI and commit go.sum before staging acceptance.
require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/go-sql-driver/mysql v1.9.3
)

require (
	github.com/bits-and-blooms/bitset v1.20.0 // indirect
	github.com/nlnwa/whatwg-url v0.6.2
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/text v0.28.0
)
