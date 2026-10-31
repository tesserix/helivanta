# Current-person review before employment approval

Status: backend and API-client implementation candidate; not deployed, accepted
through the UI, an approved real person check or an activated employment policy.
The source now implements immutable submission, detail/history, independent
decision, withdrawal/revocation and purpose-bound evidence-byte release. The byte
path pins original scope, actor/link/epoch, review revision, source and scan/file
lineage across storage I/O; the API client verifies type, size and SHA-256 before
publishing a Blob. Hospital discovery, a summary inbox and manager-only target browsing/Staff ID search
are also implemented. Manager preparation/evidence selection and immutable history
UI are source/component candidates. Normal browser and remaining failure/concurrency acceptance,
real-scanner/browser acceptance and employment consumption remain open.
Continues A01–A12 and ADR-0048/0049 after scoped HR intake. Existing identity-check
drafts and active login links retain their current guards; neither is weakened
to make a new hire appear eligible.

## Source findings and ownership

`identity_cases.go:putIdentityCaseDraft` and the identity-case database trigger
both require an active target Staff source. `login_links.go` also requires active
Staff before operational account binding. In contrast, scoped enrollment correctly
creates inactive joiners. Employment drafts/evidence are preparation only. An
approval path requiring an active target link would therefore be circular.

Workforce owns the canonical hospital Staff record, source identity fingerprint,
employment generations, protected documents and person-review facts. A person
review is an attributable decision about that existing record, not a new person
master, login binding or verified government identifier. IAM continues to own
membership and independently reviewed scoped assignments; Zitadel owns global
credentials. Every client request enters Global API and is authorized by Workforce.

Assets are private identity evidence, identity/account associations and subsequent
employment authority. Threats include a foreign tenant/hospital, an insider using
another account or duplicate Staff record, stale reviewer/source authority and
replaced evidence during disclosure. Human identity findings remain attributable
human decisions under the hospital's recorded policy. The software must never
claim automated real-world identity verification from a name match or a checkbox.

Reuse the existing unmeasured planning envelope: 500,000/1.5 million Staff at
12/36 months, 50 peak metadata requests/second, 80:20 reads/writes, 99.9%
availability and metadata p99 below 500 ms. At two reviews per Staff and three
1 KiB transitions per review, history is approximately 3/9 GB before indexes,
evidence and replicas. Commands are at most 8 KiB; lists default to 50/max 200
keyset rows; a review binds at most ten existing protected files. No new service,
queue, cache or biometric provider is justified by this volume.

## Actors and explicit independence

- The existing hospital-scoped identity-check manager prepares and submits a
  current-person review. The identity-check reviewer independently decides it.
  Both require current IAM authority and their own active, independently reviewed
  actor Staff/login link. HR intake alone grants neither decision permission.
- The target may be an inactive joiner, an existing worker or a worker requesting
  no digital access. No global account is created solely for this review. Target
  inactivity is an observed preparation state, not a reason to disable the actor
  checks or activate employment.
- A submission records a mandatory policy reference, observation date, validity
  instant, protected identity evidence and an explicit human identity finding.
  The preparer must attest that the evidence concerns the target and that they
  have disclosed known account associations and potential identity conflicts.
  An unresolved identity/conflict cannot be submitted as a successful finding.
- A reviewer must explicitly attest an independent identity examination under
  that same policy, including that they are not the target. An absent target
  login is never computed as proof of independence. Digital-access intent and
  non-digital intent are recorded separately from any asserted account ID; an
  asserted account ID is not a reviewed operational login binding.
- Reject a reviewer who is the submitter, any frozen review/evidence contributor,
  the target's current linked subject, any historically linked target subject,
  or whose reviewed actor Staff ID is the target Staff ID. Resolve these checks
  under owner locks and retain their provenance. Missing required provenance
  yields an unresolved source, not an empty exclusion set and automatic success.
- Human attestations do not prove the absence of an undisclosed second identity
  or prevent collusion. Preserve the evidence, contributor history, policy and
  reviewer accountability so a finding can be challenged and revoked. Do not
  describe this workflow as biometric matching or external-provider verification.

This uses the accepted separation-of-duties policy and existing identity templates.
It does not settle ORG-D2/D5/D6, mandate a national identifier or police check, or
activate a jurisdiction-specific hiring/background policy.

## Immutable source and evidence

A submitted review freezes tenant/hospital/Staff, source identity hash, source
Staff version and employment epoch; protected evidence IDs, metadata/release/scan
lineage and relevant classification revisions; policy/observation/validity; and
contributor/account-association provenance. The source identity hash is the
existing owner's hash, not a parallel client calculation.

Evidence must be existing scan-released `identity_evidence` for the same Staff
enrollment, with the exact recorded version and byte generations. Fail closed
for missing/unavailable/revoked evidence, changed binding, incomplete scan lineage
or a changed identity source. No file names, identity findings or storage paths
belong in outbox payloads. Later source/evidence changes never rewrite the frozen
review. Unrelated contact edits must not silently change identity findings; their
version/concurrency effects must be distinguished from the identity hash.

The independent reviewer receives only this review's source/evidence projection.
Identity review does not confer the ordinary document manager's directory-wide
file authority or expose employment, payroll, benefit or tax files. Each metadata
and byte-release phase must hold the original assignment, actor link and target
review/source basis; replacement assignments or links require a fresh request.
Repeat authorization after storage I/O before releasing private bytes. Audited
sensitive reads and immutable decision evidence are part of the transaction.

## Hospital discovery boundary

The additive `GET /v1/workforce/person-review-scopes` is a read-only,
Global-API-routed discovery surface for the existing identity manager/reviewer
roles. The canonical `PersonReviewHospitalScope` contains hospital ID/name and independent
`can_submit`/`can_review` booleans. IAM owns both assignments; Workforce owns the
actor link and hospital name. Neither capability authorizes later commands.
Both original workspace assertions are mandatory, with 409 on mismatch/absence.

Reuse the existing own-link discovery RLS and indexed descending link keyset;
no new grant, table, permissive SQL policy or migration is needed. Scan at most
20 candidates plus one probe, with four concurrent authorization checks and a
five-second total authorization budget capped by the session idle deadline.
This is a stricter discovery bound than general review collections. At the
existing unmeasured 50 metadata requests/second envelope, worst-case discovery
can make 2,000 assignment checks/second; actual capacity/latency requires load
measurement before claiming the planning p99. No extra service/cache cost is
introduced. A single unbounded all-hospitals query is rejected because filtering
must not disclose unassigned hospitals or create unbounded authorization work.

Each visible row requires held exact current assignment grants, the supervision
lock and the unchanged active reviewed actor link, with an attributable audit.
Capabilities are determined independently; tenant/hospital administration alone
is insufficient. An authoritative denial filters the row/capability, whereas
an unavailable IAM/OpenFGA/owner check yields an error (503 for authorization),
never a fabricated empty success. The cursor binds tenant, actor and route; an
empty filtered page can still have a continuation. SQL scan/audit failure returns
the owner's generic failure without a successful response. Retry repeats the
read and audit, not a grant. Removing this additive route is reversible in source;
there is no data migration or live mutation in its implementation.

## Hospital inbox and target selection candidate

Source implements two owner reads for the professional manager/reviewer workspace:
`POST /v1/workforce/hospitals/:hospital/person-review-targets/search` and
`GET /v1/workforce/hospitals/:hospital/person-review-inbox`.
Target search requires the existing identity manager assignment and reviewed
active actor link. An explicit empty `employee_code` string browses the paginated Staff list;
a nonempty `employee_code` performs a case-insensitive exact Staff ID lookup using
the existing unique lower-case index. Names accompany each result for human
selection. Do not ask the user to enter technical UUIDs or claim name matching
as identity verification. Search terms stay in the bounded JSON body, not URLs,
audit payloads or events. Target rows contain only ID, hospital, display name,
Staff ID, employment status and created time. Inactive joiners remain selectable.

The inbox admits current identity managers or reviewers with their reviewed
active actor link. Default status is pending; explicit statuses are the existing
persisted review states. Summaries include review/Staff IDs, current Staff name/
code, recorded status/version/validity and creation time. They omit findings,
policy, account associations and evidence. A recorded status is not present
eligibility: opening a row must use the existing authorized detail/current-state
contract and a fresh version before any decision. No employment action follows
from selecting a target or reading an inbox row.

Both reads require original tenant/subject assertions. Use the existing keyset
envelope, default 50/max 200; the browser resets pagination whenever hospital or
filter changes. The cursor is a position, never authority; every query independently
scopes verified tenant/hospital, current actor and permission. The inherited
unmeasured 50 metadata requests/sec planning envelope remains; target browse uses
the existing tenant/hospital/time keyset and exact code lookup its existing
casefold index. A new concurrent tenant/hospital/status/time/ID index supports
the inbox. Measure plans and latency before asserting the planning SLO.

Migration 0056 adds an explicit read-purpose context for only Staff summaries and
review heads; it does not expose profile, document, history or contributor tables.
Preserve one permissive policy, original WITH CHECK expressions and every old
DELETE restriction; add a restrictive deletion guard before extending Staff
USING. Migration 0057 registers the populated-table concurrent index. These are
forward source migrations, not permission to apply them to a live environment.
Reversing the feature requires a forward policy/index migration after consumer
removal; old migration SQL remains immutable. No new service/cache/event is needed.

Owner reads hold the selected current IAM grant and supervision/actor locks,
set transaction-local read context only after authorization, select bounded
projections and commit attributable per-row audits in the same transaction.
Dependency/lock failure returns the existing 503 authorization/busy error; a
missing permission returns 403 without revealing hospital existence, and an
unknown exact code is an empty authorized list. SQL/audit failure produces no
successful response. Reads may be retried with current authority; they grant or
activate nothing. Required proofs include no private fields, manager-only target
browsing, reviewer inbox, stale actor cutoff, tenant/hospital isolation, immutable
writes/deletes under read context, pagination, audit failure and measured plans.

## Manager evidence selection candidate

Manager preparation needs a bounded identity-purpose metadata selector, rather
than an arbitrary file-ID input or a general document-management assignment.
Source now registers `POST /v1/workforce/hospitals/:hospital/identity-staff/:id/person-review-evidence/search`
with a strict 1 KiB body pinning current Staff version, employment epoch and owner
identity hash. All three are required; version/epoch pointers distinguish omitted/
null from valid integers. Original tenant/subject assertions remain mandatory.
The existing identity manager assignment, reviewed active actor link and held
supervision/source locks authorize this read. Reviewers receive evidence only
through the submitted review's frozen detail/content authority.

The response is a canonical paginated projection of scan-released PNG/JPEG
`enrollment` / `identity_evidence` bound to exactly this Staff and source version:
file/hospital/Staff IDs, filename, MIME, size, record version and creation time.
It contains no storage keys, object generations, uploader/account associations or
private findings. A changed source returns 409; missing action/actor 403, an
invisible target 404 and unavailable authorization 503. Source/file metadata and
per-disclosed-row audits share the held transaction; there is no storage I/O or
binary release. Existing five-second owner budgets and default 50/max 200 keyset
limits apply. Reuse the indexed tenant/hospital/Staff/time document path; measured
large-fixture plans and load acceptance remain required before claiming the
unmeasured planning SLO. No service/cache/event or new index is justified yet.

Forward migration 0058 adds a metadata-selection read context only to the single
permissive document policy, preserving original WITH CHECK and restrictive delete
rules. Its context requires current identity source/review management context,
explicit selection purpose and the pinned Staff version; unrelated files and
other source versions remain invisible. It adds no generic upload, edit, private
file download, classification or contributor authority. The selector reports
scan release, not a successful identity/background/provenance finding. Submission
must still freeze/revalidate exact owner evidence and contributor history; missing
provenance stays unresolved. Existing pending-review byte authorization is unchanged.

A rollback requires a forward policy migration after removing the consumer; old
migration SQL is retained. Only disposable test application is authorized by this
source work. Required proofs: source/version/workspace mismatch; manager-only
metadata; other Staff/hospital/tenant/category and unreleased exclusion; no general
document access or SQL writes/deletes; attributable reads; and normal manager UI
selection followed by independent reviewer/scanner acceptance.

## Manager submission and historical receipt UI candidate

The source workspace now selects applicants by Staff ID/name, verifies the owner
identity basis, chooses released identity images through the selector above, and
requires policy/finding references, observation date, validity, account intent and
human identity/association attestations. It never substitutes a raw file-ID form
or general document access. Policy/validity have no invented defaults. Digital
subject input is explicitly a declaration supplied by access administration; it
is not automatic provider verification or login linking. The form preserves dirty
input and pins the review ID, command and full source/file payload for uncertain
retry; definitive invalid input remains editable and source/access refusal clears
private context. Completion notices reset on scope/access changes.

Historical receipts use the existing protected history route, with canonical
ownership, strict descending versions, unique revisions, bounded cursor envelope
and abort handling. The UI marks them historical, shows actor/time/reason/receipt
reference and does not consume them as current access. If a page reveals a version
newer than the open finding, new decisions/downloads are blocked until reopening.
The largest seen version survives older pages and history closure. Original
uncertain commands remain retryable for a verified immutable receipt; history
alone cannot clear uncertainty or activate employment.

These are source/component proofs, not real scanner/provider or normal scoped
browser acceptance. Current local runtime and named grants must be verified
before positive maker/checker QA; no expired proposal is reused or extended.

## Proposed owner transport and states

The proposed resource is below the existing owner boundary:
`/v1/workforce/hospitals/:hospital/staff/:id/person-reviews`.
The seven detail/command/history/evidence-content routes in this candidate are
registered by Workforce and verified through signed Global API requests. The
collection route below remains proposed. The canonical input is
`contracts/workforce-person-reviews.json`; its generator produces Go/Zod models.

| Command/read                          | Required behavior                                                                                                                                                                                      |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| PUT `/:review`                        | Submit a complete immutable pending review with UUIDv7 resource/command IDs, exact source and evidence versions and mandatory attestations. No editable server draft or automatic approval is implied. |
| GET collection/detail/history         | Bounded, purpose-scoped, audited current and immutable historical projections for the authorized manager/reviewer. Current effectiveness is distinct from the original receipt.                        |
| POST `/:review/decision`              | Independent approve/reject with expected version, command ID and mandatory reason/attestation. A rejected decision cannot be rewritten as approval.                                                    |
| POST `/:review/withdrawal`            | Attributable withdrawal of a pending submission without deleting its source/evidence/history.                                                                                                          |
| POST `/:review/revocation`            | Reasoned withdrawal of an approved finding's future use; preserve its original decision and dependent review provenance.                                                                               |
| GET `/:review/evidence/:file/content` | Current purpose-bound authorization before and after private storage I/O; no generic file download grant.                                                                                              |

Persist `pending`, `approved`, `rejected`, `withdrawn` and `revoked`; derive
expired/source-changed/evidence-unavailable states without rewriting history.
Approval is only a person finding. It must not change Staff status, create a login
link, issue a card, approve employment or grant an IAM permission.

Require exactly one matching expected-tenant and expected-subject assertion on
every route. Missing/changed workspace returns 409; malformed input/selector 400;
invisible tenant/hospital/object 404; missing scoped action 403; revoked credential
401; unavailable IAM/current identity/owner authorization 503. Exact command
replay reauthorizes and returns its original immutable receipt; reused intent,
stale versions and illegal transitions return a stable 409.

## Transactions, failure and recovery

Use the current five-second scoped-command budget bounded by the session idle
deadline. Preserve lock order: IAM scope/membership, hospital supervision, actor
Staff/link, target Staff, review/evidence rows in deterministic order. Checks
after a lock wait use current owner time, identity hash, employment epoch,
validity and evidence state. Head, immutable snapshot/receipt, minimized audit
and identifier-only outbox commit atomically; an outbox failure rolls back all.

IAM, current identity, PostgreSQL and release-lineage checks are critical: their
unavailability denies the command/read. Storage/scanner unavailability cannot
produce a successful review or private-byte release. Network/provider/storage
calls remain outside transactions, with the original authority pinned across
phases. NATS failure after commit leaves durable publication pending; notification
delivery does not determine identity or access validity. No cache is authoritative.

Use additive migrations, scoped composite foreign keys, immutable histories,
unique scoped command IDs and forced RLS. Extend any existing read predicate
without adding permissive policies or broadening DELETE/write authority. Existing
populated-table indexes use the repository's concurrent-index migration mechanism.
Measure actual plans before performance claims. Application rollback preserves
new review/history facts and denies unsupported use; there is no destructive
down migration that deletes accepted evidence. Production activation requires
the exact validated revision/migrations and specific rollout approval.

## Employment and lifecycle consumption

An approved current-person receipt is an input to the separate independently
reviewed employment submission. That command must freeze the exact receipt and
source epoch, reject expired/revoked/changed findings and preserve historical
self-review exclusions. The reviewed employment transition must explicitly bind
the pre-activation source epoch to the newly created operational employment
generation. Incrementing `employment_epoch` must neither accidentally invalidate
that accepted transition nor authorize unrelated historical reviews.

Suspension, accepted resignation/termination, contract expiry and rehire still
require their actual owner transitions, legacy/governance classification and all
operational consumer guards under ADR-0049. A person review cannot activate those
controls, replace global identity recovery or affect another hospital's valid
employment. Rehire needs a fresh reviewed basis and fresh operational authority;
no old receipt, card or graph tuple may reopen ended access.

## Implementation and acceptance sequence

1. Add canonical contracts, additive owner migrations, source/contributor checks
   and the real signed Global API/IAM/Postgres/OpenFGA regression for an inactive
   joiner. Observe the missing behavior fail before implementing it.
2. Implement submit/read/history and independent decision/withdraw/revoke with
   their complete manager/reviewer UI, required fields and exact retry identity.
3. Integrate protected evidence disclosure with the real scanner/storage worker;
   verify both original-assignment and actor-link replacement during I/O.
4. Prove no-login preparation without fabricated account linkage; historical and
   duplicate-person/self-review denial; foreign scope; stale sources; expiry;
   malformed/oversized commands; immutable SQL history; concurrency/lock waits;
   failed audit/outbox writes; unavailable authorization and uncertain retries.
5. Run affected race/coverage, generators, contractdocs generation/check,
   lint/vet/build, all web/CI gates and normal Admin/independent-reviewer UI.
   Then consume the reviewed finding in employment review and continue all
   lifecycle/consumer gates. No A01–A12 item closes from this design alone.

Rejected alternatives: activate a joiner to obtain an identity draft/link; accept
missing links as independent-review proof; infer person identity from email/name;
duplicate a global person master; silently convert old grants into reviewed
employment; or add a completion button before owner/consumer enforcement exists.
