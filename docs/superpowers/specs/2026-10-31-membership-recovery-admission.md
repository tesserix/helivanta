# Prevent implicit restoration of ended tenant membership

Status: local implementation candidate for A02/A11; no production approval.
The full A01–A12 objective remains unchanged. Builds on the retained membership
command, identity eligibility and legacy contract migration specifications.

The legacy POST inserts a missing role without checking retained continuity.
After final removal, it can therefore change member_count from zero to one and
restore membership without the explicit recovery command or its verified-human
identity check. Retaining the old DELETE refusal does not close this second path.

IAM owns continuity and membership; identity/contact proof belongs to Zitadel.
An authenticated current tenant administrator still needs iam.member.manage.
The affected scope is the verified tenant and exact subject. No home-org/email
claim, another tenant's history or HR title is membership authority. This change
adds no clinical permission, governance appointment or hospital employment action.

Add a forward-only IAM migration after 0018. A BEFORE UPDATE guard on the existing
continuity row refuses the zero-to-positive member_count transition unless the
existing explicit recovery command and removal-basis context are present. Its
existing AFTER membership/command triggers still require attributable current
administration, a retained exact-subject removal, fresh ordinary staff_member
membership and immutable command/epoch evidence. Do not duplicate their policy or
weaken those constraints. A named integrity violation is mapped to a bounded,
no-store 409 membership_recovery_required response on the legacy HTTP route.

Enforce this at the row transition rather than a prior application read. The
existing membership UPSERT already serializes through that row, including when
a caller initially observed no continuity row or waited behind removal. No new
lock namespace, provider call inside a transaction, copied counter or mutable
recovery flag is introduced. A refused insert rolls back membership, continuity,
audit and outbox. Existing current-row duplicate repair remains possible and
explicit ordinary recovery continues to use the existing command/receipt path.

Planning envelope remains 10 peak membership commands/s per large tenant, 4 KiB
new-command input and 100:1 reads/writes. The additional guard reads transaction
context on an already locked indexed continuity row; it adds no table scan,
provider request, datastore, worker or recurring infrastructure cost. Operational
latency and full projection/UI acceptance remain separate release gates.

Raw schema rollback would reopen this demonstrated bypass. Keep the migration
and evidence, use forward corrections, and deploy only the exact reviewed source
and migration set after named approval. Older binaries may return an internal
error for the new guard, while remaining fail-closed; coordinate handler/migration
compatibility. This is a tightened legacy recovery contract, not proof that an
external sunset/security exception has been approved.

Validation must reproduce the HTTP restoration first, verify named structural
refusal under real PostgreSQL/RLS, preserve all resulting rows/audits/events on
refusal, test supported recovery and current-row repair, tenant isolation and
concurrent removal/restoration. Run affected race/build/vet/lint and canonical
contract documentation checks. Browser/deployed evidence remains distinct.

Fresh legacy provisioning, privileged role expansion, pre-continuity history
classification and all external callers still need coordinated migration; this
guard does not establish verified first enrollment or complete A02/A11. Rejecting
only in Go was rejected because a read/insert gap or another insertion caller
could reopen the same authority. Restoring old grant tuples or epochs is not
recovery. No global account disablement or independent hospital cutoff is added.
