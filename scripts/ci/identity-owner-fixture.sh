#!/usr/bin/env bash
set -euo pipefail
[[ ${HELIVANTA_ENV:-} == dev && ${HELIVANTA_IDENTITY_FIXTURE:-} == true && ${ZITADEL_ISSUER_URL:-} == http://auth.tesserix.localhost:20080 ]] || {
  echo 'identity fixture requires explicit local development configuration' >&2
  exit 1
}
port=${HELIVANTA_PG_PORT:-5432}
[[ "$port" =~ ^[0-9]+$ ]] && (( port > 0 && port < 65536 )) || exit 1
case ${1:-} in
  setup)
    schema=$(mktemp)
    trap 'rm -f "$schema"' EXIT
    curl --fail --silent --show-error --location \
      https://raw.githubusercontent.com/tesserix/tesserix-k8s/95a2363928ef79d162e358332b79049aa51d1589/charts/apps/db-schema-bootstrap/schemas/global/global/onboarding_db.sql \
      --output "$schema"
    printf '%s  %s\n' c598be7b089b5ba977614b8f40aec9038c5d8f1f7fe9932d87eed40b565379f7 "$schema" | shasum -a 256 -c >&2
    exists=$(docker compose -f docker-compose.dev.yml exec -T postgres psql -U helivanta -d postgres -Atc "SELECT 1 FROM pg_database WHERE datname='onboarding_fixture'")
    if [[ "$exists" != 1 ]]; then
      docker compose -f docker-compose.dev.yml exec -T postgres psql -U helivanta -d postgres -v ON_ERROR_STOP=1 -c 'CREATE DATABASE onboarding_fixture' >&2
    fi
    docker compose -f docker-compose.dev.yml exec -T postgres psql -U helivanta -d onboarding_fixture -v ON_ERROR_STOP=1 -c "DO \$\$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname='onboarding') THEN CREATE ROLE onboarding NOLOGIN; END IF; END \$\$" >&2
    docker compose -f docker-compose.dev.yml exec -T postgres psql -U helivanta -d onboarding_fixture -v ON_ERROR_STOP=1 < "$schema" >&2
    ;;
  run)
    owner_dir=$(go -C backend list -m -f '{{.Dir}}' github.com/tesserix/onboarding-service/api)
    [[ -n "$owner_dir" && -f "$owner_dir/go.mod" ]] || exit 1
    exec go -C "$owner_dir" run -mod=readonly -tags identityfixture ./cmd/helivanta-identity-fixture \
      -manifest "$PWD/dev/zitadel/secrets/identity-fixture.json" \
      -pat-file "$PWD/dev/zitadel/secrets/helivanta-seed.pat" \
      -dsn "postgres://helivanta:helivanta@127.0.0.1:$port/onboarding_fixture?sslmode=disable"
    ;;
  *) echo 'expected setup or run' >&2; exit 1 ;;
esac
