# Staff membership recovery: local UI acceptance

2026-10-31. Runtime source: `e848beafe96c8b5a4817591042e2b35c278f08f4`,
branch `feat/staff-membership-recovery`. This is local synthetic QA, not a
production release or full Access/RBAC acceptance. Read with the
[progress tracker](staff-management-progress.md),
[acceptance ledger](staff-management-acceptance.md) and ADR-0049.

## Actual environment

The old UI and API were serving earlier sources. Their process/source metadata
was captured before switching. A private mode-0600 custom-format database backup
was taken before candidate startup. Both older API processes were stopped to
avoid mixed-source consumers against the same fixture database. The candidate
API serves port 8399 through the existing Global API on 8199; the candidate UI
serves the already registered `http://helivanta.localhost:4399` callback origin.
API and Global API liveness/readiness return 200. Database, NATS, OpenFGA,
Temporal, object-storage emulator and identity-provider targets were checked as
local. Payment-provider dispatch remains disabled in this fixture.

Startup applied the embedded canonical migrations to the localhost fixture.
Read-only `schema_migrations` receipts confirm IAM 0014 through 0019, including
membership commands, the typed concurrent candidate index, SELECT-only projection
reader, first-enrollment receipts and ended-membership recovery admission.
Canonical SQL/registration remains in Git and the generated runtime inventory.
0016 is a typed index migration: its empty SQL hash does not mean the registered
index definition is absent. Private receipts retain the canonical registrations,
hashes and application times for future environment replay/review. Historical
SQL must not be edited to make a future correction.

## Normal UI journey

All HMS membership writes below were performed in the UI. No direct database
write, injected session, role expansion or hospital grant was used for them.

| Scenario                                         | Observed result                                                                                                            |
| ------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------- |
| Existing synthetic Admin normal password sign-in | Dashboard and Staff access administration available                                                                        |
| Empty enrollment                                 | Account/reason/acknowledgment errors; no successful submission                                                             |
| New synthetic identity first enrollment          | Ordinary membership receipt recorded; retained after full reload                                                           |
| Recipient normal sign-in                         | Dashboard available; no hospital team access                                                                               |
| Recipient direct Staff access and payroll pages  | Explicit administration and payroll denial                                                                                 |
| Admin final ordinary-role removal                | Attributed command receipt and recoverable history recorded                                                                |
| Removed recipient fresh normal sign-in           | Login callback denies admission with the existing no-access message; no HMS workspace opens                                |
| Admin explicit recovery                          | New ordinary membership; removal and recovery history both survive full reload                                             |
| Recovered recipient fresh normal sign-in         | Dashboard opens; access administration and payroll remain denied                                                           |
| Existing restricted reviewer normal sign-in      | Administration/payroll remain denied; existing six-row hospital directory stays read-only with no enrollment/edit controls |

The disposable IdP identity was created by the already authorized localhost setup
fixture, with random credentials saved only in private mode-0600 JSON. Its verified
email flag was fixture setup. This does **not** establish recipient contact
verification, provider provisioning/reconciliation or A01 completion. No password
was displayed or committed. No employment approval or independent employment
review was performed, and no existing reviewer permission was expanded.

A read-only database check confirmed one current `staff_member` role, generation
2 with member count 1, a recovered membership ID different from the original,
one retained enrollment and both immutable removal/recovery receipts. The
recipient has zero hospital grants, Staff login links and assignment proposals.
The audit reader's RLS-bypass status was verified so zero counts were not merely
filtered rows. Application isolation remains enforced by the runtime services.

The browser used serial normal sign-outs/sign-ins. This proves the observed fresh
login and UI journeys; it does not prove an old recipient cookie remains denied
while a different isolated browser session performs removal. Keep that broader
UI race acceptance open alongside the existing signed-session server tests.
One recovery-login observation timed out; inspecting the same tab confirmed its
completed dashboard navigation. The login was not restarted or bypassed.

## Compound expiry evidence

The expanded real PostgreSQL/NATS/OpenFGA command test uses two reviewed grants
with different expiry times. A product write begins while both are valid; the
earlier source expiry cancels it, rolls it back and leaves the other grant valid.
The restored implementation passes with race detection (25.230 s). A mutation
that selects the later expiry fails for the expected reason: command deadline
instead of scoped-authority denial (27.510 s). The mutation was restored. This
uses the actual database validity clock, not a fabricated product response.

Commands: `go test -race ./internal/modules/iam -run
'^TestCompoundAssignmentCommand$' -count=1`, `go vet ./internal/modules/iam`,
`golangci-lint run ./internal/modules/iam/...`, and the candidate `go build`.
Prior full IAM/authz/tenantdb/architecture results at the same production source
remain in the tracker; this continuation changes only the regression test/docs.
No new route, permission, template, SQL migration or FGA model is introduced.

## Evidence and remaining scope

Private evidence is under the existing `.helivanta/staff-management-20261027`
bundle: `staff-recovery-candidate-runtime-20261031.json`, the pre-candidate dump,
local migration receipts, browser recipient JSON, read-only database evidence,
and enrollment/denial/reviewer screenshots. Do not copy credentials, callback
codes, dumps or raw private records into public PRs. The source hashes and
normalized QA outcomes are sufficient for public tracking.

Remaining gates include exact-head CI/review, generic privileged enrollment and
legacy compatibility, complete provider creation/contact verification/recovery,
compound projection/lock-wait combinations and query-plan/latency acceptance,
actual scoped HR owner/storage/UI integration, reviewed current-person basis,
independent employment decisions, legacy-cohort classification and all effective
renewal/suspension/resumption/resignation/termination/contract-expiry/rehire and
cross-module cutoff journeys. No A01–A12 item or broad Staff area is newly marked
complete. No production rollout, migration, issue closure or release tag occurred.
