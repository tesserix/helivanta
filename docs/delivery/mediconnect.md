# MediConnect delivery and issue evidence

Research date: 2026-10-24. Source baseline: `cc63b6f80103fb53782dcd02c45de7440bfd384b`.
Status: account and unverified link-request APIs deployed from `30efb51` via
[PR #1318](https://github.com/tesserix/helivanta/pull/1318) and
[connector PR #1525](https://github.com/tesserix/tesserix-k8s/pull/1525).
This does not establish verified patient access or patient-only onboarding. All issues below remain open. OTP work was set aside
by the user; account APIs reuse the existing verified session boundary.

## Confirmed direction

Clients enter the Global API; MediConnect owns patient-facing account and engagement
workflows. Platform patient identity, MediCore scheduling/clinical records and
LabConnect report release retain their ownership. No shared-table clinical reads,
second patient master or inferred grants from a phone number are permitted.

The user selected **email and WhatsApp OTP; SMS disabled** on 2026-10-24.
This supersedes #551's WhatsApp-to-SMS fallback requirement for this release.
Disabled means no SMS requests, fallback, queued delivery or provider invocation.
Email fallback requires an independently verified email belonging to the same
account; a caller-supplied replacement address is not recovery authority.

## Assigned issue sequence

The following issues were assigned to `sam123ben` without removing other assignees.
The order is dependency-driven, not a promise that a draft is deployable.

| Stage                                 | Issues                                                                                                             | Required exit evidence                                                                                                                                                                  |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Patient authentication                | [#551](https://github.com/tesserix/helivanta/issues/551)                                                           | Email/WhatsApp delivery and verification; SMS stays disabled; expiry, replay, rate limits, wrong recipient, provider outage and staff-MFA isolation; approved notice and launch locales |
| Accounts and proxies                  | [#37](https://github.com/tesserix/helivanta/issues/37), [#552](https://github.com/tesserix/helivanta/issues/552)   | Reviewed hospital registration binding; self/guardian/caregiver action separation; immediate revocation and expiry; cross-tenant/hospital denial; profile cache separation              |
| Consent and rights                    | [#556](https://github.com/tesserix/helivanta/issues/556)                                                           | Versioned purpose-specific notices and receipts; revocation enforced before asynchronous propagation; acknowledgement state, grievance owner and approved response policy               |
| Portal composition                    | [#75](https://github.com/tesserix/helivanta/issues/75)                                                             | Patient-only web/mobile session, profile switching, truthful unavailable/stale states, accessibility and real client→Global→owner journeys                                              |
| Discovery and booking                 | [#77](https://github.com/tesserix/helivanta/issues/77), [#101](https://github.com/tesserix/helivanta/issues/101)   | Verified practitioner source; owner-held availability, concurrent slot safety, idempotent booking and payment recovery; no fabricated availability                                      |
| Reports                               | [#73](https://github.com/tesserix/helivanta/issues/73)                                                             | Owner-signed release and amendment versions; live disclosure checks; audited view/download/share; no draft or revoked report disclosure                                                 |
| Connectivity and offline              | [#553](https://github.com/tesserix/helivanta/issues/553), [#554](https://github.com/tesserix/helivanta/issues/554) | Measured journey budgets; encrypted profile/device cache; explicit offline freshness and revocation limits; purge acknowledgements                                                      |
| Accessible speech                     | [#555](https://github.com/tesserix/helivanta/issues/555)                                                           | Approved locale content/assets; user-initiated playback, no generated clinical instructions; screen-reader and real-device verification                                                 |
| Reminders and continuing relationship | [#557](https://github.com/tesserix/helivanta/issues/557), [#78](https://github.com/tesserix/helivanta/issues/78)   | Consent rechecked at send/read; authenticated actions despite shared-handset links; expiry, cancellation, provider replay and quiet-hours tests                                         |

Prerequisites remain independently owned: identity/auth #35/#36, mobile #11,
strings/locales/content #544/#545/#550, consent/privacy #60/#62/#66/#449,
medical records #72, practitioner verification #273/#274, scheduling #102,
report approval #143, delivery #147, provider adapters #486/#487 and payments #456.
Do not close these merely because a MediConnect consumer exists. Existing owners
were not removed. AI triage/summaries and unrelated commercial features are not
part of this issue batch.

## Source-verified gaps and corrections

- Account slice adds the MediConnect module and explicit Global API owner mapping.
  It does not add a patient session or link a hospital registration.
- `backend/internal/modules/iam/loginui.go` implements password/TOTP staff login.
  `loginclient/sufficiency.go` deliberately refuses unsupported factors. Adding
  patient OTP must not remove those checks or grant staff membership.
- #551's Keycloak/Flutter/Redis descriptions are stale against current
  Zitadel/React Native/Valkey direction. Native AmbulanceConnect is not a
  MediConnect app.
- #551's proposed automatic linkage on a single phone/demographic match conflicts
  with IN-ID-01/04/05 and repository instructions. OTP proves control of a
  destination; it does not prove entitlement to any clinical registration.
- `patientmaster/{patients,hospital,access}.go` provides hospital-scoped staff
  registration and access, not patient/proxy authentication or consent lifecycle.
- `medicore/visits.go` creates visits with a nullable patient ID, not appointments,
  slot holds or a validated patient care relationship. Do not reuse visit creation
  as a booking endpoint.
- `lab/orders.go` stores a result string with pending/completed status. It has no
  patient-release/version/withdrawal protocol. `result_ready.v1` is not a portal
  release authorization. #73 cannot safely consume it as a released report.
- #37/#75/#556 overlap on consent ownership. Implement one platform consent
  authority; MediConnect composes its patient interface and retains no competing
  consent master.
- #557's signed message link does not itself prove a patient identity on a shared
  handset. State-changing actions require current account/profile authorization.

## OTP provider evidence

Primary version-pinned technical sources inspected:

- [Zitadel v4.15.3 challenge contract](https://github.com/zitadel/zitadel/blob/v4.15.3/proto/zitadel/session/v2/challenge.proto):
  native email challenge with send-code/return-code modes; phone challenge supports
  `return_code`, allowing custom server-side transport without sending an SMS.
- [Zitadel v4.15.3 session contract](https://github.com/zitadel/zitadel/blob/v4.15.3/proto/zitadel/session/v2/session_service.proto):
  email/phone OTP checks are single-use; `CheckUser.login_name` does not search
  email/phone and directs consumers to the user service. Never assume a phone is
  a unique login name or a patient identity.

These contracts establish API capability, not successful real delivery or
passwordless enrolment. A disposable-identity integration test must prove first
registration, enrolled-method prerequisites, verified factor freshness and
session completion before enabling either channel.

Read-only infrastructure discovery found:

- GCP `tesseracthub-480811`, account `unidevidp@gmail.com`, context
  `gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`.
- Zitadel Resend SMTP documented in `tesserix-k8s/docs/zitadel.md`: sender
  `auth@tesserix.app`, STARTTLS port 587, dedicated Secret Manager reference
  `prod-zitadel-resend-api-key`. No secret values were accessed or printed.
- The `zitadel-bootstrap` reconciliation CronJob's last successful time was
  `2026-10-24T09:00:18Z`; its source checks SMTP settings and active state. This
  is configuration evidence, not an inbox-delivery test or approval of the
  Tesserix sender for Helivanta patient communications.
- The only current Helivanta identity application found was `helivanta-web`.
  Patient authentication needs a separately scoped identity/application/session
  contract, not a downgrade of its existing staff MFA policy.
- No Secret Manager names matching WhatsApp/WABA/Meta Cloud were found. This is
  not proof that no provider account exists under another name. The existing
  `notification-service` deployment does not establish a reviewed WhatsApp
  authentication adapter; its named GitHub repository was not accessible.

Needed for WhatsApp activation: provider selection/configuration reference,
WABA and sending-number identifiers, approved authentication template and locale,
secret references for send/webhook credentials, verified delivery callback
contract and consented test recipients. Do not paste credentials into issues.
Needed for email activation: approved sender/branding, a test inbox, first-user
verification proof and sender delivery validation. Do not reconfigure the shared
SMTP service or rotate its credentials for this feature.

## Domain policy review

Apply IN-ID-01/04/06/07/08; IN-CL and IN-DX criteria belong to the clinical owners.
Record tenant/hospital, state/UT, service, launch locales, clinical/privacy review
owners, approved identity/proxy evidence and patient-record release rules.
No legal basis, universal retention duration, guardian exception or release policy
is activated by this design.

The repository's primary-source register remains the starting point. A fresh
2026-10-24 attempt to fetch MeitY DPDP Rules failed DNS resolution; the ABDM policy
PDF returned 403. The legal text was **not reverified** in this session. In
particular, do not copy the issues' blanket DPDP commencement/consent assertions
into production policy. The register documents phased commencement and requires
review of the Act's separate notification and subsequent amendments.

## Release and closure gates

Production auto-promotion was held by kargo-manifests #60 because the unrelated
India-only migrations have 15 incompatible rows across eight tables. New work
must not remove that hold, bypass a migration or delete records to make CI green.
Prepare the concrete migration/rollout with bounded DDL, restore rehearsal and
post-write recovery before requesting any necessary specific live approval.

For each issue: map every acceptance criterion to a test or named review; observe
red before green; run affected unit/integration/typecheck/lint/build checks;
exercise the actual UI→Global API→owner→database path; verify negative scope,
stale state, duplicate, provider outage and timeout-after-commit cases; record
CI and deployment evidence. Close only when that issue's stated definition of
done is met. A schema, stub, mock provider or documentation PR cannot close a
patient-facing delivery issue.

The initial review lacked Docker. The account implementation provisions a local
Colima `helivanta-tests` runtime for real PostgreSQL/NATS tests; provider tests
remain outside this slice. See the account evidence below.

## Account API evidence — local implementation

The account/preference portion of #37 is implemented in the `helivanta-mediconnect`
worktree. #37 remains open and assigned to Sam123ben. This does not complete
profile links, consent, rights requests, onboarding or the complete MediConnect API.
The user requested API-only work and set OTP aside. Auth console sign-in is not
patient-record linkage authority.

- `POST /v1/mediconnect/account`, `GET /v1/mediconnect/account` and
  `PATCH /v1/mediconnect/account/preferences`; explicit Global API `mediconnect`
  owner with no fallback. Contract: [account API](../api/mediconnect.md).
- Forced tenant-and-subject RLS, UUIDv7 account IDs, unique identity per tenant,
  optimistic preference versions, strict bounded input, no-store responses,
  append-only audit, and transactional ID-only account-created outbox event.
- Ten named account tests exercise real PostgreSQL/NATS, including concurrent
  creation, cross-account/tenant isolation, membership outage, stale writes,
  outbox rollback, audit-disclosure failure and Global API-to-owner HTTP.
- Observed red: missing account routes, missing preferences route and unknown
  Global API product. Green after implementation. Registry/owner count and event
  classification gates also detected the additions and were updated explicitly.
- `go test -race ./internal/modules/mediconnect -coverprofile=...` passed,
  **84.2% statement coverage**. No in-memory repository substitutes or test skips.
- `go test -race ./internal/globalapi ./cmd/contractdocs` passed.
- `go vet ./internal/modules/mediconnect/... ./internal/globalapi/... ./internal/bootstrap/...`
  and `go build ./...` passed. `make lint-go` passed with zero issues; final
  changed-package lint also passed. Contract generation and `-check`, authored
  document Prettier checks and `git diff --check` passed.
- Full backend `GOFLAGS=-p=2 ./scripts/coverage-gate.sh` completed with one
  failure: unchanged IAM `TestCacheIsRaceFreeUnderConcurrentReadsAndInvalidations`
  exceeded its 90-second harness deadline at filler entry 7771. All other packages
  passed, including architecture, Global API, MediConnect, clinical, pharmacy,
  workforce and shared libraries. The complete IAM package subsequently passed alone with race detection
  and coverage on 2026-10-25; the original aggregate invocation remains failed.
  This stress test migrates
  only `0003_iam` and does not execute MediConnect code.
- Local Docker is provided by the isolated Colima `helivanta-tests` profile;
  no production data or services used.

Next clinical-access dependency: a reviewed process and accountable reviewer for
binding a verified subject to a hospital-scoped patient registration. A shared
phone/email/demographic match cannot grant access. The user selected independent hospital registrar evidence review on 2026-10-25;
consent remains separate. The implementation evidence below supersedes that
workflow-choice blocker. No implicit clinical access is introduced.

## Unverified link-request API evidence — local implementation

The next bounded portion of #37 records, retrieves, lists and withdraws an
account's unverified hospital/patient relationship claims. Seven MediConnect
routes are now registered. This is intake only: no verified link, proxy grant,
patient disclosure or hospital verification processor is implemented. #37 and
#552 remain open. The exact contract is in [MediConnect API](../api/mediconnect.md).

- Additive `0002_mediconnect` migration enforces tenant/subject isolation,
  account ownership, immutable claims and terminal withdrawal; no production
  migration has been executed.
- Observed missing-route failures before implementing submission and list.
  Tests exercise peer/cross-tenant isolation, missing account, strict input,
  pagination, concurrent submission/withdrawal, audit failure and immutable SQL
  history. Replayed submission cannot reopen withdrawal.
- Real HTTP Global API → owner → PostgreSQL tests cover account creation and
  link intake, including forged identity headers and peer denial. Reads/list/
  submission replay fail closed when audit insertion fails.
- Final `go test -race ./internal/modules/mediconnect ./internal/archtest
./internal/globalapi ./cmd/contractdocs -coverprofile=/tmp/mediconnect-final.cover`
  passed on 2026-10-25. MediConnect statement coverage is **87.0%**; Global API
  coverage is **86.4%**. Log: `/tmp/mediconnect-final-tests.log`.
- The architecture pagination gate initially flagged the singleton request GET;
  it is now documented in the explicit singleton allowlist. The collection
  remains registered through `platform.ListRoute` and has a tested cursor.
- The prior full-backend aggregate IAM timeout remains recorded above; these
  targeted passing checks do not turn that earlier aggregate run green.

## Accessibility preference API — #37 partial

The API now supports optional account-owned text size, contrast and motion
preferences in the existing version-guarded PATCH. Omitted/null settings preserve
saved values; an explicit all-system object resets them. Existing callers remain
compatible. These are stored presentation preferences; UI rendering and clinical
profile-specific preferences require their corresponding client/authority work.

Migration `0003_mediconnect` is additive, constrained and compatible with prior
binaries. Existing accounts retain identity, version and prior preferences. The
first round-trip test failed with 400 before implementation, then passed with a
persisted response. Regression tests cover invalid nested values, legacy/null
preservation, resets, tenant/subject isolation, concurrent writers, update/read
audit failure, migration upgrade and real Global API forwarding. Exact validation
and rollout evidence are recorded on the implementing PR.

The earlier local aggregate IAM timeout is historical: both full source PR CI
and post-merge CI passed for `30efb51`, including Go coverage and browser E2E.
Production API/connector/shell reached two ready replicas each; public probes
verified authentication rejection, Via forwarding, unknown-route 404 and the
shell-to-owner mesh boundary. These probes do not certify authenticated patient
workflows. The user-approved confirmed Pakistan test cleanup cleared all 12
country preflight checks; [PR #1319](https://github.com/tesserix/helivanta/pull/1319)
records protected recovery and audit evidence. Automatic promotion stays disabled.

## Accessibility release verification — 2026-10-25

PR #1320 merged as `1223142b6204281b3b4c7d9a8314b0ff3dda4ca9`.
All PR checks, image build and post-merge CI passed. Manual Kargo promotion
`prod.01m5q5hxn8ec2pd1cnjjppfyb2.8d7125b` succeeded for Freight
`8d7125bcf335557c5cc5b88c8f4bf8e9819d7518`. API, Global API and shell are
Synced/Healthy, 2/2 ready. API/connector image digest is
`sha256:3cf6b050a6900b45957cc7547b6c1cbe4b4e01bb57a7aba5ff2810f657817004`;
shell digest is
`sha256:6345e95f20100501952d7af99036be1853ec62dba3de485964e7eabe00b0b030`.
Read-only database verification found `0003_mediconnect` and all three new
columns. Public probes returned 401 for all seven unauthenticated MediConnect
routes and authenticated IAM path, with Global API Via headers; unknown product
returned 404 and login returned 200. The source repository remains public.

## Registrar review — local validation

The agreed workflow is recorded in the
[review design](../superpowers/specs/2026-10-25-mediconnect-registrar-review.md).
Submission/worklist and approval/projection/withdrawal tests use real PostgreSQL
and NATS. Local tests cover independent registrar authority, reader/admin without
a grant, cross-tenant denial, revoked grants, malformed input, missing patient,
rejection, duplicate payload conflicts, cancellation-before-request and late
approval after local withdrawal. Original request v1 semantics remain unchanged.

The first real consumer test exposed a mistaken design assumption: `WithSystem`
uses the app pool, not BYPASSRLS. The implementation uses narrowly scoped delivery
RLS alongside registrar/account policies. The first worklist and decision tests
failed for absent routes before passing after implementation. Contract generation
also caught missing shared-payload aliases and architecture gates caught the new
singleton routes and event fields; all require explicit contract updates.

Validation completed locally: affected module and multi-owner race tests passed
(MediConnect 87.2%, patientmaster 76.7% statement coverage), Global API/architecture/
contract checks passed, full `go build ./...`, `go vet ./...` and golangci-lint
passed. The Global API test uses real owner HTTP forwarding with forged identity
headers, PostgreSQL and NATS. Outbox failure rolls back the decision and read-audit
failure prevents disclosure. Full backend coverage regression is recorded on the
implementing PR rather than inferred from these targeted runs.

A fresh production schema-only snapshot and 97 migration identifiers were restored
into a network-isolated local PostgreSQL container; no patient/account data was
copied. Both review migrations applied, repeated safely and preserved a synthetic
pre-existing account. Previous migrator compatibility passed. Protected evidence:
`~/.local/state/helivanta/mediconnect-registrar-review-20261025`.
The pre-review **writer** rollback restriction remains: hold affected writes and
reconcile withdrawals before reverting a binary that cannot emit cancellations.

Registrar changes are not yet merged or deployed. #37/#552 remain open because
portal scope, consent/proxy authority and clinical disclosure remain incomplete.
