# Workforce department handoff

Source candidate, 2026-11-02. No production rollout or migration is approved or
claimed. [Design](../superpowers/specs/2026-11-02-staff-department-handoff.md) ·
[plan](../superpowers/plans/2026-11-02-staff-department-handoff.md).

Workforce owns HR vocabulary and immutable profile/payroll references.
Organization owns the structural department/sub-unit tree. This additive mapping
records how an exact HR department revision corresponds to an exact structural
revision, retaining history without changing existing profiles or payroll. It is
not a posting, clinical approval, professional review, current eligibility fact
or IAM/OpenFGA grant. Staff postings and Pharma extensions remain separate work.

## HTTP and authority

All paths enter Global API and route to Workforce. Every route requires current
verified session tenant membership and OpenFGA `workforce.master.manage`.
Current explicit hospital reader/manager access permits reads; manager is required
for writes and exact retries. The grant is locked for the owner transaction.
Missing route permission is 403, absent/foreign hospital or object is 404,
unavailable authorization is 503. Expected tenant/subject assertions use the
existing Workforce header checks and do not supply authority. Responses are
`no-store`; reasons and actor subjects are omitted from public receipts/history.

| Method and path | Behavior |
| --- | --- |
| PUT `/v1/workforce/hospitals/:hospital/hr/masters/:id/department-links/:request` | Prepare or recover the exact retained actor/intent command. |
| GET same path | Read the retained receipt under current authority. |
| GET `/v1/workforce/hospitals/:hospital/hr/masters/:id/department-links` | Newest-first bounded keyset history with standard `limit`/`cursor`. |

Request ID, department ID and department revision are UUIDv7. Hospital, HR master
and HR revision retain their existing nonzero UUID formats. Body is one strict
JSON object, limited to 8 KiB:

```json
{
  "expected_version": 0,
  "master_revision_id": "44444444-4444-4444-8444-444444444444",
  "department_id": "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb",
  "department_revision_id": "019ccccc-cccc-7ccc-8ccc-cccccccccccc",
  "reason": "Reconcile the retained HR classification with the canonical tree"
}
```

Required mapping version is a safe integer, 0 through 9007199254740990. New
commands require the current active **department** HR revision, serialize by
tenant/hospital/master and increment the mapping sequence exactly once. A pending
request blocks another command for that HR master. A non-department source is 404. Stale source/version, changed
actor/intent reuse or a conflicting SQL guard is 409.
Missing/null/invalid fields are 400. Audit/outbox or unexpected persistence
failure is 500 with a generic message; it is not mislabeled as invalid user input.

Receipt fields are `request_id`, `hospital_id`, `master_id`, `master_revision_id`,
`department_id`, `department_revision_id`, positive `version`, `status`,
`created_at` and optional `observed_at`. Status is `pending`, `matched` or
`rejected`; only terminal receipts have `observed_at`. First observation is
immutable. A matched receipt is historical evidence that the exact structural
revision was active/current at the owner observation. Later HR edits, structural
changes or retirement never rewrite it or make it current operating authority.

Exact retries reauthorize and return the retained receipt even after source
changes. Pending retries re-publish the same identifier-only claim without
creating another request/version/audit. The original actor and normalized reason
are part of the fingerprint. A browser timeout is ambiguous: the UI retains its
original key/input/version across history refresh until it receives a verified
receipt. Recovery never silently substitutes a new command for unchanged input.

## Owner messages and storage

- Workforce publishes `helivanta.in.workforce.department_link_requested.v1`.
  Its data contains request, hospital, HR master/revision and department/revision
  IDs only. No Staff/person, private label, actor or reason is published.
- Organization retains the request identity and first result in
  `organization_department_observations`, then atomically audits and publishes
  `helivanta.in.organization.department_link_observed.v1`. The answer adds only
  explicit `matches_current` and owner `observed_at`.
- Workforce applies that answer only to the exact retained request, in its own
  tenant transaction. Missing/null/non-boolean result, malformed/oversized/extra
  payload, directed/unsupported envelope or contradictory identity/first answer
  is refused. Exact duplicate answers converge. Replayed old answers cannot
  change a newer request or overwrite a terminal receipt.
- Pending retries repair a lost answer by re-publishing the retained observation
  at Organization. They do not re-evaluate it against a later tree. Existing
  dispatcher/consumer retry and dead-letter operations remain applicable; broker
  availability is not operating-eligibility enforcement.

Canonical models/events are in `contracts/organization-structure.json` and
generated with `node scripts/generate-organization-structure.mjs` (`--check`
supported). Workforce model output is `department_link_types.go`; Go event and
Zod outputs share that source. The SDK validates exact receipt identity/version,
status/observation consistency, normalized scope, bounded unique history and
continuation before exposing data.

Forward migrations: `0070_workforce_department_links` and
`0004_organization_department_observations`. Forced RLS, request identity,
terminal decisions, sequence/pending constraints, source FKs and retained history
are database enforced. They are append-only; rollback removes exposure/consumers
while retaining additive schema/evidence, rather than dropping history. No
cross-owner table reads or writes are used. Existing HR/enrollment IDs and the
legacy field semantics are unchanged; the explicit mapping supplies lineage for
the forthcoming canonical posting migration.

## UI and acceptance boundaries

HR setup offers **Organization mapping** for HR department records. A fresh
identity/permission check governs the panel; Organization read permission
independently governs its canonical picker. Hospital readers can inspect history,
managers can map active HR revisions, and retired records remain historical.
The UI shows owner-pending, matched-snapshot and rejected states, pinned revisions,
UTC observation time and bounded history/department paging. It requires a reason,
guards unsaved navigation and hides protected results after access refusal.

Focused real Postgres/OpenFGA/NATS tests verify the two-owner path, stale source,
retained/contradictory answers, missing explicit false, role revocation and outage;
Workforce tests cover required fields, retry/concurrency, scope, immutable SQL and
audit/outbox rollback. SDK/form tests cover owner receipt validation, required
fields, scope denial, preserved retries and the reproduced lost-reply defect.
Full checks and normal local browser evidence are recorded separately in the
Staff delivery tracker when complete. These tests do not establish production,
complete HR-reference migration, reviewed operational postings or Pharma duties.
