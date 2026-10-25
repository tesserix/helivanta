# MediConnect delivery and issue evidence

## Verified guardian clinical-consent release — 2026-10-26 Melbourne

[Application #1341](https://github.com/tesserix/helivanta/pull/1341) is merged and
deployed as `81655619078522ea4e60d80679993a5b0c7d9c07`, including the upstream
failure diagnostics from [#1340](https://github.com/tesserix/helivanta/pull/1340).
All PR and main CI passed. Kargo promotion
`prod.01m5t5qv0jmefsrp6cheh92gsf.64b999f` used Warehouse `services` freight
`64b999f364a8c60fadfd6807992ae99666795796`. Infrastructure
[#1589](https://github.com/tesserix/tesserix-k8s/pull/1589) supplied an explicit
default-off writer switch; [#1593](https://github.com/tesserix/tesserix-k8s/pull/1593)
activated it only after the compatible-reader baseline was verified. Both
infrastructure PRs passed their checks, including the repository tests.

Runtime checks verified the exact release tags and linux/amd64 manifest digests,
two updated/ready replicas and current generations for API, Global API and shell.
All three Argo applications were Healthy/Synced. Each API pod had the activated
writer setting. The API-chart background workers also had two ready replicas.
Production metadata confirmed migrations 0009/0010/0011, valid/ready concurrent
indexes, validated proxy/one-authority constraints and forced RLS on all five
affected identity/consent tables. The admin migration connection targets the direct
CNPG primary service. The old signed self-v1 receipt remained readable; its exact
capture replay retained the withdrawn state. The disabled baseline probe returned
503 and created no consent before activation.

The signed proxy journeys used three existing synthetic QA identities and the
isolated test tenant through `https://helivanta.app/api/v1`. This reuses the staff
session boundary and does not prove patient-only onboarding. Approved wording and
recipient bindings were explicitly labelled synthetic. The reviewer briefly held
recorded admin/registrar privileges to independently approve these fixtures;
those privileges were removed before the patient/proxy journey. An independent
existing registrar reviewed the identity claim and proposed guardian authority.
No real policy or clinical content was approved or published.

Both **parental** and **appointed_guardian** journeys passed:

- Separate synthetic registration, explicit account initialization/reuse, child or
  ward claim, asynchronous identity review, independent approval, reviewed
  authority proposal and proxy acceptance.
- Capacity-specific approved notice and recipient catalogs; OpenBao-signed capture,
  identical replay and canonical retained receipt. Another actor's private receipt
  was hidden; incorrect tenant preconditions returned 409. Registry hospital/actor
  denials and raw patient-read denials also passed.
- The exact recipient's diagnostic-report check returned `allow` and
  `record_access_authorized:false`. An ungranted resource returned `deny`; a
  different recipient received 404. No clinical record was disclosed.
- Withdrawal, identical withdrawal replay and capture replay retained withdrawn
  version 2. The next recipient check denied. Authority revocation retained
  version 3, reacceptance returned 409 and proposal replay did not reactivate it.
- Claim withdrawal reached the review owner. Synthetic notices were retired and
  recipient bindings revoked. Registrations, account and immutable history remain.

Metadata-only verification established exactly two clinical lifecycle outbox
revisions and three authority revisions per case, one consent insert and update,
three exact-recipient decisions (one allow and two denials), and zero integrity
incidents. Two controlled retained-receipt reads appended exactly two attributable
read audits in each case. Final checks found zero temporary admin memberships and
hospital grants, preserved the original supply-compliance membership and confirmed
that review permission was absent. Both retained privacy initializer Job UIDs were
unchanged; no initializer deletion or bypass was performed.

Fresh Zitadel password/PKCE and Global API session exchanges passed during the
reader rollout and afterwards. Anonymous transport runs observed 174, 260 and 174
expected 401 responses with no other status in those samples. These are bounded
observations, not an availability SLO measurement. The original rollout-time 503
cause in [#1338](https://github.com/tesserix/helivanta/issues/1338) remains unverified;
the issue stays open with the new diagnostic code deployed.

Two QA-client corrections are retained in the evidence: ungranted hospital access
correctly returned 404 instead of an empty record, and a metadata probe using
default HTTP headers was rejected before Global API. The helper restored its
temporary role before resuming the first case; the normal QA headers returned 200
and confirmed removal of review permission. Persisted lifecycle and audit checks
passed after these corrections. Protected evidence is retained outside the public
repository. The API is configured against the global Valkey cache; the cache
StatefulSet uses Valkey 9.1.1 and had three ready replicas.

Local full backend race/coverage, 100k catalog/ledger plans and all 22 workspace
lint/type-check/test/build tasks passed before release. The exact merged tree also
passed critical Global API, MediConnect, patientmaster and tenantdb race suites.
Dependency-failure, tamper, concurrency and capacity-skew tests remain local
infrastructure evidence; production dependencies were not disrupted to repeat them.

**Recovery:** after schema-v2 proxy receipts exist, disable capture while retaining
a compatible reader and additive history. Do not roll back to a self-v1-only
binary or erase evidence to permit rollback.

#552/#60/#556 remain open. Adult delegation/family profiles, owner-authorized
clinical record release, scheduling/reminders, approved real notice publication,
deadline-worker activation and remaining parent-story criteria are unfinished.
No broader product, clinical or regulatory readiness is claimed.

## Earlier guardian registry release — 2026-10-26 Melbourne

[PR #1337](https://github.com/tesserix/helivanta/pull/1337) is merged and deployed as
`c30fdb612cfa0a02d15fe0265685c0aa880b36f8` through Kargo promotion
`prod.01m5swkmsndwf003n2r3ag7848.631bf3c`, freight
`631bf3ca4d5da779776a42838ccc2bd78da66ed4` from Warehouse `services`. Both PR
and main CI passed. API, Global API and shell each have two updated/ready
replicas with observed generations matching desired generations; their running
image digests match the release's OCI linux/amd64 registry manifests. All three
Argo applications are Healthy/Synced. Both repositories remain public.
Migration `0009_patientmaster` is present; in-development `0010_patientmaster`
is absent.

This adds the separate reviewed parental/appointed-guardian authority registry,
not clinical consent or record access. Existing self semantics and recipient
policy predicates remain intact. All registry receipts explicitly retain
`record_access_authorized:false`. The complete local backend race/coverage gate
passed: patientmaster 79.3%, MediConnect 85.9%, pharmacy 88.1%, workforce 83.8%,
IAM 80.6%. Build, vet, lint (zero issues), generators/check and secret scan passed.
An existing clinical-consent allow assertion caught a draft policy omission;
restoring its recipient predicate fixed that regression before merge.

Live verification used two existing synthetic QA identities through
`https://helivanta.app/api/v1`, fresh Zitadel PKCE/Global API sessions and the
explicit expected-tenant precondition. The existing registrar reviewed a different
actor's child and ward claims. No temporary IAM or hospital grants were added.
The proxy's explicit MediConnect account was initialized before its first claim;
the initial claim attempt correctly returned 404 while that account was absent.

For both guardian kinds, the real journey passed:

- Registration, claim submission, asynchronous review projection, independent
  registrar approval, separate evidence-attested authority proposal and proxy
  acceptance returned the expected states.
- The proxy could not review its own authority (403). A different actor's private
  receipt and a wrong hospital were hidden (404); wrong expected tenant returned 409. Evidence references and private subjects were absent from receipts.
- Exact proposal replay retained accepted v2; owner history included the receipt.
  Direct patient access was denied (403 for the live proxy without patient-read
  permission; local tests separately cover permitted-reader/no-hospital 404).
- Revocation retained v3. Subsequent acceptance returned 409, and proposal replay
  could not reactivate it. Claim withdrawal reached both owners while the revoked
  receipt remained readable. No clinical records were disclosed.
- Metadata-only database verification confirmed one registry insert, two updates,
  attributable reads and exactly three ID/version outbox events per authority.
  Synthetic registrations, the QA account and immutable lineage remain retained;
  both authority/claim links are inactive.

The sixteen tenant-precondition compatibility/negative checks also passed on
this release, with zero domain-audit delta for rejected commands. Protected local
runtime/QA evidence is retained outside the public repository.

**Open release availability finding:** one synthetic password-login request
returned HTTP 503 while promotion was still running; the helper could not decode
its body as the normal JSON error envelope. A fresh attempt after convergence
succeeded. The raw failed response and terminated-pod logs were not captured, and
current-pod/targeted Cloud Logging inspection did not establish a cause.
[#1338](https://github.com/tesserix/helivanta/issues/1338), assigned to Sam123ben,
tracks diagnosis and verified correction. Healthy pods and the later retry do not
prove release-window authentication availability.

#552/#60/#556 remain open. At this earlier checkpoint signed proxy declarations
were under local development. Adult delegation, family profiles, authorized record
release, booking/reminders,
approved real notice content and the remaining parent-story criteria are unfinished.
No clinical or legal readiness is claimed.

## Earlier self-consent release — 2026-10-26 Melbourne

Application [#1333](https://github.com/tesserix/helivanta/pull/1333) deployed
`dff023ce383f3f478214e13a34fdec8e60b68d18`. Infrastructure prerequisites
[#1532](https://github.com/tesserix/tesserix-k8s/pull/1532) and activation
[#1535](https://github.com/tesserix/tesserix-k8s/pull/1535) are merged. After
activation, API, Global API and shell were Healthy/Synced at infrastructure
`0f8d11302b6abe8ba1d77b3711c39c3e7d5cc2eb`, each with two updated/ready
replicas; all three rollout-status commands succeeded. Both repositories remain
public. API automatic service-account token mounting is disabled; the dedicated
claim and Kubernetes API projections and Istio proxy are present.

Live verification used three existing synthetic QA identities in the isolated
test tenant through `https://helivanta.app/api/v1`, with fresh Zitadel
authorization-code/PKCE login and Global API session exchange. The expected
tenant was explicitly selected and checked: the test owner has multiple tenant
memberships, and the normal default selected a different membership. This API
test does not prove browser profile selection or a patient-only login journey.

- Account creation returned 201, identical replay and owner read returned 200,
  and all returned the same account ID. Consent listing returned 200; anonymous
  account access returned 401.
- A synthetic registration, explicit link-review submission, independent
  registrar decision, consent-authority proposal and patient acceptance passed.
- Clearly labelled synthetic sharing wording was drafted and independently
  approved only for this QA run. Recipient self-approval returned 409; a third
  existing QA identity was then used as recipient, with separate creator and
  reviewer. No real patient notice or clinical/legal policy was approved.
- Signed clinical capture returned 201 and a `vault:v1:` HMAC. Identical replay
  and owner GET returned the same receipt. Another subject's GET returned 404.
  This exercised the deployed workload-authenticated current-claim client and
  OpenBao signing path, rather than a fake provider or local harness.
- The exact recipient's diagnostic-report consent check returned `allow` with
  `record_access_authorized:false`; an ungranted resource type returned `deny`
  and a different subject returned 404. No clinical record was disclosed.
- Withdrawal, identical withdrawal replay and capture replay all retained the
  terminal withdrawn state. The next exact-recipient check returned `deny`.
  Exact-row database metadata confirmed version 2/withdrawn, one eligible allow,
  one scope-mismatch deny and one inactive deny, plus one insert, one update and
  seven read-audit entries. No receipt body or clinical payload was queried.
- Cleanup revoked the test authority, both recipient bindings and original
  claim, and retired the synthetic notice through owner APIs. Temporary QA
  reviewer hospital access and tenant-admin membership were removed; fresh
  permissions confirmed review access was gone. Original supply-compliance
  membership and immutable registration, receipt and audit history remain.

The live checks used bounded Python standard-library HTTP clients reading local
QA files; no browser cookies were extracted. Protected run state is retained at
`~/.helivanta/clinical-sharing-live-qa-20261026.json`. Credentials and session
values are excluded from this repository and report. Local selected infrastructure
checks passed: 19 consent/Transit/Global API tests, Ruff, and `git diff --check`;
all activation PR CI checks passed before merge. Fault injection, 100k-row plan
checks and the backend race regression remain separate local evidence from #1333;
no production dependency was disrupted to repeat those tests.

This supersedes historical statements below that clinical sharing is not
activated or has no authenticated production test. It proves the self-consent
gate only. Proxy/guardian access, source-authorized released/amended records and
file disclosure, booking/reminder integration, real approved notice content and
deadline-worker activation remain incomplete. The broader stories remain open.

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

### Pre-promotion withdrawal correction

PR #1321 merged as `edb23be4d1cd32741af27d3254ee84ca27e9f377` after all PR
checks passed. Its full local backend race/coverage gate also passed, including
IAM; patientmaster coverage was 77.6%. Production promotion was held when rollout
review found that older writers can withdraw an original request without updating
the new review projection. The regression reproduced stale `submitted` state.

The follow-up adds original-request locking and transactional cancellation repair
on review reads, withdrawal retries and result delivery. A second regression
caught missing repair on withdrawal retry before that path was fixed. Migration
0005_mediconnect grants the delivery context source reads while retaining the
existing subject-owned write policy. Rehearsal against the isolated production
schema plus both review tables passed, including repeat application and previous
migrator compatibility. The initial registrar Freight has not been promoted;
only the corrected source is eligible for the planned manual rollout.

## Remaining acceptance and account-consent implementation — 2026-10-25

Registrar review and rolling-upgrade reconciliation are deployed at cccd5ff;
verified release evidence is in tesserix/kargo-manifests#62. Remaining #37/#552
work includes approved notices/eligibility, profile/proxy authority, expiry and
majority transitions, rights-request intake/officer routing, released-record
access, and UI/mobile acceptance (excluded from the current API-only scope).
Other MediConnect product journeys retain their separate stories and owners.

The next API slice implements tenant-reviewed immutable notices and account
consent declarations/withdrawal under ADR-0037. No legal text or eligibility rule
is seeded, and the API does not activate processing or confer clinical authority.
Initial tests failed for missing routes; exact-language testing also caught an
ORM projection issue. A response-audit outage test reproduced receipt/event commit
before failed read audit; the implementation now uses one transaction throughout.
Targeted tests cover Global API forwarding, tenant/subject isolation, immutable
content, retirement, explicit acceptance, terminal withdrawal, stale hashes,
duplicate/concurrent commands, and rollback on audit failure. This slice is not
yet deployed and does not close either parent story.

Account-consent validation: affected module, bootstrap, Global API, architecture
and contract race tests passed; MediConnect statement coverage is 83.2%. Build,
vet and golangci-lint passed. Schema-only production restore contained 100 prior
migration IDs; migration 0006, repeat execution and previous migrator compatibility
passed with a synthetic existing account. A 100k synthetic receipt query-plan
exercise uses the account-scoped page index with forced app-role RLS; this is
query-plan evidence, not an end-to-end load/SLO certification. Evidence is retained
under `~/.local/state/helivanta/mediconnect-consent-20261025`.
The initial query-plan attempt correctly failed because the schema-only restore
omits ACLs. Read-only production inspection confirmed owner `helivanta` grants
DML to `hms_app` by default, including the deployed account/review tables. The
isolated benchmark was rerun with equivalent app-role grants and forced RLS:
100k synthetic receipts used `mediconnect_consent_page` (4 buffer hits, 0.139ms
execution for that sample). The HTTP acceptance/withdrawal integration test also
passes using the production owner's actual role/default-grant setup.

## Deadline release verification — 2026-10-25

This checkpoint supersedes the earlier not-deployed account-consent and registrar
checkpoints above. It does not complete clinical/proxy authority or the parent
patient-facing stories.

Application [PR #1325](https://github.com/tesserix/helivanta/pull/1325) merged as
`19b8a5f436288a56709f386e87c24c20c6b518f0`. Full local backend race/coverage,
build/lint, affected vet and contract generation/check passed. PR and post-merge
CI passed, including web and browser E2E. Actual local PostgreSQL, JetStream and
Temporal acceptance passed worker restart, terminal transition and duplicate replay.
Isolated production-schema rehearsal passed upgrade, repeat application and old
migrator compatibility while preserving synthetic account data.

Kargo promotion `prod.01m5rjpxnx018d5k825byyzsbe.730a8b2` succeeded for Freight
`730a8b2e94f228354d44a00a3df70e3cb52f3abd`. API and Global API are two ready
replicas each at
`sha256:b9c874a891290b86cedfd0ab0c5035591faa0a5a3971e3a6432d28329cbee3f3`;
shell is two ready replicas at
`sha256:80f98773e9cce774bcadb54d17cbd576e59dc7e63f9f917d3c19740fa8582675`.
Read-only checks verified `0002_privacy`, forced RLS on all three new tables and
the non-superuser, non-bypass, non-inheriting worker role. The 36 public HTTP
probes verified unauthenticated rejection, forged-header rejection, unknown-product
404 and login 200. These probes do not prove an authenticated production clinical
journey. Both source and infrastructure repositories remain public.

Infrastructure PRs tesserix/tesserix-k8s#1528 and #1530 are merged. The original
initializer failed admission because zero CPU annotations conflict with the
namespace minimum. The correction inherits admitted defaults and versions the
immutable Job template; an actual namespace Pod dry-run passed. Replacement Job
`helivanta-privacy-secret-seed-27b84c29` completed, and the worker ExternalSecret is
Ready=True/SecretSynced. The original Job
`helivanta-privacy-secret-seed-afc1dca9` never created a Pod; it is extraneous and
keeps the postgres Argo Application Degraded. Its manifest is captured outside
the repository. The specifically requested deletion approval remains pending.

Worker activation PR tesserix/tesserix-k8s#1529 has passed CI and 78 scoped local
infrastructure tests, but remains unmerged until that health gate is resolved.
No worker readiness, live Temporal processing or tenant policy activation is
claimed. No policy, legal duration, officer or notice is seeded. Preserve
compatible workers/consumers and all clock evidence after any policy is accepted;
disabling orchestration alone is not a safe rollback.

The earlier pharmacy 400-versus-404 failure did not recur in 30 focused race
repeats. A separate test-only change replaces the scope test's near-current host
timestamp with a fixed historical cutoff, removing its dependency on host and
PostgreSQL clock agreement. Invalid/future-period assertions remain. Root cause
of the original failure is not established; no production pharmacy behavior changed.

Protected release evidence is retained under
`~/.local/state/helivanta/privacy-deadlines-20261025/`. Clinical/proxy consent,
owner-authorized disclosure, booking/reminder integrations, provider-confirmed
notifications and approved notice content remain open. #66 and #556 must not be
closed on the strength of this release. SMS remains disabled; OTP is outside the
current API development scope.

## Reviewed self consent authority — implementation checkpoint

The separate self-authority lifecycle is implemented under patientmaster in
`feat/mediconnect-patient-authority`, following the
[authority specification](../superpowers/specs/2026-10-25-mediconnect-consent-authority.md).
It includes an independently reviewed capacity proposal, patient-owned receipt/list,
explicit patient acceptance, direct patient/reviewer revocation and computed expiry.
It does not yet implement guardian/adult delegation, clinical purpose consent or
record release. No parent issue is complete and no production deployment is claimed.

Real HTTP/PostgreSQL tests first failed for the absent proposal and lifecycle
routes. The RLS linter then rejected multiple permissive policies; the final design
retains one policy per table, with exact subject read scope and unchanged registrar/
delivery checks on source-review writes. Permission, pagination and event-classifier
gates failed before their independently documented entries were added.

Targeted race tests cover subject/tenant/hospital isolation, current reviewer grants,
missing/false capacity confirmation, stale registration, source decision identity,
expiry without a timer, immutable SQL history, audit rollback, bounded cursor pages,
actual Global API forwarding with forged headers, and concurrent idempotent replay.
The initial affected module/Global API/architecture/contract race gate passed. Build,
vet and repository Go lint passed. An isolated restore of the captured 102-migration
schema passed current migration, repeat execution and previous-migrator compatibility,
preserving a synthetic existing account and forced RLS. The 100k-row synthetic
read-plan benchmark used the subject-page index under the non-bypass app role
(3.503 ms in that sample); this is not an end-to-end load/SLO claim. Bulk benchmark
seeding bypassed USER triggers only in the isolated database; HTTP tests separately
exercise normal write and audit guards. Evidence is retained under
`~/.local/state/helivanta/consent-authority-20261025/`.

The first complete backend regression gate failed: one concurrent authority
acceptance returned 500, and an unrelated pharmacy overview count disagreed with
its list. Fifty isolated authority concurrency race repeats and three complete patientmaster
package race repeats passed afterwards. A separate repeated-package diagnostic
run also reproduced a gateway timeout; the original authority failure remains
unresolved, and query/error metadata is retained for the next regression run. The pharmacy failure reproduced
with a just-committed movement omitted while the database clock was ahead of the
host; its separate fix in #1329 reads the stock cutoff from PostgreSQL. Release-documentation
CI also exposed emergency retry contention, now reproduced against the separate
case-number unique constraint and corrected in #1328. These failures must be
resolved and the complete gate rerun before this authority change is released.

### Gateway-budget and ambiguous-response check

The forwarding fixture initially overrode the gateway timeout to five seconds,
while the actual `globalapi.FromEnv` configuration fixes it at thirty seconds.
The fixture now uses that configuration directly. This corrects a test setup
mismatch; production timeouts were not changed and the original 500 is not
retrospectively attributed to it without stronger evidence. A controlled real
HTTP test loses the first acceptance response after its successful commit,
verifies the accepted receipt through the owner, then replays the identical
command and checks that only one acceptance event exists. The targeted race
checks passed. The draft remains gated on the final combined local/CI regression.

### Combined regression and cancellation handling

The combined backend `GOFLAGS=-p=2 ./scripts/coverage-gate.sh` passed at `317bff7`,
including the emergency retry and pharmacy database-clock fixes (patientmaster
78.0%, pharmacy 88.1%, emergency 75.2% statement coverage). This supersedes the
first failed aggregate run as current combined-code evidence; the earlier failure
and its unproven authority cause remain recorded above. The follow-up cancellation
test observed 500 before implementing a retryable 503
`consent_authority_unavailable` response. The affected patientmaster race package
then passed with production-budget forwarding and committed-but-lost-response
reconciliation. Final CI for that follow-up is still required. The full
MediConnect goal and clinical/proxy/disclosure stories remain open.

### 2026-10-25 authority merge and clinical notice work

Self-authority PR #1330 merged as `2c944ae63f7e407ab2866774668d7f57ede81184`
after all current-head CI checks passed (Go, web, E2E, scripts, builds and security).
Read-only production discovery still showed API `19b8a5f...`, with API/global/shell
2/2 ready; merge is not deployment. Failed privacy initializer Job remains present;
the named cleanup approval remains pending and worker activation #1529 remains open.

Next slice implements patientmaster-owned immutable hospital sharing-notice drafts,
independent approval and terminal retirement. It does not publish patient notices or
authorize clinical access. Real synthetic PostgreSQL/NATS tests observed missing
routes and missing exact receipt wording before fixes; lifecycle, wrong scope,
revoked grant, invalid bodies, immutable SQL history, concurrent Global API retries
and outbox rollback checks are included. Final aggregate regression/CI and rollout
remain outstanding until recorded below. #60/#552/#556 must remain open.

No real wording, policy applicability, proxy authority, record release, scheduling,
reminder delivery or clinical consent is activated by this change.

Notice follow-up: successful GET now verifies every content field, `no-store` and
two actual read-audit entries across replay/read. An invalid raw UTF-8 byte was
silently replaced by the standard JSON decoder and reached command conflict (409)
instead of rejection (400). The shared, already-bounded patient input decoder now
checks raw UTF-8 before JSON decoding; all patientmaster consumers were traced
against current source. The regression then passed in the full patientmaster race
package (50.213s sample). This is a strict input correction, not a policy change.

### 2026-10-25 authority production and notice validation

Authority release `2c944ae63f7e407ab2866774668d7f57ede81184` deployed through Kargo
promotion `prod.01m5rvax7wjz4rhnc20yshvj7b.b13783a`. The active Kubernetes identity
explicitly allowed Promotions; this reused the previous Kargo submission path
after the CLI session rejected authentication. Kargo/Argo performed the rollout.
API/global/shell rollout status succeeded and each had 2 ready/available replicas.
The migration Job reported success; production metadata verified patientmaster
0005/0006 and forced RLS on review/authority tables. Public Global API rejected
missing credentials with 401. This is not an authenticated production patient
journey. No notice, legal policy or clinical consent was seeded.

Notice PR #1331 merged as `424e8ca18421a28c2dff572162078d91fcc7959f` after all
current-head CI passed and final local full backend race/coverage passed using
`GOFLAGS='-p=2 -timeout=20m' ./scripts/coverage-gate.sh` with the Colima test socket.
Earlier runs retain unresolved intermittent emergency 500 and IAM fixture timeout
failures; both passed three focused repeats. A bounded run passed those packages
but exceeded the default ten-minute workforce binary budget. The twenty-minute
binary budget changed neither request deadlines nor assertions. No original
failure cause is claimed proven. Notices subsequently deployed via Kargo promotion
`prod.01m5rye9mpdjzywcbjbdfaehgt.9c8aa3f`: API/global/shell each 2/2 updated/ready,
migration 0007_patientmaster, forced RLS/one policy and both audit/history triggers
verified. Public missing-credentials probe returned 401; no authenticated production
patient journey or actual notice content was activated by this work.

The next owner boundary adds a live workload-authenticated exact claim predicate
for clinical consumers. Real Postgres tests verify committed withdrawal without
broker wait, wrong tuple/credentials, strict input, read-only RLS and audit-failure
denial. It does not implement grants, recipient authority, proxy consent or record
release; those broad stories remain open. Startup is disabled until workload
configuration and a real consumer are integrated; do not infer enablement from code.
Deadline worker activation #1529 still awaits failed initializer cleanup approval.

Current-claim evidence: the full MediConnect race package passed (24.224s sample),
as did full architecture race checks, workload-verifier tests and the actual
hospital provisioning/adoption compatibility cases. Missing API constructor,
verification-outage 401 instead of 503 and missing exact service-account audit
attribution were observed before their fixes. Input/scope, immediate withdrawal,
read-only RLS and audit rollback checks pass. A 100k synthetic claim plan used the
primary-key index under a non-bypass app role with forced RLS and all history/audit
triggers retained (0.483 ms sample); the reused local benchmark role needed its
normal audit INSERT grant restored before seeding. No live privilege was changed.
This plan is not a TokenReview-inclusive load test or production capacity proof.
