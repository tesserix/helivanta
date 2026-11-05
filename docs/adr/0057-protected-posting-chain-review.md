# ADR-0057: Protected current posting-chain review

Date:2026-11-04. Status: implemented candidate, acceptance and rollout open.

Workforce owns Staff and postings; Organization owns current department ancestry;
IAM owns assignments and OpenFGA acknowledgement. Departmental consumers need an
independent reviewer to examine current canonical supervisors before approval.
Ancestor private decision contexts disclose unrelated rationale and are unsuitable.

Use the opt-in protected context selector `chain_review=current`, with exact
pending revision, supervision_context=v1 and review purpose. Hold current scoped
posting.read + posting.review + Staff actor authority; reuse the same source,
interval, cycle, depth and Organization guards as approval. Return at most31
ordered minimal supervisor nodes, canonical Staff names, checked_at and the
minimum nominal/source cutoff. Serialize at most128KiB before committing the read
audit. The label lookup uses exact tenant/hospital/Staff/proposal composite keys
against the existing primary key, under the existing read-only discovery context;
restore that context before committing the selected-target read audit. No schema,
service, cache, permission bundle or cross-owner table access is introduced.

The reviewer has administrative posting-review authority within an approved
hospital, an independent Staff identity and current membership/source. This does
not confer managerial or clinical authority. ORG-AU-05/07 and ORG-ST-02/03/05 apply.
Insiders and foreign accounts must not gain ancestor rationale, employment evidence
or a reusable authority token by selecting references. Scope and purpose are checked
on the server; the SDK also rejects disconnected, repeated, foreign or expired
chains. The snapshot does not reserve approval; the command rechecks all sources.

Keep root-management attestation on root commands. The departmental UI separately
requires chain/source examination and uses a dedicated SDK command/recovery helper
which does not claim management review. Unknown outcomes retain exact command and
payload. Rejection remains available when chain validation conflicts.

Reuse ADR0056's unmeasured50 peak requests/s,80:20 read/write,99.9% availability,
p99 below500ms and500,000/1.5million Staff at12/36 months. At most31 source traversals
and one bounded batch of indexed labels; marginal cost is owner reads/audit storage.
IAM, Staff, Organization or database failure withholds the view; missing capability
403, invisible object404, malformed selector400, changed source409, unavailable
authority503. Audit failure rolls back the entire read/actor-use transaction.

Discovery pages are hints, not current-chain proof. Separate ancestor detail calls
would leak private context and fan out requests. Copying employee or department
masters breaks ownership. The existing guarded owner projection meets this scope.

No database migration is added. Rollback removes the opt-in selector and consumer
UI while retaining immutable history. Production rollout still needs approval of
the exact merged revision. Real-role browser QA, remaining lifecycle/operating
consumers and full Staff acceptance remain open. Detailed acceptance and steps:
[spec](../superpowers/specs/2026-11-04-posting-chain-review-context.md),
[plan](../superpowers/plans/2026-11-04-posting-chain-review-context.md).
