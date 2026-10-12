# Plan — e2e runs against production builds (#976)

Spec: `docs/superpowers/specs/2026-10-12-e2e-against-production-builds-design.md`

## Task 1 — build and run

- [x] `scripts/e2e-web.sh`: `build` (three shell variants, three zones,
      assembled into `.e2e-web/<name>/`) and `start <name> <port>`
      (`node server.js`).
- [x] `.e2e-web/` gitignored.

## Task 2 — `e2e.sh`

- [x] Stop this repo's 4301–4304, remembering a running dev stack; build;
      start the main web stack.
- [x] Phases 2 and 3 start their shell build; main web apps keep running.
- [x] Cleanup stops what it started and restores `make dev-web` only if it
      found one.

## Task 2b — ingress and test hook (spec D5, D6)

- [x] `dev/e2e/Caddyfile` + compose `e2e-ingress` (profile e2e); shells on
      internal ports; upstream keepalive 4 s.
- [x] `readStoredIdToken`; `window.__hmsUserManager` and its type removed.

## Task 3 — CI

- [x] e2e job starts `dev-api` only and waits on the API only.

## Task 4 — proof

- [x] Repeat smoke and journey against the production stack locally.
- [x] Full `make e2e` green locally; tree clean afterwards.
- [x] Dev stack restored when present, not started when absent.
- [ ] CI green.

## Gates

- `make lint-shell`; `pnpm turbo lint format:check`.
