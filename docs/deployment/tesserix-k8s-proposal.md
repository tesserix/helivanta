# tesserix-k8s changes for HMS (proposal)

Phase 1 ships images only; this is the paste-ready plan for the infra PR.

## charts/apps/ additions

- `hms-api` — Go API (port 8080, healthz/readyz; needs APP/ADMIN_DATABASE_URL,
  NATS_URL, GIP_PROJECT_ID). Copy `mark8ly-platform-api` chart shape.
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

Create GIP tenant(s) for HMS via scripts/identity/enable-tenant-google-idp.py.
