#!/usr/bin/env bash
# Verifies that the whole HMS stack is up and healthy locally.
# Usage: make verify-local   (after `make dev` in another terminal)
set -uo pipefail

fail=0

check() {
  local name="$1" url="$2"
  if curl -fsS --max-time 5 "$url" >/dev/null 2>&1; then
    printf '  ok    %-24s %s\n' "$name" "$url"
  else
    printf '  FAIL  %-24s %s\n' "$name" "$url"
    fail=1
  fi
}

echo "Infrastructure:"
docker compose -f docker-compose.dev.yml ps --status running --format '  ok    {{.Service}}' || fail=1
check "openfga"          "http://localhost:8090/healthz"
check "gip emulator"     "http://localhost:9099/"

echo "Backend:"
# The API exposes /healthz and /readyz (backend/internal/httpserver/server.go)
# — not /health and /ready.
check "api health"       "http://localhost:8080/healthz"
check "api ready"        "http://localhost:8080/readyz"

echo "Frontend zones:"
# Each zone app sets basePath in next.config.ts (e.g. medicore is served
# under /medicore, not "/") — the bare root 404s.
check "shell    (4301)"  "http://localhost:4301/login"
check "medicore (4302)"  "http://localhost:4302/medicore"
check "pharmacy (4303)"  "http://localhost:4303/pharmacy"
check "lab      (4304)"  "http://localhost:4304/lab"

echo
if [ "$fail" -eq 0 ]; then
  echo "All checks passed. Log in at http://localhost:4301/login"
  echo "  test@hms.dev       / password123  (tenant_admin)"
  echo "  pharmacist@hms.dev / password123  (pharmacist)"
else
  echo "Some checks failed. Common causes:"
  echo "  - 'make dev' not running, or still starting (Next.js takes ~20s)"
  echo "  - NODE_AUTH_TOKEN unset: export NODE_AUTH_TOKEN=\$(gh auth token)"
  echo "  - Docker not running (infra and backend tests both need it)"
  echo "  - /readyz failing on openfga: check 'docker compose -f docker-compose.dev.yml logs openfga'"
fi
exit "$fail"
