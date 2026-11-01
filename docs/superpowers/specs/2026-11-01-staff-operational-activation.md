# Reviewed Staff operational activation

Status: source activation is implemented in approved production revision
`02b1e2b7`; migrations IAM 0022/0023 and Workforce 0066 are persisted/applied.
`WORKFORCE_EMPLOYMENT_ACTIVATION_ENABLED` retains a compiled default of false.
On 2026-11-02 the product owner explicitly requested production source activation
availability and continued completion of the remaining Staff work. GitOps PR #1619
enables that flag without changing the promoted image, existing grants or schemas.

This availability decision supersedes the original blanket default-off release
instruction for the two source commands. The commands retain their existing source,
independent-review, actor and scope checks and grant no login link or business role.
The full operational-enforcement, legacy/governance migration and consumer cutoff
gates below remain required and unaccepted. See the
[digital browser and concurrency evidence](../../qa/2026-11-02-employment-activation-enablement.md).
Tracks #1035, #417 and ADR-0049; both umbrella stories remain open.

## Story reconciliation

Read the current open [Staff accounts story #417](https://github.com/tesserix/helivanta/issues/417)
and [employee/consultant registry story #1035](https://github.com/tesserix/helivanta/issues/1035)
on 2026-11-01. Both remain open. Their lifecycle, confidentiality, synthetic
walkthrough and non-resurrection requirements remain acceptance obligations.
The Keycloak/schema/route examples in #417 are proposed historical designs, not
current contracts: reuse the implemented IAM identity owner, Workforce Staff and
reviewed links, Global API destinations and published owner contracts. Do not
introduce a parallel Staff/practitioner table or treat the issue's proposed FGA
model, transition schedule or legal narrative as activated production policy.
Imports, report reconciliation and consultant-specific acceptance in #1035 are
separate unfinished Staff scope; activation eligibility does not close them.

## Scope, owners and budgets

Workforce owns the exact approved employment source, employment generations,
effective eligibility and Staff/login binding. IAM owns membership and scoped
assignments. Product owners retain object authorization and clinical/financial
preconditions. Activation must not write another owner's tables or call a remote
provider while holding the shared owner transaction.

Reuse the existing planning assumptions: 500,000 Staff at 12 months, 1.5 million
at 36 months, 50 peak owner requests/second, 99.9% availability and p99 below
500 ms. These remain unmeasured assumptions. Metadata commands are at most 8 KiB;
history pages use bounded keyset continuation. One 1 KiB activation receipt per
Staff is roughly 0.5/1.5 GB before indexes and subsequent generations. No new
service, queue, cache or always-on infrastructure is required.

## Authority and safety boundary

Introduce a separate hospital-scoped `workforce.employment.activate` permission
and independently approved IAM template. Neither the existing HR-intake grant nor
the employment-review grant silently acquires activation authority. The actor
also requires current scoped enrollment authority and an active reviewed login
link. Compound authorization remains bounded to two grants. The original tenant,
subject, hospital, assignments, actor link and source remain pinned through the
command. Self activation is denied using the target Staff/person/login evidence;
an absence of a declared digital subject is not independence proof.

Sensitive assets are Staff identity/evidence, employment state and operational
access. Threats include a stale or revoked actor, a preparer activating unreviewed
terms, a different tenant, replacement evidence and a former worker replaying old
authority. Every owner boundary reauthorizes; unreadable authority denies with
503, missing permission with 403, and foreign/missing objects with 404. Conflicting
source or replay intent returns 409. Private reads and state changes are audited.

## Generation consumption

An attributable activation command selects one exact independently approved
employment-review revision/version and current Staff version/epoch. Recheck the
frozen person finding, identity fingerprint, employment terms, sponsor and released
evidence. Reject unapproved, revoked, changed, expired or not-yet-effective terms.
No evidence or historical dates are fabricated for legacy records.

The activation receipt must durably reference the approved old source and the
new operational generation. The current Staff status trigger increments the epoch
on inactive/active transitions; this change cannot be treated as if the original
finding were independently reviewed at the new epoch. Retain an explicit immutable
consumption relation instead. The receipt, Staff transition, owner audit and
minimal outbox intent commit atomically using the existing guarded tenant command.

Use UUIDv7 command/generation IDs, strict canonical input and an intent digest.
Exact retries return the original attributable receipt under current read authority;
changed intent conflicts. A unique source-consumption constraint prevents two
commands consuming the same approved revision. Lock current IAM authority, hospital
supervision, Staff, source and generation rows in one documented order. The shared
tenant transaction already composes IAM and Workforce; no distributed saga is
needed for this decision. The shared guard port lives in `pkg/authz` beside the
existing scoped command guards; IAM implements the transaction-bound writer and
module contract packages retain data-only payloads.

The target IAM basis lock must precede the hospital supervision lock as well as
the target Staff lock. The current `intakeAuthorityTransaction` acquires
`workforce_lock_supervision` before entering its callback. Adding an IAM basis
write inside that callback would invert the order of a recipient command that
already holds its IAM basis and waits for supervision. Refactor that admission
sequence with a transaction-bound proof and a deterministic lock-wait regression
before adding the activation mutation; do not rely on deadlock retries as the
normal workflow. The source-only read does not create a target IAM basis.
Actor/target basis locks also need deterministic joint ordering. Acquiring an
actor SHARE lock and then a target UPDATE lock can deadlock reciprocal requests
before Workforce rejects an inactive actor. The admission-port design must lock
both subjects in one deterministic order (or provide an equivalent proven
owner-side mechanism), retain the frozen reviewed subject/version, and recheck
all source state in the final command. Add a reciprocal denied-request race as
well as the recipient-versus-activation race; valid business state is not a lock
ordering guarantee.

## Access, effective time and migration

Activation must not create membership, approve a login link, grant an assignment,
issue a card or claim clinical privileges. A digital binding must select the exact
reviewed subject and current generation; a non-digital worker must not acquire a
login through a legacy fallback. New link/assignment decisions must be fresh and
generation-bound. Rehire/resume cannot resurrect historical authority.

Eligibility uses owner time after locks and is checked through the product commit
and after external reads. Contract end, suspension, accepted exit, review
revocation and changed identity/evidence deny affected authority despite old
cookies, stale FGA tuples or failed cleanup. Another hospital's valid employment
and independently controlled governance/recovery remain available. Handover and
settlement obligations cannot extend access past its effective cutoff.

Before enabling operational enforcement, inventory and source-review every affected
HTTP, async, broadcast, timer and external entry point; resolve the explicit legacy
and governance cohorts through attributable independent review. The checked ledger
currently has 734 registered surfaces, 17 classified and 717 unclassified, with
zero full cutoff acceptances. It is not a runtime authorization proof. No activation
control may advertise complete operational access while these gates remain unmet.

## Verification and release

Prove approved-source consumption, exact replay, concurrent consumption, changed
intent, self/foreign-scope denial, stale actor/link/assignment and atomic audit or
outbox failure. Cover future start, exact expiry, review/evidence withdrawal,
suspension/exit, renewal/rehire and independent two-hospital/governance access.
Use real Postgres/RLS and signed Global API requests, then normal Staff Admin,
independent reviewer and recipient UI with released scanner evidence.

Migrations are additive and append-only, tested from the deployed schema. Retain
receipts/history for recovery; rollback does not erase them and requires a separate
owner assessment and named production approval. The existing runtime is reused;
cost grows with receipt/index storage. Backup status is not restore proof. Update
contracts, SDK, UI, owner inventory and progress evidence in the same delivered
slice. A new rollout needs approval for its exact revision and migration manifest.

Rejected: changing the inactive flag as the whole implementation; treating terms
approval as an IAM grant; activating only a non-digital shortcut while legacy
linking or expiry remains unsafe; global identity disablement for hospital exit;
implicitly classifying existing grants as reviewed employment; claiming success
from a disabled activation button or a source-classification count.
