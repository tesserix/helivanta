# tesserix-k8s changes for HMS (proposal)

Phase 1 ships images only; this is the paste-ready plan for the infra PR.

## charts/apps/ additions

- `hms-api` — Go API (port 8080, healthz/readyz; needs APP/ADMIN_DATABASE_URL,
  NATS_URL, ZITADEL_ISSUER_URL, ZITADEL_CLIENT_ID, SESSION_SIGNING_KEY).
  Copy `mark8ly-platform-api` chart shape. Note SESSION_SIGNING_KEY is a real
  secret and the API refuses to boot without it (#838) — it needs #45.
- `hms-shell`, `hms-medicore` — Next.js standalone (ports 4301/4302). Copy
  `mark8ly-admin` chart shape.
- `hms-postgres` — copy `mark8ly-postgres`; run `dev/init-db.sql` equivalent
  (hms_app role, NOBYPASSRLS) via init job.
- `hms-openfga` — copy `mark8ly-openfga` (Phase 2 wires the model).
- NATS: reuse the existing `nats` chart (JetStream on).

## Routing

One host per tenant; path routing: `/` → hms-shell, `/medicore` → hms-medicore,
`/api/*` → hms-api (strip nothing; API serves `/v1/*`, edge maps `/api/(.*)` → `/$1`).

## services.yaml

Add `hms-api` (backend, go), `hms-shell`, `hms-medicore` (frontend, node)
under a new `hms` appGroup with the standard ci.yml/release.yml workflows.

## Identity

Provision the HMS org, project and `hms-web` client on the shared Zitadel at
auth.tesserix.app (ADR-0006; topology in
`docs/superpowers/specs/2026-08-15-zitadel-tenancy-topology-design.md`).
Hospitals are NOT Zitadel entities — they are HMS tenants.
