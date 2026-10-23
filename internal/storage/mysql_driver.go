//go:build mariadb

package storage

// To enable MariaDB builds: go mod tidy, then go build -tags mariadb ./cmd/itmap.
// Kept behind a build tag while external dependencies are unavailable in the
// offline migration preparation environment. Production builds MUST use this tag.
import _ "github.com/go-sql-driver/mysql"
