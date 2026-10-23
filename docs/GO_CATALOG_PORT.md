# Sprint 1.1.09 (partial): catalog search

The first end-to-end catalog handler has been ported from
`server/services/catalog-projection.js` (Sites v316):

- `GET /api/companies/search?q=<text>` requires a verified session;
- the query is Unicode-aware and limited to 256 characters;
- the service reads `app_catalog.data_json` from MariaDB at `id=1`;
- response shape remains `{ "matches": [{"name": ..., "sector": ...}] }`;
- searches name, sector, roles, metro/address, job titles, revenue metadata
  and the text fields of `vpnAi`;
- DB failure returns 503 without exposing internal SQL or data to the client.

The repository is read-only and does not modify employer identity tables.
Go unit and HTTP tests run on synthetic fixtures. Actual MariaDB execution is
**not** yet verified (the external driver and local MariaDB server are missing
in the offline preparation environment). The remainder of 1.1.09 (list, map,
detail, agencies, state overlays, stable IDs and permissions) remains open.
