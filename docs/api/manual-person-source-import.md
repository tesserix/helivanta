# Manual identity source import — operator workflow

Refs #169 and ADR-0077. This is an unreleased IAM operator command for importing
externally completed, independently reviewed person/account findings. It is not
KYC certification, a registration check, a role grant or clinical authority.

## Ownership and approval

IAM retains the tenant-local source profile, canonical person references, account
membership incarnations and immutable import receipt. Native identity retains
account authentication and names; Workforce and Practitioner retain employment
and professional evidence. The input stores opaque policy, review, witness,
source-person and finding references. Never put identity numbers, credentials,
identity-file copies or private clinical data in those references or reason text.

The actual source, externally reviewed root people and a live import require
named approval. Development or approval of this workflow does not supply that
evidence. Do not invent verified people or convert tenant owners, Staff records
or professional attestations into root assurance.

## Validate and apply

1. Prepare a private document using `ManualIdentitySourceImport` in
   `contracts/manual-person-identity.json`. Include the tenant, source profile,
   explicit policy/review/witness references, original observation and validity
   interval, independent review confirmation and at least two distinct people.
   Each root has a stable source-person/finding reference and its current account
   subject and membership incarnation. Multiple accounts for one person belong
   under that same person; they are not independent reviewers.
2. Validate without opening a database connection, from `backend/`:

   ```sh
   go run ./cmd/manual-person-source --file /secure/manual-source.json
   ```

   `valid_document_only` means syntax, bounded fields and current time validity
   passed. `membership_checked: false` is explicit: it proves no live account,
   membership, qualification, evidence authenticity or source import.

3. After the named source/import approval, use the configured schema-owner
   connection supplied through the existing managed-secret configuration:

   ```sh
   go run ./cmd/manual-person-source --file /secure/manual-source.json --apply
   ```

   The application database role cannot bootstrap a source by setting a context
   flag. No bootstrap HTTP endpoint exists. The CLI emits a receipt only after
   the outer owner transaction commits. It reports no identity success on error.

4. If output was lost, repeat the same document under the same operator database
   role. Exact retained intent returns its original receipt and adds a replay
   audit event. Reusing the source profile with changed intent, or importing a
   second source over an existing head, conflicts. Do not change IDs to evade a
   conflict. Retained replay does not renew validity or reactivate authority.

Input is bounded to 64 KiB and rejects unknown/duplicate fields, malformed UTF-8,
unsupported UUIDs, controls, invalid precision and ambiguous timestamps. Errors
and default output exclude the source contents. Keep the private input outside
Git, logs, issue comments and CI artifacts.

## Persistence and failure boundary

Migration `0058_iam_manual_person_source_import` creates forced-RLS source,
people, binding, head and receipt tables. Only the actual table-owner identity
under the explicit tenant import purpose can read/write these records. Existing
IAM assignment-target and operating purposes cannot access them. Prior IAM
integrity seals are validated before DDL, then refreshed for the new restrictive
policies; the source's own policies, functions and triggers are sealed too.

Import serializes each tenant's source and locks root membership incarnations in
stable account order. Source/person reference uniqueness and compound foreign
keys prevent conflicting mappings. Records cannot be updated, deleted or
truncated through this command. No membership, role or governance grant changes.

An audited transaction creates all records and its receipt. A deferred constraint
checks the completed import, root/account bounds, current membership incarnations
and evidence validity at the actual commit. Audit failure, stale membership,
incomplete records and expiry roll back the import. A prepared receipt is not a
committed receipt. No external service or event is claimed completed by it.

## Remaining workflow

This initial operator import has no replacement/revocation command and no
application-role source reader. It is not yet usable by clinical consumers.
Account self-cases, examinations, independent review, revocation/replacement,
explicit reviewed grants, audited named UI, consumer commit-time enforcement and
full dispensing/stock/financial acceptance remain required. Keep the feature
unreleased and #169 open. No live import or migration is executed by these docs.
