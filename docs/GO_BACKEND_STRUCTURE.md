# Go source layout (migration 1.1)

The repository retains the original Sites JavaScript backend under `server/`
for reference only. The new backend is **not** in `server/`:

```
cmd/itmap/main.go             # Go service entry point and dependency wiring
internal/auth/               # signed session cookies and roles
internal/catalog/            # catalog search/list/map/detail
internal/channels/           # Telegram channels read-only list
internal/roles/              # vacancy role hierarchy
internal/vacancies/          # read-only list, date filters, role/source projections
internal/storage/            # MariaDB connection, baseline validation
internal/httpapi/            # HTTP endpoint handlers and static web serving
internal/config/             # environment validation
go.mod                       # Go module declaration
```

To find only the Go backend files: `find cmd internal -name '*.go'`.
Do not upload the old `server/` directory to a Go-only standalone service by
mistake: the root monorepo intentionally keeps it as migration reference.

`go test -race ./... && go vet ./...` verifies modules and HTTP behaviour
without a database. The MariaDB SQL integration test requires an isolated
staging database and `-tags mariadb`; CI success alone does not prove parity.

Current API focus: `GET /api/vacancies/list`. Vacancy detail and all data
mutations intentionally remain disabled until they are implemented and tested.
