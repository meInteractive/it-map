# Sprint 1.1 — V12: vacancy-card one-click applications

**Development checkpoint: 24 October 2026. NOT ready for production.**

## Legacy v316 contract

The current React vacancy list calls `GET /api/applications/create-context` to obtain
an opaque signed context and then `POST /api/vacancies/{id}/apply/create` with
`companyName`, `idempotencyKey` (UUID v4) and `creationContext`.
The legacy alternative `/api/vacancies/{id}/apply` is also implemented.

The Go route always constructs the same fixed intent as Sites:
status `Откликнулся`, empty comment, source `hh`, vacancy ID from the URL.
It returns HTTP 200 with `{ok:true,vacancy,createdApplicationId,replayed}`.
This **records** a CRM application; it does not submit a CV to an employer.

Both routes enforce signed sessions and same-origin protection. Error messages
are generic, with structured codes for expired context (409), owner changes
(409), conflicting idempotency intent (409), and a previously deleted application
(410). Missing context returns 428. No DB errors or contact data are echoed.

## Transactional semantics

1. The signed context is checked against the stored `app_runtime_generation`.
2. The transaction locks the generation row, then the company CRM row, and
   verifies the **exact** owner of the route vacancy using `FOR UPDATE`. A guessed
   real employer (`real_company_name`) is not accepted as the shortcut owner.
3. An existing per-user vacancy application is **claimed** by a new durable
   idempotency receipt without editing the application or its status history.
   A retry with that same receipt returns `replayed:true`.
4. A missing application is created along with its initial history and durable
   receipt in one transaction. The existing 10-applications-per-company limit
   remains active. Multiple pre-existing applications for one user and vacancy
   cause a conflict rather than arbitrary selection.
5. Following commit, the route re-reads the vacancy detail and checks that the
   user's linked application matches the receipt. It verifies the generation
   again before returning success. Post-commit projection errors cause an
   error response, not a false success; the same request can be retried.
6. Channel-owned vacancies can be used as the shortcut's authority even when
   their channel is absent from the companies/agencies catalog.

## Verification completed in this environment

- `go test -race ./...` and `go vet ./...`
- HTTP handler tests for both route aliases, correct response shape, sessions,
  origin, content type and body limits; error-code mapping and redacted DB errors.
- Deterministic SQL-driver tests for existing-application reuse, replay,
  mismatched payload, unknown or changed vacancy owner, ambiguous links,
  absence of new rows, and lock/replay ordering.
- Static public-repository source scan, ZIP and Git history validation.

## Mandatory staging gates

- Driver download and `go.sum` generation; build using `-tags mariadb`.
- Run read-only `db/vacancy-apply-preflight.sql` on a **private** MariaDB staging
  copy. Review duplicate user/vacancy links and ensure all relevant tables use
  InnoDB. Do not upload the private SQL dump or query results to GitHub.
- Repeat on live MariaDB: first application, reuse existing application without
  status/history mutation, network-loss replay, conflict on reused UUID with
  changed payload, concurrent presses, owner changed, deletion followed by
  retry, and runtime generation rotation during an in-flight request.
- Verify the unchanged React `VacancyActions` component against staging.
- Full backup/restore (including generation rotation), parser and scheduler,
  Google Calendar and the remaining business APIs must migrate before the
  production readiness gate can be opened.
