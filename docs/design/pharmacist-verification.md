# #169 Pharmacist verification — scope and implementation review

Reviewed 26 November 2026 against `tesserix/helivanta` main at `fde40d3779f8c8f98ff0a5f8ca9d77f39f692b4e`.
Issue: https://github.com/tesserix/helivanta/issues/169
Milestone: R1 — Hospital pharmacy pilot (#12). Issue remains OPEN.

27 November update: ADR-0077 is accepted in main (`69dedfdb`). The
[administrative policy-preparation candidate](../api/pharmacy-verification-policies.md)
is developed in draft PR #1902; its database and deployment acceptance are separate
gates. It creates no effective policy or clinical authority.

This is a development scope and delivery record, not a deployed acceptance report, legal approval or clinical policy. Live policies, grants and feature flags remain unchanged. The review was read-only; the first contract slice below has separate validation evidence.

## Conclusion

The requested clinical verification workflow is not implemented end to end. Staff credentials, manual prescription capture, prescription-linked counter sales and stock/regulatory controls are foundations, not proof that an independently attributable registered pharmacist reviewed the exact state supplied.

The issue's recommended solution predates the current counter-sale implementation. Reconcile it with the active sale path before writing migrations or adding routes. Do not re-enable the retired free-text dispense route.

## Business outcome and actors

Every prescription-based supply must carry durable evidence identifying the authorized registered pharmacist, the exact source and fulfillment state reviewed, the decision and any required independent second check. A query or rejection must block supply and reach the authorized prescriber workflow. Material changes must invalidate approval before another physical or financial effect.

- Hospital/pharmacy owner: administer setup and appoint eligible staff through reviewed access workflows. Ownership does not confer clinical review authority.
- Pharmacy technician/counter operator: prepare a request and see blockers; preparation is not clinical approval or permission to supply.
- Registered pharmacist: review current authorized clinical context and make an attributable decision for assigned outlets.
- Independent second pharmacist: countersign the same current state where an approved policy requires it; cannot satisfy both signatures as the same person through separate accounts.
- Pharmacist-in-charge: accountable oversight, queue/escalation and only explicitly authorized exception handling.
- Prescriber: receive queries/rejections and amend the source prescription through its clinical owner.
- Patient/authorized proxy: see appropriately disclosed status and next steps in MediConnect.
- Auditor/compliance reviewer: authorized, audited history without unrestricted patient or staff disclosure.

## Complete functional scope and current gap

| Requirement                | Current source evidence                                                                                                                    | Work still required                                                                                                                                                                                                                                           |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Professional registration  | Practitioner attestation source exists; Workforce pharmacy credential review/reminders exist                                               | Runtime owner and sixteen source migrations are deployed; source HTTP routes stay unreleased. Integrate authoritative registration trust, expiry/revocation and current clinical eligibility. HR approval must not be interpreted as professional permission. |
| Scoped verification access | Pharmacy routes have role permissions and outlet scope; Staff operating-grant foundations exist                                            | Specific verification/second-check permissions and current outlet assignment plus registration/privilege checks at decision and supply time; deny unavailable authority.                                                                                      |
| Prioritized queue          | Legacy waiting-dispense list exists but is not a verification queue                                                                        | Durable queued review items, bounded pagination, outlet/urgency/flag filters, SLA age and escalation; measurable queue latency.                                                                                                                               |
| Review context             | Manually recorded prescriptions and exact product/quantity/repeat citations exist                                                          | Versioned signed source linkage, authorized allergies/medications/age/weight and substitution/regulatory flags with provenance, freshness and explicit unavailable states.                                                                                    |
| Four decisions             | No clinical verification aggregate or registered decision API found                                                                        | approved, approved_with_change, query_raised and rejected with legal transitions, reasons, durable command replay and retained history.                                                                                                                       |
| Content-bound approval     | Sale lines cite immutable recorded prescription lines; no reviewed-state approval/hash gate found                                          | Canonical review-state/version binding and invalidation on relevant changes; atomic validation at the active supply transaction.                                                                                                                              |
| Second check               | No second-check workflow found                                                                                                             | Independent eligible signer, matching state and policy version, waiting status, escalation and concurrency protection.                                                                                                                                        |
| Configurable policy        | Existing schedule/licence controls are distinct; draft PR #1902 adds nonclinical append-only policy preparation, still compiled unreleased | Reviewed effective/versioned verification policy, required categories, permitted second-check conditions and governed exception rules. No arbitrary customer relaxation of mandatory controls.                                                                |
| Query/rejection handoff    | No PrescriptionQueryWorkflow or proposed verification events found in searched source                                                      | Owner contracts, durable delivery/acknowledgement, prescriber inbox, response/amendment correlation, escalation, timeout semantics and patient status.                                                                                                        |
| Immutable evidence         | Existing audit/append-only patterns can be reused                                                                                          | First-class per-attempt evidence including source/policy versions, identity/registration snapshot, state digest, reasons, timestamps and countersignature; corrections retain history.                                                                        |
| Break-glass                | No pharmacist-verification emergency path found                                                                                            | Qualified policy decision first; if authorized, narrowly scoped exception, reason, actor eligibility, retrospective task, deadline and breach escalation. Never an automatic owner bypass.                                                                    |
| Supply enforcement         | Free-text dispense route returns 410; active PUT sales path performs stock/prescription checks                                             | Enforce review on actual prescription supply/stock/financial commit paths, including later partial fills; direct API, worker and concurrent requests cannot bypass it.                                                                                        |
| UX and release             | Current UI records prescriptions and sells cited lines                                                                                     | Accessible queue/review/history/prescriber/status journeys; UI/API/dashboard feature coverage; deployment and browser acceptance with distinct staff accounts.                                                                                                |

## Current-source anchors

Paths below refer to the reviewed main revision, not the older user's root checkout.

- `backend/internal/modules/pharmacy/dispenses.go`: retired fulfill handler returns 410, with instruction to use a prescription-linked counter sale (#1680).
- `backend/internal/modules/pharmacy/sale.go:399`: active sale route registration; `:978` putSale; `:825` prescription validation. Review enforcement must be integrated with the actual transaction and its stock/financial consequences.
- `backend/internal/modules/pharmacy/prescription_sale.sql:102`: sale-line source/stock/licence/schedule and prescribed quantity/repeat guards. These are not a verification-decision guard.
- `backend/internal/modules/pharmacy/module.go:229`: sale permission; `:311` pharmacist role mapping. A role mapping does not itself establish valid professional registration.
- `backend/internal/modules/practitioner/registration_verification.go:320`: current registration attestation projection; clinical authority is explicitly separate.
- `backend/internal/bootstrap/modules.go`: The deployed source-runtime integration registers the practitioner owner alongside Pharmacy; all professional-source routes remain compiled unreleased. Source presence or closed #474 is not proof of runtime availability.
- `docs/superpowers/plans/2026-11-12-pharmacy-credential-reviews.md:17`: credential approval grants no PharmaConnect role or authority.
- `apps/shell/components/pharmacy/prescriptions.tsx`: manual prescription capture and private pages, not clinical verification.
- `apps/shell/components/pharmacy/counter-sale.tsx`: prescription-line selection and counter sale; no reviewed clinical verification journey established.
- `docs/api/http-contract.md`, `global-connector.md`, `integration-contracts.md`, `schemas.md`: actual v1 transport, Global API routing, replay/event rules and canonical schema ownership.

An exact-symbol search across backend, packages, apps, contracts and e2e found no `pharma_verification`, `reviewed_state_hash`, `PrescriptionQueryWorkflow`, `REGISTRATION_INVALID` or `can_second_check`. This supports the gap assessment; it is not a proof that every semantically equivalent implementation is absent.

## End-to-end journey to implement and accept

1. Configure a pharmacy/outlet in a verified tenant, including hospital-owned or independently owned context, applicable policy and eligible staff. Verify professional identity separately from login and HR membership.
2. Receive an authorized prescription/version from the clinical owner, or follow an explicitly agreed policy for externally issued/manual prescriptions. Persist provenance and create review work exactly once. Never infer prescribing intent from visit creation.
3. Counter staff prepare fulfillment lines and permitted substitutions under the respective owners' rules. The queue clearly identifies patient, outlet, age, urgency and blockers.
4. Pharmacist opens context. Display current source/version, available clinical data, unknown/missing information, proposed product/quantity/substitution and rule flags. Do not invent negative allergy findings or clinical advice.
5. Pharmacist decides. Plain approval binds the exact reviewed state; changed approval must follow the agreed prescriber-amendment/authorized-substitution path. Query/rejection parks or ends the request without stock or billing effects.
6. Where required, a different eligible pharmacist reviews and countersigns that same version. A subsequent relevant change invalidates both signatures and returns the work to review.
7. Supply command rechecks current authority, state/policy and matching approval atomically with stock and financial posting. Resolve a timeout through the same retained command identity; do not supply twice.
8. Record the actual supplied lines, lots, quantities and partial balance through fulfillment. Review validity for later partial supply must be explicit, not assumed perpetual.
9. Publish minimal durable events. Prove the prescriber inbox and patient status consume them correctly; broker publication alone is not completion. Patient-facing medication truth follows actual supply.
10. Retain history, audit authorized reads and decisions, expose pending/unavailable delivery and escalate unresolved queries or retrospective exceptions.

## Ownership and tenant boundaries

| Concern                                  | Owner and boundary                                                                                                                                |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| Signed prescription and amendments       | MediCore/clinical owner; DoctorConnect is the prescriber's experience. Pharmacy must not modify the clinical owner's tables.                      |
| Professional identity/registration trust | Practitioner owner, with council/source/evidence provenance. Current registration attestation is distinct from scoped clinical privilege.         |
| Employment and staff facts               | Workforce; no duplicate pharmacist master in Pharmacy.                                                                                            |
| Identity and operating permissions       | IAM/organization access; current tenant membership, eligible Staff/account link and narrower outlet/assignment scope are mandatory.               |
| Verification evidence and supply         | PharmaConnect, referencing owning clinical/professional identities and versions. Final ownership/contract must be ratified before implementation. |
| Clinical rule results                    | Relevant clinical/regulatory owners; #169 displays/consumes results and does not author a dose/allergy/interaction engine.                        |
| Patient-facing disclosure                | MediConnect, through authorized owner contracts and privacy controls.                                                                             |

Hospital-owned model: verified tenant → hospital → pharmacy department relationship → pharmacy/outlet → eligible assigned pharmacist. Independent model: verified pharmacy tenant → pharmacy → outlet → its own eligible pharmacist. Neither hierarchy alone grants clinical access. An independent pharmacy serving another hospital needs explicit current cross-tenant clinical-sharing authority and purpose-specific contracts; commercial supplier grants or a hospital affiliation are insufficient. Never trust a supplied tenant ID or directly share patient tables.

## Decisions to settle before coding

1. **Active aggregate and enforcement seam:** map #169 onto current sales and #170's future fulfillment aggregate. Decide what constitutes physical supply and ensure approval precedes stock/financial commitment. Do not create parallel dispense masters.
2. **Prescription sources:** signed internal versus externally issued/manual capture; define verified identity, amendment/withdrawal, source availability and provenance contracts for each.
3. **Professional eligibility:** registration owner/runtime, council-specific date/freshness rules, revocation, independence and current outlet privilege. State-council facts need accountable review across India; no Maharashtra-only assumptions.
4. **Expiry enforcement:** a nightly tuple update cannot mean immediate expiry. Re-evaluate authoritative validity at decision and supply time; cache TTL cannot exceed validity, and missed invalidation must fail safely.
5. **Reviewed state:** include patient/source identity and revision, product/strength/form/route, directions, quantities/repeats, permitted substitutions, policy/flag versions and clinical context versions that matter. Define precisely which nonclinical edits are excluded. Canonical serialization/versioning is required; hash equality alone is not authorization.
6. **approved_with_change:** pharmacist sign-off must not silently rewrite a prescriber's order. Route required amendments back to the prescriber and re-review the resulting version. Align with #168 and current law before enabling substitution.
7. **Second-check semantics:** scope, independence, deadline, reassignment and invalidation must be defined. Schedule X remains blocked today; #169 must not enable X/NDPS as an incidental side effect. Do not claim all X prescriptions legally require the proposed software countersignature without qualified review.
8. **Emergency semantics:** the issue simultaneously requires every supply to have prior approval and permits supply with no pharmacist on site. Resolve the applicability and lawful actor/path before implementing an exception. A retrospective task does not create legal authority.
9. **Query timeout:** decide who may cancel/expire a pharmacy fulfillment request versus the signed clinical prescription. Pharmacy must not unilaterally expire the clinical owner's order. Review stock reservation and patient-notification semantics with #170.
10. **Contract conventions:** issue sketches use /api/v1, universal Idempotency-Key and uppercase errors; actual backend uses /v1, owner-specific retained command IDs and existing error envelopes. Ratify canonical schemas, replay/conflict contracts and Global API routes instead of copying sketches verbatim.
11. **Policy activation and existing records:** define safe handling of old prescriptions/sales, pending requests and in-flight sessions. Historical completed supplies retain history; new enforcement must not break unreviewed consumers silently.
12. **Metrics:** treat queue p95 <5 minutes and break-glass <1% as proposed service/product targets to validate. Do not turn the suggested 2% query/rejection rate into a clinical quota or incentivize unnecessary rejection; use it only as contextual governance information after review.

## Dependencies and sequencing

| Linked work                                  | Observed state | Implication                                                                                                                             |
| -------------------------------------------- | -------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| #168 Generic substitution                    | OPEN           | Define permitted change evidence/invalidation contract; no automatic substitution assumption.                                           |
| #170 Prescription fulfillment                | OPEN           | Agree active aggregate, partial fills and commit gate together; current manual counter is only a foundation.                            |
| #171 Drug regulation                         | OPEN           | Consume reviewed verdicts; broader legal coverage and X/NDPS remain blocked.                                                            |
| #474 Practitioner master                     | CLOSED         | Confirm required runtime/API availability and current professional eligibility; closed state does not prove this consumer integration.  |
| #1657 Pharmacy Staff credentials             | MERGED PR      | Reuse HR facts; does not grant verification authority.                                                                                  |
| #1668 Role-free account initiation           | MERGED PR      | Login creation remains distinct from assignment and clinical eligibility.                                                               |
| #1672 Identity projection acceptance blocker | OPEN           | Reassess actual deployed impact for designated QA accounts; do not repeat its November 13 status as newly verified production evidence. |
| #1680 Retire free-text dispense              | CLOSED         | Keep retired route at 410; gate active supported supply commands.                                                                       |

Suggested bounded delivery order (each slice retains its own acceptance):

1. Ratify owner contracts, source/amendment model, eligibility, exception decisions and full acceptance matrix. Update stale issue sketches and dependent contracts.
2. Implement authoritative eligibility and immutable version-bound verification records, decisions/history and minimal review context. Add no operating grant from account creation or HR review.
3. Integrate the mandatory gate into the active supply transaction with replay, concurrency and invalidation tests. Backend denial must hold before any feature flag is enabled.
4. Deliver accessible queue and review UX, decision recovery and history; use actual names/roles with scoped registration details rather than UUID display names.
5. Deliver independent second check and durable prescriber query/rejection/response loop, including MediConnect status and delivery recovery.
6. Implement emergency exceptions only after qualified policy approval; otherwise keep them explicitly disabled and unresolved in #169.
7. Complete controlled deployment, migration/backfill plan, release flags and actual UI/API acceptance before claiming the issue complete.

## Behavior and acceptance test matrix

| Test                                           | Required observable result                                                                                                                                                                                              |
| ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Primary allergy-conflict rejection             | Current authorized context displayed, attributable rejection retained, supply blocked, prescriber inbox and authorized patient status updated. Clinical fixture is synthetic, not a generated treatment recommendation. |
| Normal approval                                | Eligible pharmacist approves exact current state; matching supply commits once with durable evidence and correct stock/financial consequence.                                                                           |
| Expired, revoked or not-yet-valid registration | Decision and supply denied at the authoritative boundary; no false verification, no stock/payment side effect; authorized escalation.                                                                                   |
| Owner/HR-only account                          | Cannot verify merely because it owns the tenant or reviewed Staff credentials.                                                                                                                                          |
| Wrong outlet, hospital or tenant               | No cross-scope context/decision disclosure or write; cross-tenant supplier grants alone provide no authority.                                                                                                           |
| Changed source or fulfillment                  | Product, quantity, directions, substitution or required policy/context changes invalidate old approval; current version required.                                                                                       |
| Concurrent approval/amendment/supply           | Serialized outcome cannot commit supply using stale state; losing decision returns actionable conflict.                                                                                                                 |
| Exact retry after commit timeout               | Same command returns original retained receipt; one decision, stock effect and event. Changed content under the same key conflicts.                                                                                     |
| Two reviewers racing                           | One final decision wins; UI identifies resolved state without exposing unauthorized Staff details.                                                                                                                      |
| Independent countersignature                   | Same person cannot sign twice; wrong or stale version cannot be countersigned; single available pharmacist sees awaiting-second-check and escalation.                                                                   |
| Prescriber query and response                  | No supply while queried; only authorized current amendment/response resumes review; duplicate/out-of-order events converge.                                                                                             |
| Query timeout/dependency failure               | No silent clinical-order expiry; approved owner semantics, escalations and explicit unavailable status.                                                                                                                 |
| Partial fulfillment and subsequent visit       | No overfill or duplicate supply; policy for continuing approval is enforced against current eligibility/source/remaining lines.                                                                                         |
| Event failure after commit                     | Outbox retains delivery; retries/acknowledgements/recovery verified at real consumers, not mocked publication alone.                                                                                                    |
| Emergency path, if approved                    | Only eligible exception actor and allowed conditions; reason, attributable exception, retrospective deadline and breach escalation persist; prohibited categories remain denied.                                        |
| Context/privacy failures                       | Unknown allergy/history stays unknown; unavailable clinical source is visible; patient/Staff data and notes absent from general logs/events.                                                                            |
| Accessible UX                                  | Keyboard/focus/labels, loading/empty/denied/stale/conflict/unavailable states, repeated-click protection, timeout recovery, responsive layout and tenant switch invalidation.                                           |
| Immutable history                              | Review attempts and registration/policy/source snapshots survive later changes/revocation; authorized read/export audit and retention controls.                                                                         |
| Release/rollback                               | Flag covers route/navigation/dashboard/API; disabled state cannot expose a bypass. Required safety gate cannot be disabled to re-enable unsafe supply.                                                                  |

Test levels: deterministic unit tests for state transitions/canonicalization/expiry policy; real database integration tests for forced RLS, triggers, locking, append-only history and transaction effects; producer-consumer contract/replay tests; a small set of browser journeys with distinct synthetic users through Global API. Use focused local validation/build and CI; no local apps, Colima or heavyweight full build is needed for this review.

## Closure evidence required

All in-scope functionality and decisions resolved (or explicitly accepted as a scope change), current contracts and generated models aligned, meaningful unit/integration/E2E checks passed, required focused local build and lint evidence recorded, deployment/migration/rollback and flags verified, and actual post-deployment pharmacist/prescriber/patient journeys demonstrated. Source review, HR credentials, an approval endpoint, event publication or a passing schema check alone cannot close #169.

## Primary-source and review limits

Consulted PCI's published Pharmacy Practice Regulations and current FAQ discovery on 26 November 2026:

- https://www.pci.gov.in/documents/25/Pharmacy-Practice-Regulations.pdf
- https://www.pci.gov.in/en/faqs/

They support the distinction between registered-pharmacist responsibility and software role membership, and make substitution a consequential reviewed boundary. This review does not certify the full current amended legal position or a nationwide exception policy. Applicable council validity, permitted changes, controlled products and emergency workflows need current primary-text and qualified pharmacist/legal review before activation. The issue's sample clinical notes and schema sketches are requirements material, not authorized care instructions or activated policy.

## Slice 1 — draft command intent contract

Implemented in this branch, not a registered API or enabled clinical workflow:

- Canonical input: `contracts/pharmacy-verification.json` (`status: draft_not_registered`).
- Generator: `node scripts/generate-pharmacy-verification.mjs` and `--check`.
- Go input types/rules: `backend/internal/modules/pharmacy/verification_inputs_generated.go`.
- Server intent validation: `backend/internal/modules/pharmacy/verification_input.go`.
- Browser schema: `packages/api/src/pharmacy-verification-contract.ts`.
- Focused Go/browser behavior tests and generator drift tests; CI checks the canonical outputs.

Decision intent names `expected_version`, exact lowercase SHA-256 `reviewed_state_hash`, one of the four decisions, nullable `reason_code`, nullable `clinical_note`, and nullable `prescriber_amendment_id`. Non-plain approval requires a machine reason and nonblank clinical note; approval with change requires an amendment reference, and other decisions forbid that reference. An amendment UUID is only an assertion to resolve against the clinical owner later; it is not proof of a lawful amendment or an authorization to substitute. Hash equality is only a stale-review precondition; no canonical clinical snapshot or digest-generation algorithm is activated by this slice.

Second-check intent binds `expected_version`, `reviewed_state_hash` and `primary_decision_id`. It never accepts a second pharmacist identity in the command body. Human identity, registration trust, current privileges, same-person independence, source lineage, scope and policy must come from authoritative server checks when the route is implemented. No operating template, IAM permission, HTTP route, event, migration, patient disclosure or dispensing gate is added here. Go helpers validate typed intent only and cannot establish JSON field presence; future handlers still require strict JSON decoding, unknown-field rejection and all owner/authority checks. Registration validity and runtime availability remain unresolved release dependencies.

The client schemas and typed server validation use canonical text bounds (2,000 UTF-16 code units for clinical notes), nonzero UUIDs, safe-integer versions and strict digest/reason formats. Browser schema objects reject unknown fields, including caller-supplied tenant/pharmacist/registration assertions and an emergency bypass flag. The contract carries no default clinical policy and grants no authority. Emergency supply is not a decision in this contract.

Remaining implementation is intentionally tracked by the delivery order and full acceptance matrix above. Do not export these draft types from the public SDK index, register a route or enable a feature based only on this foundation. Complete server authority, retained receipts, state/transaction guards, context and queue before release.

## Slice 2 — recorded source context (unreleased candidate)

The read-only `prescriptions/:id/review-context` route returns the same canonical
recorded prescription as the existing record read, a server-computed source
fingerprint, exact owner/reader scope and explicit unavailable clinical data. The
route is registered to the `unreleased` capability: no provider toggle or existing
pharmacy pilot can make this candidate available. No new clinical permission or
assignment is granted, and no decision or supply command is added.

The canonical response definitions now own the existing Go record/line models and
SDK prescription response schema as well, avoiding a second wire model. Historical
prescription storage and output field names remain unchanged. The new context
validates retained source shape, reads balances in the same transaction and uses
the existing audited prescription disclosure boundary. The SDK sends tenant and
subject preconditions and rejects a response from a different reader, tenant,
pharmacy, outlet or prescription.

The fingerprint uses `helivanta.pharmacy.prescription-source.v1` domain separation,
the verified owner tenant/pharmacy and JSON encoding of the complete record,
including remaining units and repeat counts. It sorts a copied line list and
normalizes the recorded instant to UTC; a golden synthetic vector pins version 1.
It is a source-change precondition only, not authenticity proof or the final
reviewed clinical/supply state. Images, current product characteristics and signed
clinical revisions are not inferred from the captured record. Current clinical
data and professional authority must be integrated before decisions can be enabled.

Tests cover source changes, order/time canonicalization, invalid facts, scoping,
reader switches and explicit unavailable data. Real-database HTTP tests cover
existing 403/404 boundaries, successful disclosure audit and audit-failure refusal;
their execution requires hosted validation and is not claimed from the pure
local source/SDK tests. No browser acceptance or production activation is claimed.

## Review workspace candidate

The [completion specification](../superpowers/specs/2026-11-26-pharmacist-verification-completion.md)
and [execution ledger](../superpowers/plans/2026-11-26-pharmacist-verification-completion.md)
retain the full remaining #169 scope. The recorded prescription list now has an
internal review-panel integration behind compiled `unreleased` eligibility,
matching the audited context API. Existing pilot flags cannot expose it.

The panel consumes the owner SDK rather than reconstructing clinical data. It
shows named patient/prescriber source details, prescribed lines and remaining
recorded allowances, explicitly unavailable clinical context and no approval
controls. It never displays account, product, outlet or prescription IDs as
human names. Query data is account/outlet/record-bound, discarded on close and
cleared on tenant transition. Refreshes and failures hide previous context;
late responses cannot populate a changed account. Component tests exercise
these UI and recovery guarantees; they are not evidence of clinical authority,
signed prescription integration, decision persistence or a working supply gate.
