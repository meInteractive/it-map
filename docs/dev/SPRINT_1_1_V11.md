# Sprint 1.1 — V11: idempotent CRM application creation

**Development checkpoint (24 October 2026). Not production-ready.**

## Existing Sites v316 contract retained

- `GET /api/applications/create-context`: signed `ac1.<HMAC-SHA256>` bound to the logged-in user and the stored `app_runtime_generation` UUID.
- `POST /api/applications/create` and `POST /api/applications`: JSON body with client UUID v4 `idempotencyKey`, `creationContext`, company, optional vacancy, status, sources, recruiter fields and comments.
- First success: HTTP 201, full company applications result plus `createdApplicationId` and `replayed:false`. Valid replay: HTTP 200 with `replayed:true`. Missing intent context: 428; conflicting intent or restored generation: 409; deleted original application: 410.
- A durable record in `application_creation_receipts` is written **inside the same MariaDB transaction** as the application and its initial status history. No personal text is copied to the receipt: only the canonical SHA-256 intent hash. Repeated requests cannot silently insert another application after a lost HTTP response.
- Company membership is verified against `app_catalog`. The 10-application limit is protected by a `company_crm` primary-key row lock; an optional vacancy is checked and locked, then duplicate user-vacancy links are rejected. The runtime generation row is locked and rechecked before writing.
- Request hash uses Sites-compatible JSON array field order and sorted source identifiers. Unit tests include a hash fixture independently generated with Node.js `JSON.stringify` and Unicode U+2028 formatting.
- The final response is read **after** commit. If this read fails, the HTTP request returns 503, but the durable receipt allows the same request to be safely replayed later. The generation and created record are rechecked after response construction.

## Verification completed locally

- `go test -race ./...`, `go vet ./...` and public-source static scan.
- HTTP tests: session enforcement, same-origin protection, content-type and body limits, success vs replay codes, structured 428/409/410 errors, generic DB errors do not disclose secrets.
- Pure Go tests: canonical intent normalization, identical-source hash stability, signed context tamper and generation checks, unknown company rejection.
- Deterministic fake SQL-driver tests: commit followed by HTTP-response-read error does not duplicate the application on retry; reuse with modified data fails; deleted receipts return 410; generation rotation fails; injected history failures roll back application + receipt; maximum 10 enforced.

## Staging acceptance gates, not yet completed

1. Download and pin the `go-sql-driver/mysql` module and its **`go.sum`** in a networked environment. The local environment currently cannot resolve `proxy.golang.org`. Production must build with `-tags mariadb`.
2. Restore the user-provided **private** MariaDB 10.11 dump into an isolated database. Review `db/application-create-preflight.sql` using a read-only user. Do not add results or operational records to Git.
3. On a disposable staging database, verify one create, lost response/retry, different payload with same key, a duplicate linked vacancy, 10-application limit, history insertion/rollback, and an invalidated context after generation rotation.
4. Verify Sites React client uses the context and UUID flow and receives the complete company response. The vacancy-card one-click `applyToVacancy` shortcut is a **different route** and is **not yet migrated**.
5. Backup/restore must rotate `app_runtime_generation` transactionally under the same locking discipline. Before that is implemented, backup/restore remains disabled.
6. Review DB-level uniqueness for `(user_name,vacancy_id)` and coordinate all writers (including the eventual parser), not only this Go route.

**Do not promote `master` to production.** Existing GPT Sites and its D1 have not been modified.
