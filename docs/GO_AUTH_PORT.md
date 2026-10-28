# Sprint 1.1.08: migration of legacy login and sessions

Routes implemented in Go:

- `POST /auth`: existing HTML form, 303 on success, 401 + `X-App-Login-Error: 1` on invalid credentials
- `GET /api/session`: `{name, role, isAdmin}`, or 401 if not logged in
- `GET /logout`: clears the session cookie and redirects to `/`

Accounts and privileges preserve the original source contract in
`server/constants.js`: `Eugene` (admin), `Nastya`, `Demo` (users).
All passwords and `SESSION_SECRET` are read from the runtime environment;
there are no credentials in Git. The existing `it_map_session` signed HMAC-SHA256
`v2` token format (30-day TTL) is preserved. Session cookies use HttpOnly,
Secure and SameSite=Strict. Cross-origin login is rejected and the form body is
bounded to 8 KiB. To turn this on, set `AUTH_ENABLED=true`, a randomly generated
`SESSION_SECRET` with at least 32 bytes and `SITE_PASSWORD`; the other user
passwords are optional.

Unported business API endpoints return 401 without a session and 503 with one.
Internal scheduler, MCP and database export endpoints **always remain disabled**
until explicitly ported and accepted. This is not a production-ready auth audit:
rate limiting, credential rotation procedures, real reverse proxy deployment,
remain acceptance gates for later sprint tasks. The synthetic three-account
desktop/mobile browser matrix was accepted in GO-051; it does not prove the
external reverse proxy.

When switching hosts, old sessions can only be verified if the same session
signing secret is used; rotating it intentionally invalidates old sessions.

## HTTPS/proxy и повторный вход (GO-320)

Actual loopback TLS → private HTTP → Go session flow для трёх ролей проверяет
`TestAuthHTTPSProxySessionFlow`; он не читает SQL, не сохраняет credentials и
не отключает TLS verification. Сценарии и границы проверки, включая отличие
HTTP cookie jar от browser SameSite policy, описаны в
[операторской инструкции](deployment/AUTH-ACCEPTANCE.md). Настоящие DNS/ACME,
Nginx и браузерная приёмка на тестовом домене по-прежнему не выполнены.
