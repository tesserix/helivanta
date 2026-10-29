# Implementation plan — #1389

Worktree: `/private/tmp/helivanta-opd-1389`.
Branch: `codex/1389-opd-safety-journey`.

- [x] Inspect owning APIs, consumers, role/grant rules, UI labels and e2e setup.
- [x] Add an explicitly synthetic drugmaster fixture and owner-CLI seed target.
- [x] Add the real clinician encounter/safety journey and no-grant journey.
- [x] Wire the fixture and recording flag into the e2e-only CI process.
- [x] Observe a meaningful assertion fail before the completed journey passes.
- [x] Run Playwright against isolated real services, plus affected lint, type,
      script and build checks; record actual evidence and remaining limitations.

Keep off Claude's B-2c work and #1371/#1388. This change activates no production
clinical policy and performs no production clinical transactions.

## Local validation

- Both Playwright journeys passed against isolated Postgres/NATS/OpenFGA/Zitadel,
  the real API and Global API, and the production-built shell under Node 24.
  Separate local ports/OIDC fixture kept Claude's stack untouched.
- The recording-disabled run was observed failing at the explicit
  `recording_enabled` assertion after reaching the real encounter/safety UI.
  Enabling it only on that synthetic API allowed the full journey to pass.
- `pnpm --filter @helivanta/e2e type-check`, e2e lint/format and script lint pass.
- `pnpm turbo lint format:check type-check` passes (21 tasks).
- The owner-CLI fixture seed and re-seed both pass. All 25 script tests pass, including nine clinical-fixture guards. Guard tests reject non-dev,
  non-local/unintended databases, and query-string routing/identity overrides
  before any owner command can run.
- Webpack production build passes. The local browser command uses the existing
  separate OIDC fixture on port 4399 and the Global API on port 18081; committed
  CI uses the standard main e2e stack and the same owner seed target.

No application route, event schema, migration or production clinical default was
changed. Full CI and whole-suite acceptance must pass on the final PR revision.

## Separate observation for the B-2c owner

At base `a554f2dc`, real registration events were rejected by pharmacy's
`registeredPatientConsumer` because it expects the full subject in `event.Type`
while patientmaster publishes `PatientRegistered`. The isolated API logged the
rejection and dead-lettering. MediCore's consumer and the clinical browser
journey work. This unrelated pharmacy code is deliberately not changed here.

## Main synchronization

Accepted main through `3d7d68c4` (including merged B-2c #1448) was fast-forwarded before PR publication. No unmerged Claude changes were included.
