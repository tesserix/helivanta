# Independent stock operating duties

Unreleased implementation candidate; no production activation or full supplier-workflow completion is claimed. See [ADR 0064](../adr/0064-independent-operating-assignments.md) and [independent Staff identity](workforce-independent-identity.md).

Shared IAM owns the duty, immutable decisions, graph snapshot and acknowledgement. Workforce owns current Staff/account identity and employer access. The physical products own the outlet/premise and all stock records. Supplier IDs are never exposed as hospital IDs.

## Current candidate routes

Paths are relative to `/v1/iam/operating-duties/{duty}`. IDs are UUIDv7. Expected tenant and subject headers must each match the authenticated principal exactly. Responses are private, uncached and attributable.

| Method | Path                  | Behaviour                                                                            |
| ------ | --------------------- | ------------------------------------------------------------------------------------ |
| PUT    | empty                 | prepare an immutable duty for one exact current Staff/link and physical source       |
| GET    | empty                 | current lifecycle record; lifecycle activation is not a reusable authorization proof |
| GET    | `/proposal-receipt`   | maker-bound original preparation receipt, retained after later state/expiry changes  |
| PUT    | `/commands/{command}` | independently approve/reject, maker-withdraw, or revoke using expected version       |
| GET    | `/commands/{command}` | actor-bound immutable decision recovery                                              |

[operating-assignments.json](../../contracts/operating-assignments.json) owns the strict DTOs and initial templates. Run `node scripts/generate-operating-assignments.mjs` and `--check`. Shared identity descriptors derive from the Workforce canonical inputs. The generator also produces the database template-permission function; clients cannot supply arbitrary permissions.

The Staff-private directory is `GET /v1/iam/operating-employers/{kind}/{employer}/staff/{staff}/duties`. It requires the same current read and private employer-reader authority. It accepts only one `limit` and optional signed `cursor`, orders by descending `(created_at,id)`, and audits the disclosure. Staff and employer identifiers filter owner data; they never supply authority. SDK pages request at most 25 rows and reject duplicate, foreign-owner or inconsistent continuation results. Duty and retained decision payloads include `owner_tenant_id` for explicit workspace verification.

Initial templates are `supply.inventory_receiver.v1` and `pharmacy.inventory_receiver.v1`. Both permit own-source quarantined receiving/history and the published stock-reference purpose. They do not authorize release, dispensing, dispatch, hospital/patient access or financial approval. The pharmacy template does not yet have a product consumer in this candidate; its presence is not working pharmacy end-to-end evidence.

Preparation/revocation requires an actual IAM owner/admin or pharmacy-owner membership with the corresponding permission. Compliance can review; auditors can read. TenantAdmin is implicit. Current employer-manager access is additionally mandatory for new decisions, and reader/manager access for retained reads. The IAM membership and employer access rows remain locked through the command, so deletion cannot race a completed authorization check. Structural employer registration remains a separate TenantAdmin bootstrap action.

The target must have uninterrupted current IAM membership and a current active, independently reviewed Staff/account link under the typed employer. Preparation binds exact Staff/link, organization and premise/outlet versions. A separate reviewer approves that immutable basis; raw maker/target identity and known historically approved aliases cannot satisfy independent review. The original proposal remains immutable when withdrawn, rejected or revoked.

## Projection and current use

Approval responds `approved_pending_activation` and commits the snapshot, history, immutable command receipt, audit and outbox together. External graph publication runs after commit. Partial or unavailable projection leaves the duty ineffective and retries the same immutable key. Acknowledgement rechecks current membership, source and Staff basis under the owner command. A changed basis or expiry revokes the pending revision; a delayed acknowledgement cannot undo explicit revocation.

`active` means the exact projection was acknowledged. Each product action must still call the current operating authorizer. That check combines the graph with the exact current duty/epoch/acknowledgement, time interval, uninterrupted membership, current source and current Staff/link revision. The current credential-revocation watermark is checked under the existing credential lock through commit. Ordinary sign-out invalidates the old session; it does not erase a still-valid business duty.

The callback receives only a typed scope, permission, duty/snapshot/epoch and Staff/link reference. Private IAM and Workforce database purposes are restored before product work. No scoped permission enters tenant-wide resolution. Source/identity/graph failure denies the action; unavailable integrations are not represented as an empty or zero-valued stock report.

## Product consumption and limits

SupplyConnect receipt recording, bounded receipt reads, scoped barcode lookup and identity reconciliation accept this current operating scope in addition to their existing administrative observation path. The scope is rechecked for every request, including GET recovery. The stock owner sets only an exact receipt-purpose database context; it does not grant access to unrelated party, bin, delivery or directory APIs. Original intake remains quarantine with zero available units after product identity matching.

The drug-master command-bound reference holds the publisher's source lock through the consumer transaction's commit. Its system read cannot release the only publication lock before the stock write commits. Reconciliation preserves the observed receipt and canonical published pack/unit basis; it does not make stock sellable.

The independent Staff editor now lists scoped duties and supports approval, rejection, maker withdrawal and revocation. Supplier preparation chooses an actual current warehouse/depot and a reviewed account, binding their current versions. The SDK validates exact preparation/decision receipts; ambiguous saves freeze intent and recover using GET, while an explicit rejected original write allows refresh. Read refusal hides private results. These are component-test and real-owner API results, not completed OIDC/browser acceptance for a receiver-only account.

This candidate still needs receiver-only navigation and browser acceptance, pharmacy product consumption, qualified quality release/balances/reservations/dispatch, bilateral supplier grants, hospital receiving/discrepancies and financial composition. Current licence, professional and exact Maharashtra/Odisha policy checks remain necessary at the applicable commitment boundaries. These administrative/identity records are not those checks.

Candidate IAM migration: `0038_iam_operating_duties`; candidate source migrations remain unreleased. Prepare and verify the final public-repository release, exact migrations and Kargo image promotion before seeking the required named production approval.

### Stock-quality duty expansion

The additive `supply.stock_quality_reviewer.v1` template permits source-scoped
`supply.operations.read`, `supply.stock.review` and
`drugmaster.stock_reference.read`. It follows the same independent Staff/link,
review, projection acknowledgement and current eligibility gates as receiving
duties. It does not inherit receiving or dispatch permissions. See
[the stock review contract](supply-stock.md#independent-stock-quality-review-candidate--8-november-2026).
The original template SQL is retained in `operating_templates_v1.sql`; future
catalogue expansions apply through additive migrations rather than rewriting
migration 0038.

The next order candidate adds `supply.order_acceptor.v1` and
`pharmacy.order_acceptor.v1`, permitting source-scoped
`supply.supplier_order.accept` and `.read` only. They grant no receiving, quality
review, reservation, dispatch or financial authority. IAM 0043 applies the
expanded function; `operating_templates_v2.sql` retains the exact published 0041
function bytes. See [the supplier order candidate](supplier-orders.md).

## Staff preparation workbench

The Staff editor prepares duties for both independent pharmacy employers and
supplier employers. The operator selects from the canonical catalogue filtered
by employer kind, including receiving, order acceptance and physical fulfilment;
warehouse quality review is a separate template. No arbitrary permission list is
accepted. The exact active reviewed account/Staff versions and selected current
outlet or warehouse/depot are submitted to the existing IAM owner API. Pharmacy
structural organization/outlet versions are immutable version 1, as enforced by
PharmaConnect's current operating-location guard; regulatory eligibility is a
separate product-owned check at use.

Source and reviewed-account pages retain their server cursors and expected
workspace headers. Refused reads hide preparation. Role maximum validity is
checked before submission as well as by IAM. Ambiguous proposal responses freeze
the original command and recover its immutable receipt. Preparation and approval
never substitute for acknowledged activation or current authorization. Inactive
Staff require an active current directory record and a fresh independently
reviewed account basis; reactivation does not revive old duties. Hospital Staff
use the separate reviewed employment, account-link, department posting and
pharmacy-relationship workflows. See
[the preparation design](../superpowers/specs/2026-11-09-pharmacy-staff-preparation.md).

## Native operating-access governance source candidate

The separate canonical `iam.operating_access_reviewer.v1` bundle contains only
`iam.governance.operating.manage` and `iam.governance.operating.review`. Each
grant names one independent pharmacy or supplier employer. Tenant, hospital,
department, outlet and premise scopes are rejected. Existing account-link,
hospital assignment and operating-duty templates keep their permissions.

IAM's closed `WithGovernanceSource` mapping recognizes these two capabilities
and uses its current native-owner or exact-grant authority, credential watermark,
membership incarnation, validity and canonical employer topology. Revoked or
absent source denies; dependency failure is unavailable. This grants no stock,
clinical, financial or operating permission and creates no Workforce manager or
Staff row. Existing operating-duty routes retain their legacy administrative and employer
checks. Separate native routes below use the current operating governance source
while retaining all current Staff/link, location, membership and independent
decision checks. Source support alone does not establish complete Staff and
pharmacy acceptance.

The canonical Go/Zod template enum expands additively; clients must refresh it and
reject unknown keys. The source-only increment needed no schema migration; native consumption below
adds IAM0050. The
existing IAM governance ledger keeps immutable versioned keys and source facts;
rollback stops issuance/consumption of the new template and retains history.
Database source cases for both employer kinds, current owners/delegates, wrong
scope/purpose, source revocation and missing topology are acceptance gates.

## Native operating-duty administration candidate

Employer discovery uses `GET /v1/workforce/operating-authority-scopes` through
the Global API. Workforce owns the employer identity/name; IAM owns the current
nonclinical owner or exact `iam.operating_access_reviewer.v1` grant. Each returned
row contains only `employer_kind`, `employer_id`, `name`, `can_manage` and
`can_review`, generated from `staff-access.json.OperatingGovernanceEmployerScope`.
Only pharmacy/supplier employers with at least one currently authorised capability
are disclosed; no source grant, Staff, contact or product stock data is returned.

Exactly one expected-tenant and expected-subject header must match the refreshed
principal (`409 session_changed` otherwise). Missing coarse operating governance
permission returns `403`; absent root-specific authority filters that root out;
unavailable identity, source, database or audit returns `503`, without a partial
successful page. This read never grants access or creates a legacy manager.
Each disclosed capability is rechecked and audited inside the source callback.

The sealed list scans at most twenty candidates per page, descending by
`(created_at,id)`, with at most four simultaneous source checks and a five-second
overall source budget bounded by session expiry. An empty visible page can have
`has_more:true`; follow its opaque cursor. Cursors bind tenant, subject and route.
SDK requests cap pages at twenty, propagate cancellation, reject duplicates,
unknown fields, rows without authority and inconsistent continuation metadata.
Query keys include tenant/account; results have zero stale/retention time and no
automatic retries. Names are for selection, never reusable authority. Staff and
location preparation projections and native UI adoption remain separate gates.

All client traffic uses the Global API, with these IAM-owned destinations:

| Method | Path after `/v1/iam`                                                           | Authority                                                           |
| ------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------- |
| PUT    | `/operating-governance-employers/:kind/:employer/duties/:id`                   | native operating manage                                             |
| GET    | `/operating-governance-employers/:kind/:employer/duties/:id`                   | native operating manage                                             |
| GET    | `/operating-governance-employers/:kind/:employer/duties/:id/proposal-receipt`  | native operating manage, original maker/source                      |
| PUT    | `/operating-governance-employers/:kind/:employer/duties/:id/commands/:command` | native review for approve/reject; native manage for withdraw/revoke |
| GET    | `/operating-governance-employers/:kind/:employer/duties/:id/commands/:command` | native operating manage, original actor/source                      |
| GET    | `/operating-governance-employers/:kind/:employer/staff/:staff/duties`          | native operating manage, bounded staff page                         |

`kind` is exactly `pharmacy` or `supplier`; IDs are canonical v7 UUIDs. Existing
canonical OperatingDuty input, decision, view and receipt models are reused.
Non-list commands reject query filters. The verified tenant and path employer
must match every selected resource and body. Current IAM owner/grant authority
is held throughout the transaction. Workforce verifies the source in that
current guarded context and reads only the independent employer root; it creates
no legacy manager, Staff, login link or location. Target identity/current reviewed
link, membership incarnation, location evidence and independent maker/target
exclusions remain in the existing duty engine. The source provides no stock or
professional clinical authority by itself.

Additive IAM0050 adds a private immutable administration marker and forced-RLS
source/decision records. Canonical insertion/receipt triggers emit those records;
a deferred completeness constraint prevents partial attribution. It is checked
while the command purpose remains active, before a successful API response.
Actor, employer, capability, membership epoch, credential watermark, validity,
owner membership ID or exact grant ID/version are verified on native writes.
Receipt replay requires that same current authority identity, not a replacement
grant or recreated owner membership. Preparation and command source proof never
appears in public duty views. `404` hides absent/wrong scope, administration mode
or original source; missing coarse admission is `403`; stale inputs/transitions
remain `409`; current source/dependency/integrity failure is `503`.

Existing legacy duties retain their original API and receipt semantics. Native
commands cannot adopt legacy rows, and legacy human commands cannot change a
native row. The canonical projection worker still acknowledges valid approvals
or revokes an invalid target using its service-bound path. Native projection and
target operating-authority consumption verify the installed source support;
source expiry/revocation cuts off future administrative commands and replay,
while completed target duties retain their own current identity, location,
validity and explicit revocation checks.

The migration verifies the preceding assignment integrity before adding purpose
policies, then refreshes that checked baseline. Native support pins policy,
function and trigger definitions, enabled state, forced tenant RLS and protected
TRUNCATE privileges. Purpose-scoped membership reads select only actor/target;
unrelated IAM writes stay denied. A selected duty/version index bounds source
receipt validation. Failed attribution, audit or outbox work rolls back with the
canonical mutation. No new infrastructure is introduced. Native UI/SDK adoption,
remote acceptance of the new API/migration and the full both-model live Staff and
pharmacy journeys remain required. This candidate has not been deployed.

### SDK administration context

`OperatingDutyStaffScope.administration` is an explicit client context: absent or
`"legacy"` retains the existing duty routes; `"governance"` selects the employer-bound
native routes for preparation, reads, decision commands and GET-only recovery.
Unknown values, empty strings and explicit `null` fail before a request. The mode
is part of the query key and is never added to canonical command bodies. A denied
native command or receipt does not retry through legacy authority. The client
continues to bind responses to verified tenant/actor expectations, exact employer,
Staff, request ID and retained intent. Mode selection is routing context, not
server authority. Qualified discovery and native UI adoption remain outstanding.
