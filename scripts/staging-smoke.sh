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
printf '%s\n' 'PASS: staging liveness OK; MariaDB reachable; production readiness remains closed'
