# Independent pharmacy supplying hospitals: partner access and hospital groups

Date: 2026-11-06. **Proposed design extension**, requested during review of the [staff/RBAC design](README.md). No new API, group model, trading relationship or procurement workflow is activated by this document. Existing accepted ADRs and canonical contracts retain precedence.

## 1. Concrete example and tree

An independent pharmacy business P supplies medicines to hospitals H1 and H2. Its pharmacist may own the business, but professional registration, employment, outlet operating authority and permission to supply the relevant products are distinct checks. A personal pharmacist account is not automatically a licensed distributor. Verify the real legal entity, premises, activity/category, country/state, current evidence and bilateral terms before enabling that activity.

```text
Independent pharmacy / supplier customer P
├── Employer and staff register owned by P
├── Approved supplying pharmacy outlet / supply premise P-B1
├── P's own staff assignments
│   ├── Commercial / order operator
│   ├── Warehouse / dispatch operator
│   ├── Responsible pharmacist / quality reviewer where applicable
│   └── Finance operator
├── Relationship R-P-H1: P-B1 ↔ Hospital H1 / receiving outlet H1-O1
│   ├── Grant to H1 Procurement group: view agreed catalogue + submit own order
│   ├── Grant to H1 Receiving group: view own shipment + submit receipt evidence
│   ├── Grant to H1 Finance group: view own invoice / dispute evidence
│   └── Optional named H1 user: limited timed grant for this relationship
└── Relationship R-P-H2: P-B1 ↔ Hospital H2 / receiving outlet H2-O1
    └── Separate H2 groups and grants; H1 sees none of H2's records

Hospital H1 owns its groups and each person's membership:
├── Procurement: buyer/requester and separate order approver
├── Receiving: receiving clerk and separate quality/acceptance reviewer
└── Finance: invoice reviewer and authorized payment approver
```

**Share an action set on a relationship or record with an approved recipient; do not share a login, an employee record or the supplier's internal administrator role.** The supplier's permissions remain separate from permissions given to hospital participants. The same pattern works for one approved hospital user, several named users, or an approved hospital-managed group.

This is a trading relationship. It differs from an independent pharmacy operating a hospital counter and from dispensing against a named patient's prescription. Bulk procurement normally requires no patient identity. A patient-specific fulfillment service needs its own clinical order/consent/authorized-disclosure contract and cannot reuse procurement viewer access to open charts.

## 2. Ownership and product boundaries

| Fact / action                                      | Owner and scope                                                                                                                  | What the partner can receive                                                                                                       |
| -------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| Hospital group and reviewed membership             | IAM in the hospital's tenant; hospital access administrator controls membership                                                  | Minimal eligibility decision for an actor in the exact published group; no private HR profile or unrestricted membership directory |
| Supplier internal staff/stock/finance authority    | Supplier IAM and the product owning each object/premise                                                                          | Nothing by default; partner grants never make hospital users supplier administrators                                               |
| Relationship and terms                             | Canonical bilateral relationship owner under the existing Supply/Pharma trading boundary; both parties approve the same revision | Participant IDs, exact outlets/premises, services/categories, validity and approved disclosure fields                              |
| Hospital request / purchase order / local approval | Hospital's designated procurement owner, linked to the real receiving facility/outlet                                            | Supplier receives the authorized order/approved revision and necessary business contacts                                           |
| Supplier acknowledgement / dispatch                | Supplier's current product owner; SupplyConnect for distributor operations, PharmaConnect for its own outlet-ledger acts         | Only the agreed order lines, accepted changes, shipment, batches/expiry, documents and safe status                                 |
| Destination receipt and hospital stock             | Destination product owner; PharmaConnect owns a pharmacy receipt                                                                 | Supplier receives receiving/discrepancy advice; it cannot post hospital stock                                                      |
| Supplier invoice, hospital payable and settlement  | Each existing financial owner retains its own ledger; exact owning modules require procurement/revenue review                    | Authorized invoice fields, dispute and verified settlement references for the parties to that transaction                          |
| Shared view                                        | Authorized, versioned participant projection of an owner's fact                                                                  | Explicit fields from the owner; never a separately editable master or an unrestricted cross-tenant query                           |

Reuse [SupplyConnect](../../api/domains/supply.md), [PharmaConnect](../../api/domains/pharmacy.md), [ADR-0026](../../adr/0026-pharmaconnect-country-and-trading-boundaries.md) and [ADR-0031](../../adr/0031-live-platform-identity-for-bilateral-trading.md). A pharmacy does not need a second stock master in SupplyConnect just because it supplies a hospital. Which product owns its source dispatch depends on its actual business model; link canonical organization/product identities and preserve one ledger owner for each physical act.

The repository's supply specification still marks full quote → PO → dispatch → receipt → settlement composition as work to specify. Do not invent a deployed hospital procurement API or a generic collaboration microservice. Determine the relationship/command owner and exact contracts during the existing business-integration review. This document specifies access requirements for that owner.

## 3. How a role is assigned to a hospital user or group

There are two approvals: **the hospital chooses eligible people; the data owner chooses what those people may see or do**. Both parties approve the relationship and disclosure terms. Group management does not confer the power to create external grants, and granting a group access does not confer the power to change its membership.

1. Hospital IAM creates a dedicated externally publishable group, such as `H1 / P procurement team`. A general “all hospital staff” group is ineligible by default.
2. Hospital access authority approves each direct member's purpose, local role, hospital/unit scope, start/end dates and reviewed staff link. The group cannot contain supplier users, nested groups or wildcard members in the initial slice.
3. Both parties approve R-P-H1, exact legal/premise identities, permitted activities/categories, disclosure fields, validity and group-control policy. Identity verification alone does not approve trading.
4. The supplier's authorized disclosure/grant reviewer selects that **verified H1 group**, a supported partner role, relationship/outlet/record selectors and an expiry. H1 accepts the grant. Initial state remains pending until both current decisions and required projections agree.
5. Each user signs in with their own identity in their H1 context. Global API routes the authorized collaboration request to its owning product. The hospital role and current group membership are checked together with the owner's current disclosure grant; the browser cannot choose a tenant header to impersonate P.
6. The UI presents P's approved catalogue, H1's own orders, H1-bound shipments and role-appropriate documents. It labels the source owner and observation freshness. H2 customer information, P's supplier costs, other negotiated rates, internal stock totals, private staff details and patient records are excluded unless separately and explicitly authorized.
7. Removal from the H1 group, loss of local eligibility, expiry of the partner grant or relationship suspension blocks subsequent authorization checks. No graph tuple or cached role badge can override current owner state.

For **one named user**, the hospital nominates and approves that subject, the supplier grants the same bounded partner template, and the same evidence/revocation rules apply. For an ad hoc list of users, use explicit subject grants or a dedicated reviewed group; do not create an unowned “shared account.”

### Group membership growth is itself an access change

The relationship explicitly states which hospital administrators may manage the published group and the permitted employment categories/local roles/units. Within those agreed limits, a newly reviewed eligible member receives the existing group grant without the supplier individually approving every staff rotation; this is an explicit delegated membership decision accepted by both parties. The membership change is independently authorized and audited. An administrator cannot self-enroll to gain partner access.

An out-of-policy member, a new hospital, broader fields/actions, a nested group, or a changed group administrator ceiling requires a revised approval. Higher-risk approval/clinical/financial templates can require named-person approval in addition to membership; joining the viewer group never grants those actions. Member additions can be capped or require dual review by the agreed policy, without inventing universal numeric limits here.

Supplier reviewers see the minimum recipient assurance needed to approve the grant, not the hospital's whole staff directory. The hospital can revoke any member immediately; the supplier can suspend its grant immediately; either party can suspend the relationship. Neither party needs the other's approval to reduce access.

## 4. Role matrix for the shared work

Names are **proposed partner template examples**, not existing permission strings. They remain shared IAM assignments with an explicit relationship and participant scope, not a new private grant vocabulary. Use only templates backed by accepted owner contracts.

| Partner template                 | Eligible holder                                      | Permitted scope / actions                                                                | Explicit exclusions                                                                      | Assignment approval                                                                   |
| -------------------------------- | ---------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| `partner.catalogue_viewer.v1`    | Named hospital user or H1 Procurement group          | Agreed items, pack/units, H1-specific prices and availability offer                      | Raw warehouse stock, procurement costs, other customers' terms                           | Hospital nomination + supplier disclosure reviewer                                    |
| `partner.order_requester.v1`     | Named user or reviewed H1 Procurement group          | Create/edit H1-owned drafts and submit requests in approved categories                   | Approve own PO; accept supplier substitutions as clinical authority                      | Hospital workflow approver + bilateral role/disclosure approval                       |
| `partner.order_approver.v1`      | Eligible named approver or restricted approver group | Approve H1 order within assigned authority and reviewed financial limits                 | Own-request approval, supplier order acceptance, payment release                         | Hospital independent access/financial authority; supplier gets approved business fact |
| `partner.shipment_viewer.v1`     | H1 Receiving group                                   | Only H1-bound shipment state, expected lines/batches and relevant transport evidence     | Shipments to H2, delivery-driver personnel details beyond need                           | H1 acceptance + supplier disclosure reviewer                                          |
| `partner.receiving_submitter.v1` | H1 Receiving group                                   | Record H1 delivery count, discrepancy and receipt evidence through the destination owner | Edit supplier dispatch; declare own submitted receipt quality-released without authority | Hospital receiving authority; supplier receives authorized advice                     |
| `partner.quality_reviewer.v1`    | Eligible named hospital pharmacist/quality reviewer  | Review H1 received lots and safe-release decision under product policy                   | Waive required evidence, release recalled/unsafe stock, prescribe                        | Hospital professional/governance + independent access review                          |
| `partner.invoice_viewer.v1`      | H1 Finance group                                     | H1 invoice and related PO/receipt/credit/dispute evidence                                | Supplier bank-master editing; other customers' balances; automatic payment authority     | Hospital finance authority + supplier disclosure reviewer                             |
| `partner.relationship_admin.v1`  | Named bilateral relationship administrators          | Propose recipient groups and bounded grants, review terms and suspension                 | Granting beyond allowlist, self-approval, operational stock/clinical powers              | Independent authorized reviewer on each relevant side                                 |

Hospital users' writes affect their own party's contribution through its owner: order drafting, order approval, receipt or dispute. They do not edit supplier source records. Supplier users separately acknowledge, allocate, dispatch and invoice under P's own roles. A hospital receiving clerk cannot become a supplier warehouse operator by receiving a shipment-viewer grant.

Continue the main document's readable aliases where useful:

```text
hospital_pharma_rbac_partner_shipment_viewer_r
  recipient = verified H1 Receiving group
  template = partner.shipment_viewer.v1                 [proposed]
  scope = R-P-H1 / supplier premise P-B1 / destination H1-O1
  filter = only shipments whose actual participants match that scope

pharma_rbac_dispatcher_w
  recipient = supplier P's own reviewed employee
  scope = P / supplying premise or outlet P-B1
  action = supplier's own authorized dispatch command
```

The prefix never acts as security authority. A group role is a reusable permission bundle assigned to a group; users remain individually attributable. A person holding both requester and approver roles still cannot approve their own order because the command enforces separation of duties against the actual requester identity.

## 5. Operational scenarios to support

These are realistic workflow scenarios derived from the sources and existing contracts, not observations of a live customer deployment.

| Scenario                                                | How it should work                                                                                                                                                                                           | Who sees / acts                                                                                                                      |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ |
| Regular replenishment to H1 and H2                      | Separate relationships, offers/orders and destinations. P may fulfill both using its own eligible stock                                                                                                      | H1 groups see H1 only; H2 groups see H2 only; P's own operators see only their assigned operational scope                            |
| Hospital branch/group procurement                       | Central procurement group may participate only for hospitals and destinations explicitly approved on the relationship                                                                                        | Shared parent ownership never adds an unlisted hospital; each receipt stays with its real destination owner                          |
| Daily team rotation                                     | Hospital admin adds an eligible shift member under the approved group policy; removing a leaver cuts subsequent access                                                                                       | No supplier-wide role copying or duplicate employment record                                                                         |
| One external pharmacist supplies and owns the business  | Keep business, dispensing and quality roles distinct; verify activity/premise authority                                                                                                                      | Ownership is not a licence; workflows requiring a second person remain pending until an authorized independent reviewer is available |
| Partial shipment / short delivery                       | Order for 100 packs, dispatch 60, receive 58: retain each quantity and source-line identity, record a shortage of 2 on that shipment, and leave the unfulfilled 40 under agreed backorder/cancellation terms | Hospital records its count; supplier cannot mark 100 received; units/pack conversions are exact                                      |
| Wrong batch, damaged packs or temperature excursion     | Record receipt evidence and quarantine/discrepancy. Authorized reviewer determines release/return under current policy                                                                                       | Driver delivery completion and receiving-clerk count cannot auto-release usable stock                                                |
| Brand/strength/pack substitution                        | Supplier proposes a new explicit order revision; authorized hospital commercial review and any required clinical review apply                                                                                | A procurement group does not gain prescribing authority; preserve the original line and prior partial acts                           |
| Consignment stock at a hospital                         | Track physical custody separately from title, use/ownership-transfer evidence and invoice trigger                                                                                                            | Hospital custodian records permitted facts; supplier cannot edit hospital care records; no automatic double-owned stock              |
| Patient-specific emergency delivery                     | Separate approved clinical fulfillment contract and urgent authorized actors; minimum required patient/order data                                                                                            | Bulk procurement viewer group sees no patient identifiers; urgency does not bypass qualification or stock safety                     |
| Recall after delivery                                   | Batch-linked traceability identifies only affected recipients. Send accountable safety notice and track acknowledgement/quarantine/return evidence                                                           | Emergency safety/obligation contact path is separate from suspended commercial access; no continued general portal access            |
| Courier handoff / proof of delivery                     | Courier gets a time-limited task view containing only destination/contact and required evidence                                                                                                              | No order pricing, finance, patient chart or ability to confirm hospital quality acceptance                                           |
| Invoice dispute / credit / payment                      | Match the relevant PO, dispatch, receipt and invoice; record discrepancy/credit and independent payment approval                                                                                             | GRN is not settlement; viewing an invoice does not authorize payment or changing bank details                                        |
| Contract expires with undelivered stock or open invoice | Block new trade and general partner grants; retain narrowly authorized historical obligation, return, dispute and safety paths                                                                               | No deletion of evidence and no perpetual broad access justified by an unpaid invoice                                                 |
| Partner or group authority unavailable                  | Protected cross-party read/command returns unavailable or visibly pending as appropriate; no stale allowed fallback                                                                                          | A stale dashboard is labeled and cannot drive release, acceptance or payment                                                         |

For consignment, cross-border, controlled-drug categories, electronic signatures, retention and professional supervision, the exact operating model and primary-source legal applicability remain review gates. Do not activate a generic workflow from these examples.

## 6. Shared trade flow and attributable records

```text
Verify both parties + premises + activity evidence + agreed relationship revision
  → publish approved catalogue/offer to eligible H1 recipient(s)
  → H1 requester creates order → independent H1 approval
  → P accepts / proposes changes → H1 accepts authorized revision
  → P reserves eligible source stock → dispatch + batch/quantity evidence
  → courier handoff / in transit (not hospital receipt)
  → H1 counts physical receipt → discrepancy / quarantine / quality review
  → destination owner posts approved received/accepted stock
  → reconcile supplier invoice ↔ PO ↔ receiving evidence
  → independently approved payable / settlement / credit
```

Each arrow is a governed transition, not a claim that the chain currently exists end to end. A shared correlation/relationship/order-line reference connects immutable owner records; it does not replace their IDs or create a cross-module transaction. Source dispatch and destination receipt have separate owners, durable idempotency, replay handling, exact units and reconciliation. Late/duplicate events cannot duplicate stock, replace a newer acknowledgement or undo a recall.

### Live visibility without fabricated success

The shared timeline displays the source owner, source revision, last observed time and actual order/dispatch/receipt/quality/financial states. A courier scan is not hospital acceptance; an invoice upload is not payment. Machine-authenticated events update only authorized recipient projections, preserve ordering/version checks and carry minimized references. Subscription delivery, reconnect/backfill and every fetch recheck recipient access; event arrival itself is never authority. Lag or an unavailable partner is shown as stale/pending/unavailable rather than a fabricated stock figure, delivered status or ETA. State polling and streams use bounded pages, scoped cursors and replay-safe reconciliation.

## 7. Required group and grant facts

Extend shared canonical inputs only after review; no parallel copy of the wire model is defined here.

| Concept                  | Required identity, provenance and constraints                                                                                                                                                 |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Published hospital group | Immutable group ID + owner tenant + hospital/unit boundary + purpose + membership-policy version + status/validity + approved group-manager ceiling                                           |
| Group membership         | Verified subject + current local eligibility + grantor/reviewer + validity + source staff/link generation + revocation/version; initial slice has direct members only                         |
| Recipient                | Typed `subject` or `group`, with owning tenant/hospital and verified binding; group name or email string alone is insufficient                                                                |
| Partner grant            | Owner tenant/product + exact relationship revision + recipient + template version + scope/record selector + field policy + validity + both decisions + status/version/epochs                  |
| Shared object view       | Source owner and immutable source/version references, participant IDs, purpose/data classification, freshness, authorized fields and record-specific restrictions                             |
| Action evidence          | Individual actor, home tenant, effective group/membership/grant versions, relationship version, action, target/source references, reason, result and correlation; no shared-login attribution |

Current supply grants store a **subject**, fixed role and optional premise inside one tenant (`supply_access_grants` / `supply_party_access`). They are not a bilateral hospital-group grant model. The current OpenFGA model does not establish this published partner-group flow. Do not encode `group:…#member` into an existing subject text field and assume graph or RLS support. Introduce a typed recipient through the shared assignment model and an explicit reviewed canonical migration, without changing existing v1 semantics.

## 8. Enforcement and revocation across organizations

Partner access requires the intersection of **current hospital identity/local permission + eligible direct membership or named-subject nomination + current recipient grant + active reviewed relationship + actual object participation + field/action policy**. The same qualifying grant must provide both action and scope. Department inheritance cannot cross a relationship into another employer's objects.

Keep the home tenant and external resource owner separate in server context. Partner views come through an explicitly contracted owner-authorized disclosure path. Do not switch the browser into the supplier tenant, query cross-tenant tables, copy the whole supplier catalogue privately, or accept caller-supplied group/tenant assertions. Product-machine requests use an audience/purpose/actor/object-bound authority contract; a service credential is transport identity, not permission to impersonate any partner user.

Current source authority must be checked at the authorization boundary. Resolve required live identity/group evidence before opening the product's local transaction; recheck local object/grant/relationship versions under the documented lock order before disclosure/commit. Do not hold database locks across remote calls. A stale event-driven projection alone cannot authorize a sensitive partner action. If current membership or relationship authority cannot be established, return 503; explicit ineligibility denies.

For hospital-owned writes, the hospital owner checks its membership/approval authority in its own guarded transaction and publishes the resulting authorized business fact; the supplier consumes that fact, not the user's browser credentials. For supplier-owned stock acts, P's own actor/owner performs its own guarded command. There is no atomic transaction across both organizations and no promise that a later revocation retracts an already admitted disclosure or committed physical act. The eventual contract must explicitly define the read admission point, in-flight behavior and revalidation before consequential delayed release; subsequent authorization checks after revocation must deny, with no positive-cache grace period.

Unsharing does not delete records, undo medicines already delivered, erase an audit or recall a downloaded file. Export/download has a separate explicit permission, restricted fields, current disclosure checks, safe file release and audit. Notifications contain safe references and require authorization on click. Revocation closes active subscriptions and prevents subsequent pages/downloads; do not leak H2 IDs, totals or metadata through search/counts/notifications.

## 9. User experience and acceptance gates

The supplier uses **Partners → Hospital H1 → Shared access**. The reviewer chooses _named person_ or _hospital-managed group_, then exact supported actions, locations, fields and expiry. The hospital uses **Supplier P → Our team access** to nominate/accept recipients and maintain reviewed membership. Both sides see Pending / Active / Suspended / Expired / Revoked, approvers and impact history. Neither side sees the other's private access-administration directory.

The hospital user's workspace shows **Hospital H1 · Supplier P · Receiving**, with only H1's eligible records. A capability preview explains “View H1 shipments” or “Submit H1 receiving evidence”; it never shows “Pharmacy admin” as a shortcut. Changes preview current recipients and affected records; reductions take effect without waiting for partner approval. Group-manager or field/action widening goes back through review.

Required synthetic acceptance cases before exposing a group consumer:

- User directly nominated versus user in the approved H1 group; nonmember and suspended/leaving member denied. No shared login or HR ownership transfer.
- H1 group tries H2 order/shipment, unrelated supplier outlet, supplier stock write, private HR file, patient chart or another customer's negotiated rate: denied without existence/count leakage.
- Hospital group admin tries self-enrollment, adds an out-of-policy member, nests a supplier/cross-hospital group, or widens the published grant: denied/pending new review.
- User has viewer membership plus an unrelated admin assignment: no mixing action/scope and no business/financial/clinical escalation.
- Same person requests and approves an order/receipt/financial exception: blocked even through two different role/group paths.
- Membership removal races with read admission, transfer, export, websocket event and delayed command: exercise the documented boundary; later checks deny and stale events cannot reactivate access.
- Supplier suspends grant or either party suspends relationship while graph/broker is stale: no new admission using stale allow; unresolved authority returns unavailable.
- Shared link, attachment, search, count and notification are reauthorized and restricted to H1 fields; expired download/reference cannot bypass scope.
- Partial delivery, duplicate receiving event, changed-content retry, wrong pack units, cold-chain discrepancy, recall after allocation and invoice mismatch preserve separate physical/financial evidence.
- Relationship ends with open orders/invoices/recalls: no broad commercial access, but separately authorized obligation/safety process remains possible with history.

New review decisions: published-group membership policy and initial no-nesting restriction; typed shared-IAM group recipient; precise partner disclosure/relationship owner; supported actor/action/field catalogue; read-admission/revocation semantics; safety/history obligations after suspension. These require product, hospital/pharmacy operations, IAM/security, procurement/finance and applicable privacy/legal review. They do not settle existing open ORG decisions or activate policies.

Traceability: ORG-AU/ORG-AC and ADR-0052/0055 for attributable scoped authority; IN-PH-02/03/04/05 for clinical/stock separation; IN-PH-08/09/10 for linked procurement evidence, owner-specific posting and current trading authority. See the [research supplement](research.md#partner-group-and-supply-scenario-research) for primary-source support and limits.
