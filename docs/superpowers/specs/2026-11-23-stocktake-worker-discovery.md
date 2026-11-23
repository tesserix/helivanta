# Reviewed stocktake worker discovery

Status: implementation candidate for #1686; source registration and generated
models are under hosted validation, with no deployment or release claim. Depends on the reviewed authority and continuation work in #1818
and #1822. `pharmacy_stocktake` remains compiled ineligible.

## Requirement and owners

A worker must choose a named pharmacy/outlet under their current reviewed duty,
without knowing identifiers, receiving owner-management access or enabling broad
Stock/Catalogue. The receiving UI currently requires a known outlet link. The
Workforce operating-authority directory and IAM management location choices are
administration interfaces and cannot supply worker authority.

IAM owns duties, acknowledgement, identity/session admission and pagination.
Workforce remains the source for current Staff/link/employment. Pharmacy owns
pharmacy/outlet names, country, timezone and structural versions. There is no new
staff, outlet, stock or authorization master.

Retain ADR-0073's 10 command/s planning workload and 10:1 read/write ratio. Scan at
most 20 candidates plus one pagination probe per request; return no more than 20
choices. Use one four-second discovery deadline inside the five-second Global
API budget. This is a measured acceptance target, not a deployed SLO claim.

## Proposed contract

`GET /v1/iam/me/pharmacy-stocktake-locations` is routed to Platform through the
Global API. It accepts only `limit` (1–20) and an opaque cursor. Require the current
expected tenant and subject headers; they bind the caller's view and never confer
authority. Reject duplicate/unknown/empty filters and cross-account cursors.

Classify discovery as a baseline continuation so retained work can be found when
new stocktakes are withdrawn. Return only the current principal's acknowledged,
active pharmacy stocktake duties that still pass current read admission. A
tenant owner, affiliation or a supplied identifier cannot create a choice.

Use `SealedFilteredListRoute`: scan candidates before current-source filtering,
retain empty-page continuation and encrypt hidden anchors. Order immutable duty
creation time/ID oldest first. Do not stop at an empty filtered page or expose
candidate IDs in cursor plaintext. Multiple roles at one outlet remain separate,
named duty choices; do not invent a combined role or authorize from the UI choice.

Add an optional server-internal exact duty ID to `OperatingScopeRequest`.
Existing business requests keep their current behavior when it is absent;
discovery narrows admission to its candidate. Verify its snapshot ID as well as
the existing scope/version/epoch/graph/credential and Staff/link checks. This
prevents another read duty from shadowing the candidate whose role is displayed.

Within the admitted source callback, Pharmacy supplies current employer/outlet
names, code and timezone from its guarded location projection. Add descriptive
fields to the internal location reference, rather than joining Pharmacy tables
from IAM or copying names into the duty master. Existing structural versions keep
their meaning; names do not confer authority or legal operating eligibility.

Define one canonical IAM response model in `operating-assignments.json` with
owner tenant, duty and location references, employer/location names and code,
country/timezone, template key/label, assigned stocktake permissions, snapshot
revision/epoch and expiry. Generate Go/Zod outputs. Assigned permissions are a
presentation hint; every action independently rechecks its current permission.
Validate required names/timezone and refuse unavailable metadata rather than
displaying an ID as a name.

Audit authorized projection reads under the current actor and exact duty before
releasing a response. An unavailable graph, source, audit or inconsistent
snapshot fails the whole page closed, without partial names. An expired,
revoked, unacknowledged or changed-basis duty is not an effective choice. Preserve
credential-cutoff errors. No PHI, contact details or credentials belong in this
projection or its logs.

## Data and delivery

Check the actual caller/subject access plan under application RLS. The existing
employer-first duty indexes do not establish an efficient subject-first page.
Use measured evidence for any new concurrent index; assign a forward migration
only after rechecking the merged registry. No live DDL follows from this design.
No prior migration or immutable command history may be rewritten.

Named product and quarantine-location projections remain Pharmacy-owned,
read-only, exact-outlet stocktake purposes. Reuse canonical owner schemas and
existing snapshot-selection rules, including inactive products with physical
stock; do not substitute general catalogue or receiving authority. The candidate adds `GET /product-choices` and `GET /quarantine-choices` beneath
the reviewed stocktake root. They reuse receiving product/location wire models,
retain discontinued products, and filter quarantine locations server-side. Both
are stocktake-gated collections (maximum 200) with actor/outlet-bound cursors,
current read-duty admission, stocktake provenance and read audit. They grant no
receiving permission. The SDK validates owner schemas, outlet, quarantine
status, duplicate IDs, page bounds and cursor progress before exposing choices.
Hosted backend validation remains required before acceptance.

The SDK and UI use authority/account/outlet namespaces from #1822. A scope change
removes private results and actions until the next scope is checked. No ID entry,
legacy editor fallback, guessed name or silently fabricated success is allowed.

## Acceptance before release

Red-first Global API tests must prove named current-duty discovery, two duties at
one outlet without shadowing, current-source/graph failures, owner/foreign denial,
expiry/revocation/Staff-link changes, malformed and actor-bound cursors, empty
filtered-page continuation, audit-failure nondisclosure and bounded source calls.
Then verify named product/quarantine choices, the real SDK, interactive operating
and retained-work pages, unknown outcomes and feature withdrawal. Run focused
hosted builds/race/unit/browser checks, protected PR checks, approved migration and
Kargo rollout, and actual deployed browser acceptance. Keep #1686 open until its
complete requested workflow is verified.

## Worker stocktake page

The candidate `/pharmacy/working-stocktakes` page is classified under
`pharmacy_setup`. It resolves the current account, then uses only the current-user
duty directory. Empty filtered pages keep their continuation. Each option names
its pharmacy, outlet/code and reviewed role; no identifier entry or ID-as-name
fallback is provided. A selected duty opens reviewed retained status, original
command recovery and count-authorized cancellation through the existing baseline
continuations. It does not discover through owner-management or legacy outlet
access APIs. General Stock and Catalogue remain disabled.

Account transition, identity refresh/failure, directory refresh/failure, or a
changed duty snapshot removes the prior outlet panel. The panel and recovery
state are keyed by account, tenant, duty, snapshot version and authority epoch.
Current action permissions remain enforced by the owning APIs. The worker-command continuation connects starting, physical entry, independent
review and register export to this page under `pharmacy_stocktake`. Counter
choices expose creation, entry, submission and cancellation; reviewers expose
approval/rejection only for an independent submitted count; readers expose no
commands. These are current-duty presentation hints, never server authority.
The start form selects reviewed named-choice SDK readers when
`authority: "operating"` is supplied. Legacy consumers preserve their existing
behavior. Empty operational lists remain visible even without creation rights.

A missing, expired, withdrawn or failed release snapshot returns to baseline
retained status, original command recovery and counter-authorized cancellation.
An unconfirmed start retains its original open form as well as its tab-local
reference; creating that reference cannot unmount the form. An outlet/account
change removes the old command interface. Compiled eligibility remains false;
this implementation does not claim merged, deployed or browser acceptance.

Hosted 45106386216 passed the named-choice Global API contracts, actual directory
query-plan inspection and existing receiving consumers on `69fdd874`. That plan
uses a small fixture and is not subject-page scale acceptance. UI regression
45106877670 reproduced the legacy catalogue lookup in operating mode. Follow-up
UI validation is running; no merged or deployed UI acceptance is claimed.

Hosted worker-command evidence: 45107898992 reproduced a reviewer receiving
counter-only cancellation; 45108121437 passed after permission separation.
45108396545 reproduced missing released creation, hidden empty reader lists and
review-only physical-entry controls; 45108665090 passed the affected SDK/UI tests,
lint, type checks and shell build after integration. 45108976853 reproduced lost
open-form recovery and retained new-work controls after a failed flag refresh;
45109143051 passed the fixes with 77 SDK and 49 shell tests, Staff visibility
checks, lint, type checks and the affected shell build. Broader protected checks remain required.
