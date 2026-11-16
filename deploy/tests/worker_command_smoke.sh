#!/usr/bin/env bash
# Изолированный synthetic smoke: отдельный disposable контейнер без сети.
set -euo pipefail
repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
container="itmap-worker-smoke-$$"
tmp_dir="$(mktemp -d)"
worker_pid=""
cleanup() {
  if [[ -n "$worker_pid" ]]; then kill "$worker_pid" 2>/dev/null || true; wait "$worker_pid" 2>/dev/null || true; fi
  docker rm -f "$container" >/dev/null 2>&1 || true
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

cd "$repo_dir"
CGO_ENABLED=0 go build -tags mariadb -o "$tmp_dir/itmap-worker" ./cmd/itmap-worker
chmod 755 "$tmp_dir/itmap-worker"
docker run -d --name "$container" --network none \
  -e MARIADB_ROOT_PASSWORD=ci_only_disposable_root \
  -e MARIADB_DATABASE=itmap_worker_staging \
  -e MARIADB_USER=itmap_worker_app \
  -e MARIADB_PASSWORD=ci_only_disposable_worker mariadb:10.11 >/dev/null
[[ "$(docker inspect -f '{{.HostConfig.NetworkMode}}' "$container")" == none ]]
ready=0
for _ in $(seq 1 60); do
  # Entrypoint starts a temporary socket-only server during initialization.
  # Wait for the final TCP listener and the application account instead.
  if docker exec "$container" mariadb --protocol=tcp -h 127.0.0.1 -u itmap_worker_app -pci_only_disposable_worker itmap_worker_staging --skip-column-names -e "SELECT 1" >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
[[ "$ready" == 1 ]] || { echo 'synthetic MariaDB did not start' >&2; exit 1; }
docker exec -i "$container" mariadb -u root -pci_only_disposable_root itmap_worker_staging < "$repo_dir/db/testdata/empty-schema.sql"
docker exec "$container" mariadb -u root -pci_only_disposable_root itmap_worker_staging -e \
  "INSERT INTO app_runtime_generation(id,generation) VALUES(1,'11111111-2222-4333-8444-555555555555'); INSERT INTO firecrawl_settings(id,scheduler_json) VALUES(1,'{\"enabled\":false}');"
docker cp "$tmp_dir/itmap-worker" "$container:/tmp/itmap-worker"

start_worker() {
  local log_file="$1"
  docker exec --user mysql \
    -e APP_ENV=staging -e DB_HOST=127.0.0.1 -e DB_PORT=3306 \
    -e DB_NAME=itmap_worker_staging -e DB_USER=itmap_worker_app \
    -e DB_PASSWORD=ci_only_disposable_worker \
    -e SESSION_SECRET=ci_only_synthetic_session_secret_32_characters \
    -e ITMAP_WORKER_ENABLED=1 -e ITMAP_PARSER_EXECUTION_ENABLED=1 \
    -e ITMAP_WORKER_STAGING_REAL_PROVIDERS=1 -e ITMAP_WORKER_HEALTH_ENABLED=1 \
    "$container" sh -c 'echo $$ > /tmp/itmap-worker-smoke.pid; exec /tmp/itmap-worker' >"$log_file" 2>&1 &
  worker_pid=$!
}
http_get() {
  timeout 3s docker exec "$container" bash -c 'exec 3<>/dev/tcp/127.0.0.1/18082; printf "GET %s HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n" "$1" >&3; cat <&3' _ "$1" 2>/dev/null
}
wait_live() {
  for _ in $(seq 1 30); do
    if http_get /healthz | grep -q '"live":true'; then return 0; fi
    if ! kill -0 "$worker_pid" 2>/dev/null; then return 1; fi
    sleep 1
  done
  return 1
}

start_worker "$tmp_dir/term.log"
wait_live || { echo 'worker did not become live' >&2; cat "$tmp_dir/term.log" >&2; exit 1; }
http_get /readyz | grep -q 'HTTP/1.1 503' || { echo 'disabled scheduler reported ready' >&2; exit 1; }
# A second instance cannot bind the diagnostic socket. It must exit before
# entering the engine, without disturbing the first process or leaking secrets.
if timeout 30s docker exec --user mysql \
    -e APP_ENV=staging -e DB_HOST=127.0.0.1 -e DB_PORT=3306 \
    -e DB_NAME=itmap_worker_staging -e DB_USER=itmap_worker_app \
    -e DB_PASSWORD=ci_only_disposable_worker \
    -e SESSION_SECRET=ci_only_synthetic_session_secret_32_characters \
    -e ITMAP_WORKER_ENABLED=1 -e ITMAP_PARSER_EXECUTION_ENABLED=1 \
    -e ITMAP_WORKER_STAGING_REAL_PROVIDERS=1 -e ITMAP_WORKER_HEALTH_ENABLED=1 \
    "$container" /tmp/itmap-worker >"$tmp_dir/collision.log" 2>&1; then
  echo 'second worker unexpectedly started on occupied diagnostics port' >&2; exit 1
else
  collision_status=$?
  [[ "$collision_status" != 124 ]] || { echo 'second worker did not exit' >&2; exit 1; }
fi
grep -q 'startup_or_runtime_failure' "$tmp_dir/collision.log" || { echo 'collision exit reason missing' >&2; exit 1; }
if grep -E 'ci_only_disposable_worker|ci_only_synthetic_session_secret' "$tmp_dir/collision.log" >/dev/null; then
  echo 'collision log exposed credentials' >&2; exit 1
fi
http_get /healthz | grep -q '"live":true' || { echo 'first worker stopped after collision' >&2; exit 1; }
counts="$(docker exec "$container" mariadb -u root -pci_only_disposable_root --skip-column-names --batch itmap_worker_staging -e 'SELECT (SELECT COUNT(*) FROM apify_runs)+(SELECT COUNT(*) FROM scheduler_attempts)+(SELECT COUNT(*) FROM scheduler_jobs)')"
[[ "$counts" == 0 ]] || { echo 'bind collision unexpectedly created work' >&2; exit 1; }
docker exec "$container" sh -c 'kill -TERM "$(cat /tmp/itmap-worker-smoke.pid)"'
for _ in $(seq 1 30); do
  if ! kill -0 "$worker_pid" 2>/dev/null; then break; fi
  sleep 1
done
if kill -0 "$worker_pid" 2>/dev/null; then echo 'worker did not stop on SIGTERM' >&2; exit 1; fi
if ! wait "$worker_pid"; then echo 'worker did not stop cleanly' >&2; exit 1; fi
worker_pid=""

start_worker "$tmp_dir/generation.log"
wait_live || { echo 'restarted worker did not become live' >&2; cat "$tmp_dir/generation.log" >&2; exit 1; }
docker exec "$container" mariadb -u root -pci_only_disposable_root itmap_worker_staging -e \
  "UPDATE app_runtime_generation SET generation='22222222-2222-4333-8444-555555555555' WHERE id=1"
for _ in $(seq 1 25); do
  if ! kill -0 "$worker_pid" 2>/dev/null; then break; fi
  sleep 1
done
if kill -0 "$worker_pid" 2>/dev/null; then echo 'worker did not stop on generation change' >&2; exit 1; fi
if wait "$worker_pid"; then echo 'generation change returned success' >&2; exit 1; fi
worker_pid=""
grep -q 'generation_changed' "$tmp_dir/generation.log" || { echo 'generation exit reason missing' >&2; exit 1; }
counts="$(docker exec "$container" mariadb -u root -pci_only_disposable_root --skip-column-names --batch itmap_worker_staging -e 'SELECT (SELECT COUNT(*) FROM apify_runs)+(SELECT COUNT(*) FROM scheduler_attempts)+(SELECT COUNT(*) FROM scheduler_jobs)')"
[[ "$counts" == 0 ]] || { echo 'isolated smoke unexpectedly created work' >&2; exit 1; }
echo 'PASS: isolated worker lifecycle; no network or paid operations'
