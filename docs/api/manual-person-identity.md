# Manual person identity — unreleased implementation

Refs #169 and ADR-0077. IAM owns tenant-local physical-person assurance; native
identity owns authentication and names, Workforce owns employment and Practitioner
owns professional registration. No identity finding confers clinical authority.

Canonical models live in `contracts/manual-person-identity.json`. Run
`node scripts/generate-manual-person-identity.mjs` and its `--check` variant.
The generated models are not registered HTTP contracts or released endpoints.

## Implemented internal rules

- Source import validation requires an explicit policy, examination/review/witness
  references, independent root-review confirmation and at least two distinct
  canonical people. Root account subjects and source-person references cannot
  conflict. Each account retains an explicit membership incarnation.
- A self-case requires consent and an exact source-profile version. Its account
  subject and membership incarnation are separate server-derived arguments,
  never fields accepted in the submitted case intent.
- Examination requires original-evidence and association findings, attributable
  references, nonfuture observation and unexpired evidence. It advances only an
  exact nonterminal case version under a current source and assured examiner.
- Independent decisions require the latest examination and exact case version.
  A reviewer must be a current assured person distinct from the target and every
  evidence contributor, including contributors to superseded examinations.
  Changing accounts does not satisfy person independence.
- Approve/reject advance the case once and retain `clinical_authority:
not_evaluated`. Internal transitions return a new value without changing their
  input. No stock, registration, clinical or financial action occurs.
- Web validation preserves exact command text, rejects controls/blank reasons,
  and enforces the same UUIDv7 resource references and integer bounds as Go.
  Time-sensitive and owner-derived constraints require server evaluation.

These internal functions operate on snapshots. They do not authenticate those
snapshots, acquire locks, recheck current membership or provide transaction
commit-time enforcement. A future database-backed command must load and lock
current owner records, verify tenant/source/account/person incarnations, audit
disclosure and effects, retain replay receipts and enforce the validity deadline
at commit. Unit tests of these rules do not prove those boundaries.

## Operator source persistence

The [source import command](manual-person-source-import.md) adds unreleased,
operator-only source/person/account persistence and retained import receipts.
Application-role source reads, case persistence and clinical consumers remain
unimplemented. The command grants no membership, role or clinical authority.

## Remaining delivery

Account-case/examination/decision persistence and purpose guards; source
replacement/revocation; explicit reviewed examiner/reviewer grants;
account-bound APIs and audited named projections; SDK and complete review UI;
revocation and retained replay; hosted real-database negative/concurrency tests;
qualified policy approval/publication; prescribing resolution and mandatory
verification inside the actual dispensing, stock and financial transaction.

Migration `0058_iam_manual_person_source_import` is proposed for the source-import
change. No live migration, profile/root import, feature activation or deployment
has been performed. #169 remains open.
