# Staff profile master connection plan

1. Reproduce missing language, exit-reason and contract-type references through
   the existing enrollment endpoint before changing application code.
2. Evolve the canonical model/generator and forward-only typed-reference
   migration, concurrent indexes and distinct compatibility marker.
3. Reuse the owner resolver with presence handling and canonical labels; verify
   history, retries, isolation, stale/null/clear behavior and older-writer refusal.
4. Connect the three UI controls, preserving original profile fields and legacy
   text; contract type remains selection-only. Verify component and actual UI.
5. Generate/check contract docs and canonical outputs; run affected race tests,
   vet/lint/build, frontend gates and bounded query evidence. Commit/push/merge
   only on passing checks. Production migration/rollout needs named approval.

## Validation checkpoint — 2026-10-28

Implementation and generated contracts are present in the isolated feature
worktree. The real-DB/HTTP regression group passed after its recorded failing
baselines, including null compatibility, old-writer protection, canonical labels,
historical pins, scope denial, retry and metadata-only event assertions.

Commands completed:

- `go test ./internal/modules/workforce -run '^TestProfileMasters' -count=1`
  passed (8.650s), using the existing disposable Docker test engine.
- `go test -race ./internal/modules/workforce ./internal/globalapi ./internal/archtest ./cmd/contractdocs`
  passed: Workforce 404.352s, Global API 34.028s, architecture 39.691s,
  contractdocs 4.937s.
- `go vet ./...`, `golangci-lint run ./...` (zero findings), and
  `go build ./...` passed from backend.
- `go run ./cmd/contractdocs` and `go run ./cmd/contractdocs -check` passed.
- Node 24 `generate-workforce-enrollment.mjs --check` and `git diff --check` passed.
- Configured `pnpm turbo lint type-check test build format:check` passed all
  30 tasks: 1,037 shell, 389 API, 236 UI, 62 configuration and 9 emergency-access
  tests. Initial new test selectors failed typecheck; exact-name regular
  expressions corrected the unsupported Testing Library option. A subsequent
  lint failure from escaped spaces was corrected before the passing full run.
- Node 24 `node --test scripts/*.test.mjs` passed all six tests.

The authorized local database accepted migrations 0040/0041 through `cmd/migrate`,
including RLS/provenance lint. The feature API runs on local 8099, Global API on
8199 and a configured production shell on 4399. Unrelated local services were
preserved. These are local changes, not production migration evidence.

Actual Staff Admin UI acceptance now saved all three references, reloaded and
reopened them, retired each selected master from v3 to v4, retained all recorded
v3 labels, saved an unrelated preferred-name edit and confirmed retention after
another full reload. Read-only SQL independently verified staff version 11,
three recorded v3 references and three retired current v4 masters. All business
writes used the UI; database checks used read-only transactions.

Initial local sign-in attempts failed because the resumed validation build and
navigation used the wrong client/origin. The saved fixture specifies the
port-specific idle-timeout client and `http://helivanta.localhost:4399`; restoring
both resolved sign-in without any IdP configuration or permission changes.
Chrome's earlier `ERR_BLOCKED_BY_CLIENT` is no longer an active blocker.
An independent reviewer login showed no hospital access in Staff directory,
zero add/edit staff buttons, and no master table or add-record button in HR setup.
The existing reviewer link remains revoked; no new role or membership was granted. There is no approval stage in this master/profile
contract; these checks do not replace approvals for access, payroll or letters.

The full backend coverage gate failed after host-side Colima forwarding became
unreachable. Its first observed Emergency assertion (expected 202, received 503)
passed unchanged in isolation after local forwarding recovery (6.793s). Docker
was healthy inside the VM; no VM restart or container stop was performed. A
private Docker socket and loopback service forwarding were restored using the
existing SSH connection. All-package coverage is being rerun with two package
workers on the 4 GiB VM; no tests are skipped. This does not establish the cause
of every first-run failure or promise a passing full gate.

The two-worker full coverage retry completed with 54 passing packages and only
Practitioner failing: four pre-existing source-review fixture scenarios received
409 during setup. Workforce passed at 83.5% coverage (350.086s), Global API at
86.7%, and contractdocs at 79.4%. Independent [PR #1402](https://github.com/tesserix/helivanta/pull/1402)
uses the database owner clock for those test observations; the focused wire
timestamp regression was observed red with host time and green after restoration
(4.907s). The full Practitioner race/coverage package passed (116.383s, 84.6%).
No application guard, policy or migration changed in that prerequisite. The
complete coverage gate subsequently passed with that test correction and the
initial profile implementation. Response compatibility work followed that pass;
the post-projection complete gate is tracked separately below.

A fresh administrator relogin also retained all three recorded v3 pins. The
private employment-section screenshot records this later check. The existing
local Docker IPC socket was restored from its unowned stale socket without
restarting the VM or stopping containers.

## Versioned response and browser compatibility checkpoint

The owner response tests first failed because default profiles included new
properties and invalid selectors still created staff (expected 400, received
200). The new editor regression first failed because an unconfirmed API response
opened the form. After implementation, all ProfileMasters cases passed (10.253s)
and all 17 enrollment/directory/setup component tests passed.

Canonical projection metadata now generates the selector constants, response
marker type and the original profile projection. GET/PUT default to the legacy
shape; `profile_masters=v1` opts into the extension. Unsupported/empty/repeated
selectors reject before mutation. Actual HTTP comparisons prove every original
field remains, and legacy amendments/retries preserve all three pins. New UI
cache keys include the version, and missing response confirmation blocks editing.

Post-change checks passed: all 30 web gates (1,038 shell tests; API 389, UI 236,
configuration 62, emergency-access 9), full backend vet/lint (zero findings), API
build, contractdocs generation/check, canonical check, six Node script tests,
explicit docs/contract formatting and diff checks. Full `go build ./...` passed.
The post-projection `GOFLAGS=-p=2 ./scripts/coverage-gate.sh` completed successfully
with all 55 tested packages passing and no coverage-floor failures. Workforce
passed in 336.835s at 83.5%, Practitioner in 120.113s at 84.6%, Global API in
31.495s at 86.7% and contractdocs in 5.397s at 79.4%. Docker was configured through
the recovered local test socket with the container socket override unchanged.

Actual browser checks used the same synthetic hospital, administrator and exact
recorded client/origin, through Global API:

- The final UI against the previous supervision API showed the explicit
  unavailable message with zero Save buttons. No business mutation was attempted.
- Restoring the updated API and clicking Try again loaded all three recorded
  references. An unrelated amendment saved through the opted-in editor.
- The actual pre-feature production UI build loaded the default response,
  displayed its original language text control and no contract-type control,
  saved an unrelated preferred-name edit and reloaded successfully.
- Restoring the final UI displayed that saved amendment and all three recorded
  v3 references. Read-only SQL independently confirmed staff version 13, all
  pins at v3 and current masters retired at v4. The database assertions account
  for pg bigint values being returned as strings by the Node client.

Runtime versions were switched only for this local compatibility check; the
final UI and API are restored. No VM/container restart, credential or identity
provider change, new grant, production migration or rollout occurred. Private
screenshots and SQL evidence remain in the local fixture directory.

Prerequisite #1402 merged as `3238d2b85b3fcbe7cb5ab8e27346231a334c841d` after all
Go, web, scripts, browser E2E, images and secret checks passed. The feature remains
unmerged and broad Staff issues remain open.

The independent reviewer checks were repeated against the final restored build:
no hospital directory access, zero add/edit staff controls, and no HR setup table
or add-record button. The role's pre-existing non-Staff membership remains; this
is not evidence for every organization role. The new profile fields introduce no
new permissions or authority from classification.
