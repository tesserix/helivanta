# Versioned Staff profile master connections

Status: implemented and tested locally; not deployed. Continues #1035, ADR-0016 and the
Staff acceptance ledger. Other consumers and the full Staff goal remain open.
Applies ORG-AC-01/02/05/06/07/08; no India statutory policy is activated.

Workforce owns hospital HR masters, immutable revisions and protected enrollment.
Keep the Global API and existing enrollment route, generated contract and
permission plus locked explicit hospital-manager authority and forced RLS.
Baseline source resolved department/designation/grade pins; language and exit
reason were text, and contract type had no profile consumer. Engagement type
is a separate fixed vocabulary and must not be reinterpreted.

## Contract and transitions

Add optional `preferred_language_revision_id`, `exit_reason_revision_id`,
`contract_type` and `contract_type_revision_id` through the canonical
`contracts/workforce-enrollment.json` generator. A canonical `profile_masters`
mapping generates both Go reference bindings and TypeScript selector metadata,
including null compatibility and selection-only validation; the mapping is not
accepted from request bodies. New input fields use an optional third tuple
member so SDK parsing preserves omission rather than generating empty-string
clears; form defaults remain explicit for the updated UI. Existing language/exit strings
retain their limits and legacy behavior. Contract type is a new server-derived
label, at most 80 characters, requiring its typed revision. It is not legal
terms, a signed offer, payroll eligibility or employment/access activation.

Resolve labels from the same tenant/hospital and expected master category. New
pins require active current revisions; existing pins retain historical/retired
revisions. Omission by older clients preserves the pin; explicit empty strings
clear it. New revision properties reject null as the generated string/UUID model
does; original reference null semantics are not silently tightened. Preserve
expected-version and exact retry behavior. Hidden/wrong-kind/foreign references
return opaque 404, invalid/inactive selection 400, stale changed enrollment 409;
authority failures retain existing fail-closed behavior. No new route or role.

## Response compatibility review

An older enrollment editor spreads the returned profile into a strict form
schema. Merely omitting new empty fields does not protect older editors when a
profile carries populated pins. Preserve the default response profile shape;
the updated editor opts into the extension with `profile_masters=v1` on GET and
PUT. Unknown, empty or repeated extension versions fail with 400 before writes.
This is a projection selector, never authorization or a request to clear pins.
Legacy request omission continues to preserve all new references and canonical
labels, including when the default response hides the new metadata. Opted-in
responses carry `profile_masters_version: "v1"`; the updated editor checks this
marker and includes the version in its query-cache key. A rolling/older API that
ignores the selector must not make unknown references appear unassigned or allow
an edit from an unconfirmed representation.

Declare the extension query/version in the canonical enrollment contract and
generate both Go/TypeScript constants and the legacy profile projection from the
same fields. Keep the owner input and transaction models intact; apply response
projection only at the HTTP boundary so fingerprinting, stored references and
outbox behavior cannot change with response selection. No new endpoint, role,
network hop or infrastructure. Add regressions against actual HTTP bytes for
both populated and empty profiles, omitted-client edits, replay, invalid/repeated
selectors and absence of partial writes. Recheck the new editor's opted-in path
and actual local UI after implementation. The focused actual-HTTP and component regressions passed after their intended
red runs. Broader local checks and updated runtime compatibility UI also passed;
remote CI and release remain pending.

## Persistence and mixed-version behavior

Append `0040_workforce_profile_masters`: nullable UUIDs, new contract-type text,
typed composite foreign keys including tenant/hospital/category and three
reference indexes via separate `0041_workforce_profile_master_indexes` concurrent-index metadata. Fixed category
columns use constant defaults/CHECKs to avoid unnecessary stored-generated
column rewrites. No matching, backfill or conversion of existing text.

A distinct profile-master compatibility marker is required before replacing a
profile carrying a new pin. Existing binaries already set the older generic
HR-reference marker; reusing it would let them erase unknown fields. Retain
both guards. Deploy schema, then the updated API fleet, then expose the shell
fields. App rollback retains columns, pins/history and the guard; older writes
to affected profiles fail closed. No destructive down migration or unpinning.

Profile writes, staff version, minimal audit and metadata-only outbox stay in
one owner transaction. No network call under locks and no new event PII. Stable
category-order SHARE locks serialize selection against retirement/rename; an
identical retry creates no extra enrollment change.

Assets are classifications and historical HR facts. Threats are foreign-tenant
or hospital users, wrong-kind references, stale clients and mixed-version
writers. Server authorization and SQL constraints protect those boundaries;
selection controls grant no clinical or organizational authority.

## Capacity, UI and evidence

Use the existing provisional 2,000-staff/hospital envelope. Three UUIDs, an
80-character label, category values and three B-tree keys add conservatively
less than 2 MiB/hospital; there is no autonomous 12/36-month record growth.
Six indexed master resolutions bound each update, three more than today. No
new HTTP hop, infrastructure or paid dependency. Platform 1,000 peak RPS/80:20
planning is not measured HR demand. Preserve existing route availability and
measure representative owner queries before claiming latency improvements.

Extend the existing selectors for language, exit reason and contract type,
including recorded text/revisions, pagination and retry. Contract type is
selection-only. Do not invent universally required government IDs, contract
classifications or exit reasons; legal approvals/terms stay separate.

Failing-first real-DB/HTTP tests must prove canonical labels, exact replay,
current/active restrictions, historical preservation, old-client omission,
explicit clearing, malformed/null input, wrong-kind/hospital/tenant denial,
private directory/event non-disclosure and the mixed-version database guard.
Frontend tests and synthetic Staff Admin UI must verify submitted IDs, edit,
reload, persistence and independent-role denial before release claims.

Relationship, repeatable qualification and private-document category consumers
have different limits/retention controls and remain separate explicit work.

## Remaining consumer research

The canonical sources show three distinct follow-ups under #1035:

- Emergency-contact `relationship` is limited to 60 characters, while the HR
  master name permits 80. A connection must define compatible label handling;
  never truncate a valid master label or silently widen the v1 response contract.
  Preserve references by stable contact ID, not array position or name, when an
  older client omits newly introduced child fields.
- Repeatable qualification text permits 150 characters and an 80-character
  master label fits. Pins must still follow the stable qualification row ID,
  immutable revisions, current/retired selection rules and existing evidence
  anchors. A qualification classification is not a reviewed credential or
  clinical privilege. Child-row replacement needs its own older-writer guard.
- Private-document `category` is an allowlisted evidence purpose from
  `workforce-documents.json`, not the configurable HR label. An arbitrary
  document-category master must not replace that purpose, select a storage path
  or change release authority. Any additional classification needs a separate
  typed reference and must retain quarantine, exact-content scanning,
  record/version anchors, independent review and protected release behavior.

Each child consumer also needs compatible default/opted-in response projections;
adding unknown properties to strict repeatable-item schemas repeats the editor
failure reproduced above. These are source findings for the next slices, not
implemented consumers or activated HR policy.
