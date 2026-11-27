# Manual evidence and independent person identity review — #169

The product owner approved the recommended manual evidence and independent
identity-review workflow on 27 November 2026. ADR-0077's separate qualified clinical
approval decision remains applicable. This design supplies identity evidence only;
it does not make a person a pharmacist or confer clinical privilege.

## Owners and trust

IAM owns canonical tenant-local person references, account bindings and assurance
history. Zitadel/native accounts retain authentication and account presentations;
Workforce retains employment/account-link cases; Practitioner retains professional
records and registration attestations. No existing HR, membership, login link or
registration row is automatically converted into physical-person assurance.

A tenant may operate hospital-owned departments or an independent pharmacy. Person
identity has tenant scope and requires no artificial hospital or Staff employment.
Clinical consumers still enforce their narrower institution/outlet scope.

Manual evidence is an attributable finding from examining original identity
evidence, with an opaque source-person reference and references to the examination
and finding register. The identity source retains those references and human
findings, not national identity numbers or copied identity documents. Uploaded
professional/Staff documents retain their existing owner and scan/release rules;
the original-examination path cannot release an uploaded file or bypass scanning.
This is a supported manual examination method, not automatic KYC certification.
Its operating policy and actual examiners require accountable applicability review.

## Explicit bootstrap

A source profile must first import at least two independently reviewed people and
their current tenant account bindings from an externally completed manual review.
A schema-owner operator command performs this attributable import using explicit
policy, approval and witness references and explicit validity dates. It defaults
to validation only. There is no public bootstrap endpoint and no implicit owner,
admin, Staff or practitioner conversion. The app database role cannot import root
assurance by setting a context flag. Import grants no role or clinical authority.
Actual live profile content and import need separately named approval; no real
people or verified findings are fabricated during development.

Reviewers separately need current reviewed IAM governance assignments for identity
examination or independent review. The existing governance-grant owner alone is
not an identity-reviewer assignment. The source rejects implicit owner authority
for these new capabilities. Reviewer identity comes from the active source profile,
never distinct account IDs. Different accounts linked to one person remain one
reviewer. Missing/expired/revoked profile, assurance, membership or assignment denies.

## Workflow

1. A current authenticated tenant account submits its own case against an explicit
   active source profile/version. The command retains that membership incarnation
   and consent; the applicant supplies no verified-person or clinical authority.
2. A separately assigned examiner, with current manual person assurance, records
   original-evidence findings, a stable source-person reference, the proposed
   canonical person, observation/validity dates and examination references. These
   are manual findings, not client-conferred authority. Known aliases and potential
   duplicate identities must be resolved; a new UUID is not evidence of a new person.
3. An assigned independent reviewer examines the exact evidence revision and makes
   an attributable approve/reject decision. Its canonical person must differ from
   both examiner and target person, including their other accounts. The source
   reference cannot be assigned to another canonical person. Approval binds only
   the applicant's authenticated account and retained membership incarnation.
4. Additional accounts submit their own cases and are bound to the same canonical
   person after fresh independent review. A conflicting existing binding cannot be
   overwritten. Revocation and future identity reconciliation preserve history and
   invalidate current use rather than erase facts.
5. Clinical-policy and dispensing consumers obtain current, locked identity
   references through an IAM-owned contract. They separately require professional
   registration, local privilege/designation and their actual operating assignment.

Every command has a retained actor/intent-bound receipt, expected version and legal
state transition. Reads/history are bounded, account-bound, no-store and audited
before disclosure. Human findings/reference contents stay out of ordinary events
and logs. Unknown sources remain unavailable. No UI uses account/person IDs as
names; native account presentations are reauthorized and failures are explicit.

## Storage and delivery

Use forced tenant RLS and source-purpose gates for profiles, people, account
bindings, self-submitted cases, immutable evidence findings, immutable decision
receipts and revocations. Preserve the IAM assignment-target/discovery purpose
restrictions and validate existing metadata seals before extending them. A
shared/exclusive tenant source lock serializes source changes against current use;
all operation deadlines and source validity must be enforced at the consumer commit.

Routes remain compiled unreleased during implementation. The operator command is
not executed live as part of development. Add canonical Go/Zod inputs and receipts,
real hosted PostgreSQL tests, focused local builds and UI/SDK evidence. No local
application, database, Docker or Colima is started.

Required assertions include second-account self-review, wrong scope, changed
membership/profile/evidence, exact replay after later transitions, conflicting
commands, concurrent decisions, source expiry/revocation, audit failure and no
clinical privilege from identity assurance. Full #169 additionally needs qualified
policy approval/publication, review decisions/second check/prescriber resolution
and atomic enforcement in the actual sale/stock/financial commit.
