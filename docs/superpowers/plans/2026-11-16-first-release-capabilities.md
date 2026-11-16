# First-release capabilities implementation plan

Tracking: [#1728](https://github.com/tesserix/helivanta/issues/1728).

Date: 2026-11-16. Status: implementation in progress; unchecked items remain acceptance gates.

Design: [onboarding, Staff and PharmaConnect release capabilities](../specs/2026-11-16-first-release-capabilities-design.md).

## Delivery sequence

Deliver correct increments with explicit dark deployment and acceptance. Keep
application changes in Helivanta, desired infrastructure in `tesserix-k8s`, and
production promotions in `kargo-manifests`. Preserve unrelated checkout changes.
No local apps, containers or databases; local static checks, unit tests and
production builds are permitted. Database/browser acceptance runs in CI and the
existing prod/QA environment with synthetic tenants.

### 1. Pin and automate GrowthBook upgrades

- [ ] Inspect the existing infra platform-tools Warehouse, Stage, credentials,
      paths and promotion behavior before adding subscriptions. Identify ownership of
      every GrowthBook image override, including inline Argo Helm values.
- [x] Read-only inspection: running package version is `5.2.0`, matching the vendor
      latest stable release, but deployment uses `latest`. Existing platform-tools
      stage is failing; its direct main-push pattern must not be copied.
- [ ] Verify running/target image provenance and supported upgrade path; capture
      exact registry index/platform digests and previous recovery references.
- [ ] Capture prior deployment/configuration and independently recoverable Mongo
      backup/restore evidence. Do not log secrets or archive them in public Git.
- [ ] RED: add tests proving GrowthBook is discovered as a supported stable
      semver image, Freight resolves the same subscription, and promotion changes
      only the owned app version/digest. Prove floating tags are rejected.
- [ ] GREEN: add a dedicated GrowthBook Warehouse/Stage in the existing infra
      project, with exact image version/digest pins, supported current-minor patches,
      PR write-back and required post-merge Argo verification. Keep unrelated tools
      out of its Freight. Surface newer minor/major versions for reviewed range widening.
- [ ] Check seed behavior does not reset existing shared projects, features,
      users, SDK connections or environment values during an image-only rollout.
- [ ] Render Helm/Kustomize, run relevant infra tests, verify public repository
      visibility, commit/push and merge through normal CI.
- [ ] State account/project/context/namespace/target before the authorized rollout.
      Promote the specific compatible version through Kargo; verify app/SDK health,
      preserved data, project isolation and a recoverable failure procedure.
- [ ] Document automation: supported update range, verification requirements,
      pause behavior, major/schema approval gate and dependency on recoverable backup.

Do not upgrade unrelated platform tools or Mongo as a side effect. An unverified
upgrade path is a blocker for that rollout, not permission to force the change.

### 2. Freeze route, panel and dependency inventory

- [ ] Generate a matrix of every registered API method/path, UI page, navigation
      item and nested Staff/pharmacy panel. Assign baseline, capability, internal
      dependency or explicit continuation classification and an accountable owner.
- [ ] Trace account creation, employment review, staff login links, department
      postings, outlet grants, professional credentials and revocation. Retain all
      required independent-review screens in R1.
- [ ] Map hospital-owned and independent-pharmacy employer models separately.
- [ ] Reconcile the R2 matrix against Mahesh's case IDs and dependencies. Record
      which existing acceptance evidence applies to the exact deployed versions.
- [ ] Keep incomplete operational workflows dark; do not certify retail from
      presence of menu entries, handler registration or the names-selector proof.

### 3. Canonical release manifest and CI rules

- [ ] RED: test unknown keys, unknown routes, duplicate bindings, unclassified
      exports/legacy routes, dependency cycles and missing owner/evidence records.
- [ ] GREEN: add canonical `contracts/release-capabilities.json` and generators
      for Go/Zod/types. Generate bindings; do not maintain parallel hand-copied keys.
- [ ] Every optional group defaults off. Baseline auth/recovery is explicit.
- [ ] Validate page/panel/API consistency and baseline exceptions against the
      runtime registry; update generated contract references.
- [ ] Require release-flag owner, review/expiry date and removal or permanent
      entitlement decision; CI fails on undocumented or expired release controls.
- [ ] Add evidence records for implemented, CI-passed, deployed-dark,
      live-accepted, approved and enabled states, bound to version/digests/config.

### 4. Backend GrowthBook evaluation and API admission

- [ ] RED: evaluate off/missing/unknown flags, dependencies, provider errors,
      malformed payload, snapshot expiry, clock boundaries and deployment allowlist.
- [ ] GREEN: add a pinned compatible GrowthBook/OpenFeature Go integration using
      trusted request context and local evaluation. Refresh bounded snapshots without
      external network calls or DB transactions on the request hot path.
- [ ] Dedicated Helivanta project/environment/SDK reader; product-prefixed secret
      in OpenBao, namespace-bound ESO reader. Never expose management or SDK secrets.
- [ ] Add proposed platform capability read, canonical schemas and account-bound
      SDK method through Global API. Test exact subject/tenant response binding.
- [ ] Extend all route-declaration paths, not just ordinary `GET`/`POST`. Preserve
      membership, permission, establishment and handler-scope checks. Unknown route
      classification prevents boot or fails CI; never default allow.
- [ ] Cover direct product requests as well as Global API routes; do not let
      client headers select tenant, role, department or release cohort.
- [ ] Explicit off → 404; provider unavailable/over-age → 503 for new gated work;
      existing authz/status semantics remain intact. Baseline identity stays usable.
- [ ] Revalidate flags at new command admission; retain durable idempotency and
      attributable history. Classify audited status/recovery/continuation paths so
      accepted stock and financial work can finish safely after disablement.
- [ ] Measure p99 local evaluation overhead and record refresh/expiry metrics,
      decision failure counters and policy version without identity/PHI payloads.

### 5. Consistent shell, Staff and department experience

- [ ] RED: owner with every permission cannot see unreleased zones, pages,
      cards, phone links, shortcuts or nested Staff panels. A bookmarked page does
      not fetch business data before capability admission.
- [ ] GREEN: add account-bound capability state and intersect it with existing
      `visibleZones(can)`/page permissions. No optimistic full menu while loading.
- [ ] Clear requests, forms and cached results on account switch. Test late
      responses from tenant A cannot populate tenant B's view or re-enable controls.
- [ ] Present R1 Staff tasks together: named team accounts, staff records,
      required reviews, department assignment, pharmacy prerequisites and revocation.
      Avoid duplicate menus for the same task; preserve the current source of truth.
- [ ] Hide roster/leave/payroll/benefits and advanced panels independently of
      broad owner permissions. Do not remove required approval paths.
- [ ] R1 PharmaConnect shows setup/team readiness with honest blocked/pending
      states. R2 counter, stock, reports, returns and payables stay dark until accepted.
- [ ] Essential security settings, sign-out and recovery are always reachable
      for an authenticated authorized person. Choose a safe released landing page.
- [ ] Public onboarding is deployment gated; issued verification/password links
      retain completion behavior. Existing verified users do not receive verification
      again, and expired/reused links keep the established refusal/resend behavior.

### 6. Automated test matrix

| Test layer                  | Required cases                                                                                                                                                                                                                     |
| --------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Unit                        | Manifest schema/dependency closure, bounded local evaluation, TTL boundary, off/missing/error behavior, account-switch races, navigation and nested panel visibility                                                               |
| Go integration with real DB | Feature-on still denies unauthorized role/outlet/department; cross-tenant refusal; stale membership; no unauthorized side effect/audit leakage; preserved idempotent command outcomes                                              |
| Global/product contract     | Equivalent off behavior through Global API and direct product API; pagination/count/export/download/legacy path coverage; canonical payload and error schemas                                                                      |
| Provider                    | Dedicated Helivanta environment, known on/off flag, unrelated project isolation, bad SDK secret, provider restart, stale snapshot, invalid update and recovery                                                                     |
| CI browser                  | Desktop/phone owner and multiple staff; deep links; hidden panels; account chooser; stale response; no unrelated module APIs before admission                                                                                      |
| Live synthetic R1           | Fresh hospital signup and independent-pharmacy signup; email/password/sign-in; multiple staff accounts; independent review; scoped assignments; staff login; suspension/revocation; no clinical authority inherited from ownership |
| Live synthetic R2           | Mahesh's G-1–G-5 and applicable case groups; positive journeys plus negative scope, duplicate, dependency failure, payment/refund and stock-safety outcomes                                                                        |
| Upgrade                     | Exact tag/digest, compatible schema, preserved shared features, SDK decisions across rollout, verified backup/restore and failure pause                                                                                            |

Commands are executed only when their implementation slice exists. Expected
application checks include the affected pnpm unit/typecheck/lint/build tasks,
Go unit/race/vet/lint and real-DB CI suites, canonical generator `--check`, and
`go -C backend run ./cmd/contractdocs` plus `-check`. Record exact commands and
results in each PR; remove generated local `.next` after a successful build.

### 7. Merge, deploy dark, accept, then enable

- [ ] Commit/push every finished increment with the repository-matched identity.
      Do not combine unrelated infra changes from existing checkouts.
- [ ] Merge normal green checks; verify both repositories remain public.
- [ ] Deploy exact images/configuration through Kargo; inspect migrations,
      workload readiness, Argo and Kargo health. No imperative pod-image edits.
- [ ] Run R1 acceptance with distinct synthetic hospital/pharmacy tenants and
      several staff identities. Preserve existing tenants, passwords and MFA.
- [ ] Record R1 owner/reviewer decisions and enable only its passing groups for
      the selected pilot tenants. Verify UI and direct API both change consistently.
- [ ] Run R2 acceptance and obtain pharmacy/compliance/finance/product decisions;
      fill Mahesh's sign-off record before enabling its retail group closure.
- [ ] Verify disable/drain/re-enable, provider outage and account switching on
      actual deployed versions. Save private screenshots without public PII.
- [ ] Publish exact enabled scope and explicitly list dark/unaccepted groups.

## Completion rule

The plan is complete only when a fresh owner can perform the advertised released
journey, each invited staff member has exactly the intended tested access, hidden
features cannot be reached through any client path, GrowthBook evaluation is
isolated/fail-closed, and the pinned Kargo upgrade path is verified. Documentation,
passing unit tests or an infrastructure upgrade alone do not satisfy this rule.

## Progress recorded on 2026-11-16

Infrastructure is merged in tesserix-k8s #1678 and kargo-manifests #74. Read-only
inspection verifies GrowthBook `5.2.0` pinned by digest, Healthy/Synced Argo state
and Healthy/Ready `kargo-infra/growthbook-prod` with verified Freight. Exact
references are recorded in ADR-0072. This does not verify a Helivanta reader or
application flags, shared-provider restore, or the unrelated legacy tools stage.

Application candidate: [#1730](https://github.com/tesserix/helivanta/pull/1730).
Canonical route/page/panel definitions, local server evaluation/admission,
account-bound snapshots, shell/deep-link/navigation gates and public signup
admission are implemented. Security and existing onboarding completion remain
baseline. Receiving is classified under supplier collaboration, outside pharmacy
foundation. Account switching closes baseline account settings as well as
business pages until the transition ends.

Local evidence: API 1,457 tests, UI 323 tests, shell 2,065 tests; manifest and
source-review inventory tests/checks; web lint/typecheck/format; API/shell builds;
release and contractdocs race tests, selected platform/bootstrap admission tests,
Go vet and affected-package golangci-lint. Both account-settings regressions were
observed failing before correction. Local database-backed package tests failed
because Docker is absent; their CI acceptance remains required. No local app,
container or database was launched. Generated build artifacts are removed after
validation. CI has already caught and corrected toolchain ordering and the
router source-review hash change; final CI results remain a gate.

Next: complete nested Staff panel/API argument admission, provision an isolated
Helivanta project/environment reader through OpenBao/ESO, and perform the stated
provider/database/browser/live acceptance before promoting the application.
No capability has been enabled and no existing tenant or identity has been reset.
