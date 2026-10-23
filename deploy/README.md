# Staging only: Go migration shell

**Do not point the production domain to this service**: the business API and
MariaDB are not connected, and readiness intentionally returns HTTP 503.

For isolated staging, build with `go build -o itmap ./cmd/itmap`, create a
protected `/etc/itmap/staging.env` owned by root (`chmod 600`) with
`APP_ENV=staging`, `HTTP_ADDR=127.0.0.1:8080`, and appropriate DB metadata.
The application is not yet connected to DB; `DB_PASSWORD` is not consumed.

A reverse proxy terminates HTTPS and forwards only the staging hostname to
`127.0.0.1:8080`. Do not expose an unfinished service directly to the internet.
Production startup is intentionally prohibited until all acceptance gates pass.
The example systemd unit needs editing before installation; it is not deployed.
