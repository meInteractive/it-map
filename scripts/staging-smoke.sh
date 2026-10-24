#!/usr/bin/env bash
# Read-only smoke check for an explicitly configured Go staging URL.
set -euo pipefail
url="${1:-http://127.0.0.1:8080}"
case "$url" in
  http://127.0.0.1:*|http://localhost:*|https://*) ;;
  *) echo 'Only loopback HTTP or HTTPS staging URL is allowed' >&2; exit 2 ;;
esac
url="${url%/}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
live_status="$(curl -sS --connect-timeout 3 --max-time 10 -o "$tmp/live" -w '%{http_code}' "$url/health/live")"
[[ "$live_status" == 200 ]] || { echo "liveness returned HTTP $live_status" >&2; exit 1; }
python3 - "$tmp/live" <<'PY'
import json,sys
with open(sys.argv[1],encoding='utf-8') as f:
    assert json.load(f).get('status')=='alive', 'invalid liveness response'
PY
ready_status="$(curl -sS --connect-timeout 3 --max-time 10 -o "$tmp/ready" -w '%{http_code}' "$url/health/ready")"
# Until cutover readiness MUST stay closed. This test should not accidentally
# deploy the still-incomplete app by interpreting /live=200 as production ready.
[[ "$ready_status" == 503 ]] || { echo "pre-cutover readiness returned unexpected HTTP $ready_status" >&2; exit 1; }
python3 - "$tmp/ready" <<'PY'
import json,sys
with open(sys.argv[1],encoding='utf-8') as f:
    doc=json.load(f)
assert doc.get('status')=='not_ready', 'staging readiness unexpectedly open'
assert doc.get('reason')=='business_api_not_migrated', 'MariaDB not connected or unavailable'
PY
# Optional isolated CI-only auth contract. No real credentials or remote domains.
if [[ "${ITMAP_SMOKE_AUTH:-0}" == '1' ]]; then
  case "$url" in
    http://127.0.0.1:*|http://localhost:*) ;;
    *) echo 'Auth smoke requires loopback HTTP and synthetic CI credentials' >&2; exit 2 ;;
  esac
  [[ -n "${ITMAP_SMOKE_LOGIN:-}" && -n "${ITMAP_SMOKE_PASSWORD:-}" ]] || {
    echo 'Missing synthetic smoke credentials' >&2; exit 2;
  }
  anonymous_status="$(curl -sS --connect-timeout 3 --max-time 10 -o "$tmp/anon" -w '%{http_code}' "$url/api/session")"
  [[ "$anonymous_status" == 401 ]] || { echo "Anonymous session returned HTTP $anonymous_status" >&2; exit 1; }
  anonymous_api_status="$(curl -sS --connect-timeout 3 --max-time 10 -o "$tmp/anon-api" -w '%{http_code}' "$url/api/companies/list")"
  [[ "$anonymous_api_status" == 401 ]] || { echo "Anonymous company API returned HTTP $anonymous_api_status" >&2; exit 1; }
  login_status="$(curl -sS --connect-timeout 3 --max-time 10 -D "$tmp/login-headers" -o "$tmp/login-body" -w '%{http_code}' \
    --data-urlencode "login=$ITMAP_SMOKE_LOGIN" --data-urlencode "password=$ITMAP_SMOKE_PASSWORD" "$url/auth")"
  [[ "$login_status" == 303 ]] || { echo "Synthetic login returned HTTP $login_status" >&2; exit 1; }
  # Transfer only the cookie pair. Avoid displaying the signed token in CI logs.
  python3 - "$tmp/login-headers" "$tmp/cookie" <<'PYTHON'
import sys
from http.cookies import SimpleCookie
headers = open(sys.argv[1], encoding='utf-8').read().splitlines()
values = [line.split(':',1)[1].strip() for line in headers if line.lower().startswith('set-cookie:')]
jar = SimpleCookie()
for val in values:
    jar.load(val)
if not jar:
    raise SystemExit('login did not issue a session cookie')
name, morsel = next(iter(jar.items()))
open(sys.argv[2], 'w', encoding='utf-8').write(name + '=' + morsel.value)
PYTHON
  session_status="$(curl -sS --connect-timeout 3 --max-time 10 -H "Cookie: $(cat "$tmp/cookie")" -o "$tmp/session" -w '%{http_code}' "$url/api/session")"
  [[ "$session_status" == 200 ]] || { echo "Authenticated session returned HTTP $session_status" >&2; exit 1; }
  python3 - "$tmp/session" "$ITMAP_SMOKE_LOGIN" <<'PYTHON'
import json, sys
with open(sys.argv[1], encoding='utf-8') as f:
    doc = json.load(f)
assert doc.get('name', '').casefold() == sys.argv[2].casefold(), 'unexpected authenticated account'
PYTHON
fi
printf '%s\n' 'PASS: staging liveness OK; MariaDB reachable; production readiness remains closed; optional authentication gate OK'
