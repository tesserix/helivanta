# HMS Authorization — Final Whole-Branch Review Fix Wave

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development.
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix every issue raised by the final whole-branch review of the OpenFGA authorization
branch (`feat/authz-spec`), which found seam bugs between the 14 tasks of
`docs/superpowers/plans/2026-08-11-hms-authorization.md`. This plan's tasks are independent
review findings, not a build-up of new functionality — read each task's "Investigate first"
step before writing any code, because the current state of the named files is the ground truth,
not this document.

**Context documents (read, do not re-derive):**
- Design spec: `docs/superpowers/specs/2026-08-11-hms-authorization-design.md`
- Original execution ledger: `.superpowers/sdd/2026-08-11-hms-authorization/progress.md`
- Standards: `docs/standards/backend.md`, `docs/standards/frontend.md`, and this repo's `CLAUDE.md`

## Global Constraints

- Modules never import other modules' packages (depguard + `internal/archtest`); cross-module
  data flows only via events.
- Every tenant table keeps forced RLS with `USING` and `WITH CHECK`.
- Runtime DB access only via `tenantdb` accessors; `WithAdmin` only from its allowlist.
- slog only (logrus banned); wrap errors with `%w`.
- `403` = member lacking permission; `404` = cross-tenant; `503 authz_unavailable` = FGA
  failure. Never fail open — this applies to grants AND (after this plan) revokes.
- The string `MedCora` must never appear in the repo.
- Commit messages: conventional commits, single line, no signatures, no `Co-Authored-By`.
- Backend tests requiring Postgres/OpenFGA use testcontainers and need Docker; run as
  `cd backend && go test ./<pkg>/...`.
- Before the wave is done: `cd backend && go build ./... && go vet ./... && go test -race ./...
  && ./scripts/coverage-gate.sh` green; `golangci-lint run ./...` clean; `pnpm turbo lint
  format:check type-check test build` green from repo root; `make dev && make verify-local &&
  make e2e` green.
- Functions under 50 lines.

---

### Task 1: Canonicalize `Principal.TenantID` at the parse boundary

**Why first:** other tasks (the reconciler fix, the integration test) depend on tenant IDs
being consistently cased; fixing this first avoids two tasks touching the same seam.

**Investigate first:** read `backend/pkg/authn/gip.go` (where the `tenant_id` claim is parsed
into `Principal`), `backend/pkg/authz/middleware.go` (how `p.TenantID` reaches `Resolve`),
`backend/pkg/authz/client.go` (the string-prefix matching in `Resolve`/role/perm object
builders), `backend/internal/platform/reconcile.go` (writes canonical lowercase from
`tenant_id::text`), and `backend/internal/modules/iam/module.go` (the `iam-fga-sync` consumer,
which currently publishes `p.TenantID` — the raw claim — instead of a parsed/canonical UUID) and
`backend/internal/modules/iam/me.go` (`normalizeTenantID`, which currently patches this
downstream of the parse).

**Requirement:**
- In `backend/pkg/authn/gip.go`, at the point the `tenant_id` claim is parsed into `Principal`:
  parse it as a UUID (`github.com/google/uuid`) and store the canonical lowercase string form
  (`uuid.String()`) on `Principal.TenantID`. If the claim is not a valid UUID, reject the token
  (same error path as other malformed-claim rejections in that file — do not invent a new error
  shape).
- This closes the deferred finding from Task 2 of the original plan: "no validation/documentation
  that tenantID excludes `/`" — a canonical UUID string cannot contain `/`, so no separate check
  is needed, but add a one-line comment at the parse site noting this invariant is why
  `Resolve`'s object-id prefix matching is safe.
- Remove `normalizeTenantID` from `backend/internal/modules/iam/me.go` as redundant — but first
  grep the whole repo for other callers/uses of tenant ID normalization and confirm none of them
  still need it. If something else depends on it, say so in your report instead of silently
  leaving two normalization paths.
- Confirm `iam/module.go`'s `iam-fga-sync` consumer now publishes/writes the canonical
  `p.TenantID` (it will, transitively, once the claim is canonicalized at parse time) — verify by
  reading the code path, not by assumption.
- Add a test in `backend/pkg/authn` asserting a mixed-case `tenant_id` claim (e.g.
  `AbC12345-...` uppercase-mixed UUID) yields the canonical lowercase form on `Principal`, and a
  test asserting a non-UUID claim is rejected.

**Acceptance:**
- `cd backend && go test ./pkg/authn/... ./pkg/authz/... ./internal/modules/iam/...`
- No remaining reference to `normalizeTenantID` unless you documented why it must stay.

---

### Task 2: Go/TypeScript `public` sentinel parity

**Investigate first:** read `backend/pkg/authz/authz.go` (`PermissionSet.Has`, unconditional
true for `Public`), the frontend `can()` implementation (search `packages/ui/src` and
`packages/api` for `can(` and `/iam/me/permissions` consumption), and
`packages/ui/src/zones.ts` (`visibleZones`, the page filter's special-case for the string
`"public"`, and the zone filter's special-case for `key === "dashboard"`).

**Requirement:**
- Make the frontend `can()` treat `"public"` as always allowed, mirroring Go's `Has`. Do this at
  the single source (the hook/function itself), not by re-special-casing at call sites.
- In `packages/ui/src/zones.ts`: remove the `key === "dashboard"` special case from
  `visibleZones` now that `can("public")` correctly resolves — the existing `"public"` string
  special-case in the page filter should also collapse into using the corrected `can()` if it was
  only there to work around the same bug (verify before removing; if it does something else,
  keep it and say so).
- Fold in the deferred finding: a zone whose pages are ALL filtered out by permission should
  itself be filtered out of `visibleZones` (not shown as an empty zone in nav).
- Tests: a hypothetical second public zone renders in `visibleZones` (proves the fix, not just
  the removed special case); a zone with zero visible pages does not appear in `visibleZones`.

**Acceptance:**
- Frontend test suite covering `can()` and `visibleZones` passes: run the package's vitest
  command (check `package.json` in the owning package for the exact script).

---

### Task 3: Module generator no longer invents a role mapping

**Investigate first:** read `backend/scripts/new-module.sh` end to end, focusing on the
generated `Permissions()`/`Grant` template that currently grants a placeholder permission to
`RoleNurse`.

**Requirement:**
- Change the generated template to emit an **empty** `Roles: []authz.Role{}` (or equivalent) with
  a comment directly above it: `// TODO: choose the roles that hold this permission`.
- Verify the generator still produces a module that builds and its generated test passes:
  generate a scratch module (pick an unused name, e.g. `scratchgen`), run its build/tests, then
  delete every file/dir it created and confirm `git status` is clean afterward (no orphaned
  registration in `cmd/api/main.go` or `internal/archtest/arch_test.go` — the generator should
  either not touch those for an ungenerated/undeleted module, or you must revert what it touched).

**Acceptance:**
- Scratch module generate → build → test → delete cycle is clean; paste the commands and output
  in your report.

---

### Task 4: Small hardening items (independent, batch together)

**Investigate first, item by item — each is a separate file/behavior:**

1. **`platform.GrantsFor` dedupe.** Read `backend/internal/platform` for `GrantsFor` (or
   equivalent registry aggregation function) and its oracle/matrix test. Two modules declaring
   the same `Permission` currently produce duplicate grants silently (the oracle test keeps only
   the last). Add a registry-level uniqueness check — fail loudly (return an error or panic at
   boot, matching how other registry invariants in this codebase fail) when the same
   `Permission` is declared by two different `Grant` entries. Add a test proving the duplicate is
   caught.

2. **Unknown `role_key` in `Reconcile`.** Read `backend/internal/platform/reconcile.go`. It casts
   any `role_key` string straight to `authz.Role` and writes a tuple. `knownRole()` (find it —
   likely in `backend/pkg/authz` or the HTTP handler layer) only guards the HTTP path; the seed
   script writes rows via raw SQL and bypasses it entirely. Make `Reconcile` skip and
   `slog.Warn` (with tenant and role_key context) any `role_key` that is not one of the known
   `authz.Role` constants, instead of writing a tuple for it. Add a test: seed a Postgres row
   with an unknown role_key, run `Reconcile`, assert no tuple is written for it and the known
   rows still are.

3. **`ListObjects` truncation detection.** Read `backend/pkg/authz/client.go`'s `Resolve` and
   `ListRoles`. OpenFGA's `ListObjects` caps result length (check the SDK/response type for the
   page-size constant or documented cap). When a result's length hits that cap, `slog.Warn` that
   truncation may have occurred (include tenant/caller context). Correct `Resolve`'s doc comment,
   which currently promises complete results — it should state the truncation caveat.

4. **Harden `TestWithAdminIsOnlyCalledFromTheAllowlist`.** Find this test (likely
   `backend/internal/archtest` or near `tenantdb`). It currently scans source text for the
   literal `.WithAdmin(` call syntax, which a method value (`f := db.WithAdmin`) would evade
   without tripping the check. Extend the scan (AST-based via `go/ast`, which this repo's
   archtests likely already use elsewhere — check for existing AST helpers before writing new
   ones) to also catch `db.WithAdmin` referenced as a bare selector expression (method value),
   not just as a call. Add a test fixture that would previously have evaded the check and confirm
   the hardened test catches it, then confirm it still passes against the real (compliant)
   codebase.

**Acceptance:** each of the 4 items has its own test proving the fix; `go test -race
./backend/internal/platform/... ./backend/internal/archtest/... ./backend/pkg/authz/...` green.

---

### Task 5: Make `Reconcile` authoritative — revocation must converge

**Investigate first:** read `backend/internal/platform/reconcile.go` fully (both `Reconcile` for
memberships and `ReconcileTenant`/whatever function handles permissions — the review found
`docs/standards/backend.md` mis-credits `ReconcileTenant` with memberships when that's
`Reconcile`'s job; confirm the actual split before editing docs in Task 8), `backend/pkg/authz/
client.go` for what read/list capability the SDK client already exposes (`authz.Client`) and what
the OpenFGA `Read` API offers, and the idempotent `delete` helper already used for tuple removal
elsewhere (search for existing delete/revoke code paths — `iam` module's revoke handling is a
good starting point).

**Requirement — make `Reconcile` (and the permission-side reconciler, whichever function that
is) delete tuples Postgres no longer backs:**
- Add a read/list capability on `authz.Client` using the OpenFGA SDK's `Read` API (add this to
  `authz.Client` itself, or to `platform.TupleWriter`/a companion interface — pick whichever
  keeps `authz.Client`'s existing public surface coherent, and say which you chose and why).
- For a given tenant, read its existing `role:` and `perm:` tuples from OpenFGA, compare against
  what Postgres currently backs (roles the tenant's members currently hold; permissions the
  system role definitions currently grant), and delete any tuple Postgres does not back.
- **Scope deletions strictly to the tenant being reconciled** — filter by the tenant's object-id
  prefix (using the same canonical tenant ID from Task 1) before issuing any delete. Never touch
  another tenant's tuples. Add a test that reconciling tenant A does not delete or read tenant
  B's tuples (seed both, reconcile A only, assert B's tuples untouched).
- Make the whole operation idempotent and safe to run at every boot on every replica — running
  `Reconcile` twice in a row with no Postgres changes between must produce zero deletes and zero
  errors the second time.
- **Convergence tests (must-have, not optional):**
  1. Seed a tuple in OpenFGA that has no backing row in Postgres. Run `Reconcile`. Assert the
     tuple is gone afterward.
  2. Seed a tuple that IS backed by a Postgres row. Run `Reconcile`. Assert it survives.
  3. Run `Reconcile` twice back to back on unchanged Postgres state; assert the second run is a
     no-op (no errors, tuple set identical before/after).

**If, after investigating, you judge this too large or too risky for a single task** — say so
explicitly in your report with your reasoning, do not attempt a half-implementation, and stop
before making any code changes. In that case Task 8 (docs) must instead correct
`docs/standards/backend.md`'s "rebuildable from Postgres" claim to state plainly that the
reconciler is additive-only and revocation is not self-healing — flag this clearly in your report
so the plan owner can route Task 8 accordingly. Prefer attempting the fix; only stop if a real
risk (e.g. an OpenFGA SDK limitation you cannot work around, or a scale/performance problem you
can't resolve within this task) blocks you.

**Acceptance:** the three convergence tests above pass; a full reconcile run against a
multi-tenant fixture proves no cross-tenant deletion.

---

### Task 6: Tenant switching — implement the token re-mint (do not remove the feature)

**Investigate first:** read `backend/internal/modules/iam/me.go`'s `POST /v1/iam/me/tenant`
handler in full (the membership gate, the false "re-minted by the shell" comment, the 403/503
paths), `backend/pkg/authn/gip.go` (the `TokenVerifier` abstraction and how the Firebase Admin
auth client is currently built/held — likely private to that package or behind an interface in
`Deps`), `packages/ui/src/tenant-picker.tsx` (current no-op reload + toast), `apps/shell/lib/
firebase.ts` (existing Firebase JS SDK config/init pattern), `apps/shell/app/login/page.tsx`
(existing sign-in flow), and `apps/shell/app/api/session/route.ts` (existing pattern for
exchanging an ID token for the `hms_session` cookie).

**Requirement:**

Backend:
- Add a narrow interface (e.g. `CustomTokenMinter` or similar — name it to fit the file's
  existing naming conventions) exposing only `CustomTokenWithClaims(ctx, uid, claims) (string,
  error)`, backed by the Firebase Admin Go SDK's `auth.Client`. Do NOT leak the Firebase client
  itself through `Deps` — wire this narrowly, alongside (not replacing) the existing
  `TokenVerifier` abstraction. Confirm `authn`'s existing tests still pass unmodified (or with
  minimal, justified changes) after this addition — the review is explicit that this must not
  break `TokenVerifier`.
- In `POST /v1/iam/me/tenant`: keep the existing membership gate (403/503 behavior unchanged) —
  **the gate must run before any minting call.** On success, mint a custom token for the caller's
  UID carrying `tenant_id: <target tenant>` as a custom claim, and return it in the response
  instead of (or alongside, if the existing response shape is still useful) the current echo.
- Test: the endpoint returns a token only after the membership check passes, and fails closed
  (no token, correct error) when the underlying `ListRoles`/membership check errors — this must
  be a real test against the handler, not just eyeballed.

Frontend:
- `TenantPicker` currently lives in `packages/ui` (app-agnostic). If completing the exchange
  requires shell-only Firebase config, split or move the component so `packages/ui` stays free of
  app-specific config — state exactly what you did (moved wholesale to `apps/shell`? split into a
  UI-only presentational piece plus a shell-side container?) in your report.
- Reuse the existing sign-in/session-post pattern from `apps/shell/app/login/page.tsx` and
  `apps/shell/app/api/session/route.ts` — do not invent a parallel flow. On a successful tenant
  switch: call `signInWithCustomToken` (Firebase JS SDK) with the token from the backend, obtain
  a fresh ID token, POST it to the existing session route so `hms_session` is replaced, then
  reload.
- Test: a frontend test proving the picker performs the sign-in + session-POST exchange (not
  merely a reload) — mock the Firebase SDK call and the session POST, assert both happen in
  order before the reload/toast.

E2E (attempt, but do not force it):
- `test@hms.dev` is currently seeded as `tenant_admin` in exactly one tenant. Add a second
  membership for `test@hms.dev` in a second tenant to `scripts/seed-dev.mjs` if that's where dev
  seeding lives (confirm the actual seed file/mechanism first).
- Add or extend an e2e journey test that actually switches tenants and asserts the switch took
  effect (e.g. a subsequent authenticated call reflects the new tenant, or tenant-scoped data
  visibly changes) — not just that the toast appeared.
- If a genuine end-to-end proof turns out impractical within reasonable complexity, say so
  explicitly in your report with the specific obstacle, rather than weakening the assertion to
  something that would pass without the fix working.

**Acceptance:** backend test proving gate-before-mint and fail-closed; frontend test proving the
real exchange; e2e proof or an explicit, reasoned statement of impracticality.

---

### Task 7: One integration test through the real HTTP authorization path

**Depends on:** Task 1 (canonical tenant IDs) and ideally Task 5 (reconciler) being complete,
since this test reconciles a tenant and expects the tuple set that produces to be correct.

**Investigate first:** read `backend/internal/archtest` structure and existing container-backed
tests (search for `testinfra.StartOpenFGA` — likely already used by the original plan's Task 2
tests) to match the existing test-harness pattern, `backend/pkg/authz/middleware.go`
(`authz.Middleware`), `backend/internal/platform` (`platform.Router`, `authz.Require`), and pick
one real module (with real routes already registered in `cmd/api/main.go`) to mount for the test.

**Requirement:**
- Boot a real `authz.Client` against `testinfra.StartOpenFGA`.
- Reconcile one tenant (using the real reconciler from Task 5).
- Grant the tenant's test principal one role via the real reconciled tuples (not a fake
  resolver).
- Mount the real `authz.Middleware` and the chosen real module's routes on a real `platform.
  Router` / Gin engine.
- Assert: a request with a permitted route for the granted role returns 200; a request with a
  route requiring a permission the role does not hold returns 403. One route per role is enough
  — this is a seam test, not a second permission matrix.
- Put the test where it naturally fits — `internal/archtest` if that package already hosts
  cross-cutting integration tests, otherwise a new integration package; justify your choice
  briefly in your report.
- Keep it fast: minimize container/test setup to what this one test needs.

**Discrimination check (must document in your report, both directions):**
- Temporarily unmount `authz.Middleware` from the test's router (comment it out or bypass it) —
  run the test — confirm it FAILS. This proves the test actually depends on the middleware being
  present, i.e. it would have caught the original "every /v1 route 500s" bug class.
- Restore the middleware — run the test — confirm it PASSES.
- Paste both run outputs (or the pass/fail summary) into your report.

**Acceptance:** the test exists, passes with the middleware mounted, and demonstrably fails with
it unmounted (documented, then reverted).

---

### Task 8: Correct the spec and standards docs where they overclaim

**Depends on:** the outcome of Task 5 (whether the reconciler became authoritative or was judged
too risky) — read Task 5's report/ledger entry before writing this task's changes, since D2's and
the standards doc's correct wording depend on that outcome.

**Investigate first:** read `docs/superpowers/specs/2026-08-11-hms-authorization-design.md` in
full, locating sections D2, D7, and D9 exactly, and `docs/standards/backend.md`'s description of
`Reconcile`/`ReconcileTenant` and the reconciler.

**Requirement — in `docs/superpowers/specs/2026-08-11-hms-authorization-design.md`:**
- **D2:** currently claims revocation takes effect on the next request with "no staleness
  window." Reword to describe actual behavior: revocation lands only when the outbox event
  carrying the revoke is processed (i.e., there IS a staleness window, bounded by outbox/consumer
  lag) — and, if Task 5 shipped, additionally note the reconciler now self-heals lost revoke
  events on the next reconcile pass (state that bound too, e.g. "at next boot/reconcile," if
  that's how it actually runs); if Task 5 did NOT ship, say plainly that a lost revoke event
  currently has no self-healing path.
- **D9:** currently claims `pkg/authz` "takes scope as a parameter" and the FGA model "reserves
  the scoping shape." Neither is true. Replace with: there is no scope parameter today; because
  permissions are FGA objects rather than model relations, adding scope later is a bounded
  migration (name the actual pieces it would touch: model, reconciler, tuple rewrite, `Resolve`
  signature) while `Require(perm)` and every route declaration stay untouched. Be explicit that
  existing tuples would need rewriting as part of that future migration — do not imply it's free.
- **D7:** the shipped system made FGA the read-side authority for membership — `/me/tenants` and
  the tenant-switch membership gate read role tuples, not the `iam_members` table directly.
  Verify this claim against the actual code (`iam/me.go` and wherever `/me/tenants` is
  implemented) before writing it — if the implementation actually reads `iam_members` directly in
  places, say precisely which paths read which source. Write the accurate version into D7,
  since today this inversion exists only in a code comment, not in the spec.

**Requirement — in `docs/standards/backend.md`:**
- Fix the line crediting `ReconcileTenant` with re-applying memberships — that's `Reconcile`'s
  job; `ReconcileTenant` (confirm actual name) is permissions only. Get the actual function names
  and responsibilities right by reading `reconcile.go`, not by trusting the current doc text.
- Align the reconciler description with Task 5's actual outcome: either describe it as now
  authoritative (deletes what Postgres doesn't back, scoped per-tenant, idempotent) or as
  additive-only with revocation not self-healing — whichever Task 5 actually shipped. Do not
  leave the current "rebuildable from Postgres" wording standing unmodified either way.

**Acceptance:** a reviewer reading only D2, D7, D9, and the `docs/standards/backend.md`
reconciler section can accurately predict the system's actual revocation and scoping behavior
with no code-reading required.

---

## Also produce

After all tasks are complete and the final whole-branch review is clean, write a report to
`.superpowers/sdd/2026-08-11-hms-authorization/final-fix-report.md` covering: what changed per
task (1-8, mapping to the original review's FIX 1-7 plus the smaller items), discrimination
evidence for Task 7, anything judged too large/risky and why (Task 5's outcome in particular),
exact verification commands run with their output/summary, and a self-review against the Global
Constraints above.
