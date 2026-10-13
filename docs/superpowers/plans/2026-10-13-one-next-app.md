# Plan — The clinician zones collapse into one Next app (#933)

Spec: `docs/superpowers/specs/2026-10-13-one-next-app-design.md`

## Task 1 — the guarantee, first

- [x] `e2e/tests/navigation.spec.ts`: window marker survives OPD → Pharmacy →
      Lab via the rail. Run against the multi-zone build; record the failure.

## Task 2 — move

- [x] Pages into `apps/shell/app/{medicore,pharmacy,lab}` (D1 table).
- [x] Components and tests into `apps/shell/components/{medicore,pharmacy,lab}`.
- [x] `lucide-react` onto the shell's dependencies.
- [x] Delete `apps/medicore`, `apps/pharmacy`, `apps/lab`.

## Task 3 — links

- [x] `HmsShell` rail, panel, home links and dashboard cards → `next/link`;
      `next` peer dependency on `@helivanta/ui`.

## Task 4 — config and tooling

- [x] Shell rewrites, `Dockerfile.shell`, `images.yml`, `turbo.json`, env
      examples, `new-zone` (script, generator, lint reference), dev-script ports.
- [x] e2e harness: zone builds and zone routes removed.
- [x] Lockfile: importers removed, no unrelated churn, frozen install passes.

## Task 5 — docs

- [x] ADR-0010; ADR-0001 marked superseded in part.
- [x] `frontend.md` §1, §2, §6; `CLAUDE.md`; the frontend skill; README.
- [x] Comments: `session-renewal.tsx`, `idle-timer.ts`, `zones.ts`,
      `hms-shell.tsx`, `permissions-cache.ts`, eslint config.
- [x] 2026-10-12 e2e spec amended.

## Proof

- [x] Every D1 URL served through the production build.
- [x] `smoke.spec.ts` unmodified, full `make e2e` green.
- [x] `navigation.spec.ts` fails before, passes after.
- [x] `pnpm turbo lint type-check test build format:check` green; shellcheck clean.
