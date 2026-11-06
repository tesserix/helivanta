# Staff ownership, departments and scoped RBAC

Date: 2026-11-06. **Status: ownership/collaboration design for product review, with a local hospital department-team implementation candidate. No new contract approval or production rollout is implied.**

This document restarts the staff/access discussion from the organization tree. It covers hospital-owned departments, their delegated staff administration, independent pharmacy businesses and branches, and independent operators serving hospitals. Hospital departments are owned and managed by the hospital. PharmaConnect is software used by a pharmacy business; the software product itself is not an employer or a department.

The design preserves accepted [ADR-0024](../../adr/0024-pharmacy-ownership-and-outlet-tenancy.md) and [ADR-0044](../../adr/0044-organization-structure-and-access-defaults.md). Standalone Workforce scope is already decided by [ADR-0054](../../adr/0054-typed-employing-establishments.md); its remaining implementation slices and the proposed cross-tenant service relationships still need their applicable review gates. Existing ORG-D2, ORG-D5 and ORG-D6 remain open. All names marked proposed are illustrative, not registered permissions or supported wire fields.

## Merge baseline and accepted decisions

This draft was initially researched in the older, dirty local worktree at `b812bcff`. Before merging, its references were reconciled with remote `main` at `ab9f8a65`. The [research record](research.md#main-branch-reconciliation) distinguishes the historical observations from that newer baseline. This document does not reset completed staff work or authorize reimplementation.

Current `main` already contains the organization module, expanded hospital staff-access templates, department-aware IAM authority and pharmacy department-binding work. Reuse them under [ADR-0051](../../adr/0051-shared-department-owner-boundary.md), [ADR-0052](../../adr/0052-staff-source-command-authorization.md) and [ADR-0055](../../adr/0055-department-scoped-assignments.md). Their exact current contracts take precedence over illustrative proposal names here. New work concentrates on the independent-employer gap and any missing complete consumer journeys, with source verification before implementation.

ADR-0055 explicitly lets a department assignment cover current descendants. Organization supplies the current locked ancestry; OpenFGA holds no duplicate department hierarchy. This proposal preserves that rule. The proposed explicit inclusion/review of new **product outlets** does not change department/sub-unit inheritance. Existing approved workload budgets are also unchanged by the illustrative pharmacy planning scenario below.

The [department team-management review](department-team-management.md) applies the accepted Staff, supervision and transfer decisions to the user’s next focus: assigning staff, department heads, supervisors and safe transfers.

## 1. The tree: what we understand

```text
Helivanta platform                                      [software operator]
│
├── Tenant H: hospital customer / hospital group         [customer boundary]
│   ├── Legal entity / employer                          [accountable business]
│   ├── Tenant owners and administration                 [governance, not clinical]
│   ├── Employer staff register                          [one HR owner]
│   └── Hospital H1                                      [care/data boundary]
│       ├── Hospital administration and HR
│       ├── Pharmacy department D-PH                     [hospital-owned]
│       │   ├── Department head                          [supervision assignment]
│       │   ├── Department staff administrator           [bounded HR/access duties]
│       │   ├── Department staff view                    [postings, not HR copies]
│       │   └── Runs hospital-owned pharmacy P-H1        [explicit product binding]
│       │       ├── OPD outlet O-1 → assigned staff
│       │       └── IPD outlet O-2 → assigned staff
│       ├── Laboratory department → sections → assigned staff
│       ├── Nursing department → wards → assigned staff
│       ├── Finance / billing → counters → assigned staff
│       └── Other departments → sub-units → assigned staff
│
└── Tenant P: independent pharmacy customer              [separate customer boundary]
    ├── Legal entity / employer                          [pharmacy-owned, not hospital]
    ├── Pharmacy owner and corporate administration
    ├── Pharmacy organization P1                         [typed employing establishment]
    │   ├── Its own employer Staff register and HR
    │   ├── Branch/outlet B1 → manager + pharmacist + cashier + stock staff
    │   └── Branch/outlet B2 → manager + pharmacist + cashier + stock staff
    └── Pharmacy organization P2                         [separate employing establishment]
        ├── Its own employer Staff register and HR
        └── Further branches/outlets → assigned staff

Tenant H / Hospital H1 ··· service agreement ··· Tenant P / Outlet B1
                         NOT an ownership edge
```

Read the hospital pharmacy nesting as **organizational responsibility**, not a second pharmacy ownership tree. The canonical product tree remains `tenant → pharmacy organization → outlet`; an effective-dated hospital/department binding describes who runs it. Hospital ownership and a service/partner affiliation are different relationship types. Same-tenant mixed businesses are permitted by ADR-0024, but sharing a tenant does not merge employer, hospital, or outlet access.

“Branch” means an outlet in the first slice. If a business needs a regional office containing several licensed premises/outlets, model that separately after confirming the need; do not invent two competing branch/outlet masters. An independent pharmacy requires no hospital or hospital department. Laboratory/supply/emergency businesses may later follow their own reviewed product-unit trees; do not activate standalone operation for every department automatically.

**Staff appear in several views but have one owning employment record per engagement.** Department staff pages, outlet staff pages, HR pages and access pages reference that record. One login may link to separately reviewed employments in multiple organizations; neither organization can discover the other's private HR record through that login. Under ADR-0054, one pharmacy establishment maps to one eligible Pharma organization; a corporate tenant containing P1 and P2 therefore has separately scoped employment records, even if both share a legal parent. Corporate HR requires explicit authority for each establishment.

## 2. Deliverables and reading order

- Start with the tree above and the [ownership overview](diagrams/01-ownership.svg).
- Open [the editable eight-page draw.io file](staff-and-rbac.drawio) in diagrams.net.
- [Department teams and atomic transfers](diagrams/08-department-team.svg) shows the local owner-backed implementation candidate; see [ADR-0061](../../adr/0061-department-team-and-atomic-transfers.md).
- Review [delegated administration](diagrams/02-administration.svg), [record ownership](diagrams/03-records.svg), [authorization](diagrams/04-authorization.svg) and [staff lifecycle](diagrams/05-lifecycle.svg).
- Sections 3–7 define ownership, roles and permission naming; sections 8–12 cover operations, enforcement and delivery.
- [Research and evidence](research.md) separates primary-source findings from product choices and current implementation.
- [Independent suppliers, hospital groups and real operating scenarios](partner-collaboration.md) explains named-user/group sharing, ordering, delivery, receipt, disputes and revocation, with [group access](diagrams/06-partner-groups.svg) and [trade handoff](diagrams/07-trade-handoff.svg) diagrams.

The SVGs are companion previews. The draw.io file contains native editable shapes and connectors, rather than flattened screenshots. The text is the normative draft when an overview diagram omits detail.

## 3. Ownership rules

| Fact                                                        | Single owner                                           | Consumers and limits                                                                                    |
| ----------------------------------------------------------- | ------------------------------------------------------ | ------------------------------------------------------------------------------------------------------- |
| Tenant, legal-entity links and entitlement                  | Platform customer/organization registry                | Every product; a subscription enables a capability, not a person's access                               |
| Hospital department/sub-unit tree                           | Platform organization-structure owner, per ADR-0044    | HR, IAM, products; hospital authorizes changes, products reference canonical IDs                        |
| Physical location                                           | FacilityConnect                                        | Departments and outlets link locations; location does not grant access                                  |
| Staff employment/engagement, postings and HR history        | Workforce                                              | Department/product staff views use authorized projections; no product-private employee master           |
| Authentication identity                                     | Zitadel through the Helivanta login boundary           | IAM links a verified subject; email/name/phone matching is insufficient                                 |
| Reviewed staff-to-subject link                              | Workforce                                              | IAM consumes eligibility/current-state evidence; link alone grants nothing                              |
| Role template, assignment, grant approval and revocation    | IAM                                                    | Product checks remain authoritative for the object and its workflow                                     |
| Pharmacy organization, outlet and pharmacy business records | PharmaConnect                                          | Workforce references product-unit IDs; it cannot change stock or dispense records                       |
| Professional credential / designation / local privilege     | Respective credential and accountable governance owner | IAM/products consume current evidence; administrative users cannot waive it                             |
| Prescription/patient identity/encounter                     | Existing clinical owners                               | Pharmacy receives an authorized fulfillment contract, never an independently edited prescription master |

### Independent staff require an explicit Workforce extension

**Accepted direction:** ADR-0054 makes Workforce the single Staff owner for typed employing establishments (`hospital` or `pharmacy`). Current source includes typed establishment registration; that foundation is not evidence that every independent enrollment, private-file, posting or IAM consumer is complete. Reuse the accepted model rather than a Pharma-owned HR implementation.

The canonical root follows ADR-0054: an employing establishment has immutable kind and ID. A hospital retains its hospital ID; a pharmacy establishment is exactly one eligible independent/chain Pharma organization with the same ID. The existing physical `hospital_id` column is retained as an establishment key behind typed guards; it is not permission to expose a pharmacy as a hospital. Hospital routes admit only hospital establishments; reviewed pharmacy route families emit `pharmacy_id`. Outlets remain Pharma-owned posting targets. Legal-entity references do not create an alternative tenant-wide HR root.

All associated HR tables, private files, payroll configuration, identifiers, links, reviewer authority, RLS, generators and consumers must migrate together for the chosen slice. No unrelated HR history becomes chain-wide readable because the employer is the same. Employer-wide HR administration needs a specific assignment; a branch manager sees only the staff fields and postings their branch process requires.

The first implementation can keep hospital staff unchanged and gate independent staff enrollment as unavailable until this extension passes its contract and isolation gates. Independent business/catalogue operation already existing in PharmaConnect is not proof that independent HR exists.

## 4. The access model: role + action + scope + conditions

```text
Assignment = verified subject + versioned role template + tenant
           + explicit scope + validity + approval + provenance

Allow = current identity and membership
    AND product entitlement
    AND active, approved assignment granting the action
    AND object inside that assignment's permitted scope
    AND current required credential / privilege / designation
    AND object rules, relationship, purpose and separation of duties
    AND current authority versions / epochs
```

A title, employment, posting, professional registration, licence scan, subscription, hospital affiliation or OpenFGA tuple alone grants no business authority. Scope and action must come from the **same qualifying assignment**; never combine one assignment's permission with another assignment's scope. Independent assignments may authorize different actions, but mandatory restrictions apply to every authorization path.

Scope types are explicit: tenant, employer workforce scope, hospital, department, sub-unit, pharmacy organization, outlet, supply premise, emergency base. Templates enumerate allowed scope types. Unknown/missing scope denies. A tenant-scoped owner template grants only its governance actions; it does not expand outlet operations or patient access.

### Inheritance must follow verified responsibility

- A department assignment covers the named node and current descendants under ADR-0055, using Organization’s locked ancestry check. Reaching hospital-owned product units additionally requires current approved responsibility bindings and a product template that supports that reach (ORG-ST-03).
- It never covers a sibling department, another hospital, or an independent provider's outlet merely because the provider serves the department.
- An outlet assignment is outlet-only. A pharmacy-organization assignment may cover descendants only if that template explicitly allows it. Proposed default: new outlets require explicit inclusion/review for operational and clinical assignments; fixed scope sets do not silently grow.
- Hospital departments have a single organizational parent. Matrix reporting, clinical supervision and service relationships are separate edges, not extra parents.
- For department re-parenting, preserve ADR-0055’s current-descendant semantics and guarded ancestry checks; this draft adds no mandatory reapproval policy to accepted department grants. Product operator/binding changes need their own impact review and current product checks. Historical actions retain their original scope/version.

## 5. Your naming convention, made precise

Use the requested names as **readable profile aliases**. Keep the shared platform permission vocabulary `<module>.<resource>.<action>` and shared IAM assignments underneath. This avoids creating a second authorization system for hospital pharmacy.

```text
Profile alias: hospital_pharma_rbac_<role>_<action>
Profile alias: pharma_rbac_<role>_<action>

Example: hospital_pharma_rbac_staff_admin_w
  template: workforce.department_staff_admin.v1             [proposed]
  action: workforce.staff.create                          [proposed]
  scope: Tenant H / Hospital H1 / Pharmacy department D-PH

Example: pharma_rbac_staff_admin_w
  template: workforce.outlet_staff_admin.v1                 [proposed]
  action: workforce.staff.create                          [same proposed permission]
  scope: Tenant P / Employer E-P / Pharmacy P1 / Outlet B1
```

Each alias resolves to an explicit template version, permission subset and approved scope selector. The prefix is descriptive; the server never derives authority by parsing `hospital_` or `pharma_`. Template keys already registered, such as `workforce.leave_reviewer.v1`, remain intact. New examples here are not replacements for current v1 contracts.

| Suffix | Meaning proposed for the profile editor                          | Boundary                                                                      |
| ------ | ---------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `r`    | Read/list the permitted resource fields                          | Sensitive identifiers, documents and patient data need explicit permissions   |
| `w`    | Create a draft/new resource                                      | Does not approve, activate, dispense or post money                            |
| `e`    | Edit permitted draft fields                                      | Does not overwrite immutable evidence or finalized clinical/financial actions |
| `a`    | Approve a named workflow transition                              | Not “all access”; no self-approval; action-specific limits apply              |
| `m`    | Manage access proposals/assignments within a grantable allowlist | Separate from business approval and from HR edit                              |
| `x`    | Export approved data fields                                      | Separate permission, audit and disclosure checks                              |

`a = approve` is a proposed interpretation because the original shorthand is ambiguous. The UI should spell out **Approve** and **Manage access** rather than show unexplained letters. “Write” and “edit” must not both map to an unrestricted database update. Avoid generic delete for staff, prescriptions, stock and money: use deactivate, retire, amend, reverse or correct with history.

### Examples of profiles

| Hospital-owned profile                  | Independent profile            | Actual action and scope                                                                                                             |
| --------------------------------------- | ------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------- |
| `hospital_pharma_rbac_staff_admin_r`    | `pharma_rbac_staff_admin_r`    | View operational staff fields in the named department/outlet                                                                        |
| `hospital_pharma_rbac_staff_admin_w`    | `pharma_rbac_staff_admin_w`    | Create staff intake in the same employer; activation follows reviewed enrollment policy                                             |
| `hospital_pharma_rbac_staff_admin_e`    | `pharma_rbac_staff_admin_e`    | Update allowed employment/contact/posting drafts; no salary/credential verification by implication                                  |
| `hospital_pharma_rbac_access_admin_m`   | `pharma_rbac_access_admin_m`   | Propose permitted role assignments in bounded units; clinical/privileged grants need independent review                             |
| `hospital_pharma_rbac_pharmacist_r`     | `pharma_rbac_pharmacist_r`     | Read only the authorized fulfillment/patient details at the assigned outlet                                                         |
| `hospital_pharma_rbac_pharmacist_a`     | `pharma_rbac_pharmacist_a`     | Resolve to an explicitly named pharmacy transition, e.g. approved dispense finalization; credential and business checks still apply |
| `hospital_pharma_rbac_stock_reviewer_a` | `pharma_rbac_stock_reviewer_a` | Approve a stock correction where the actor did not submit it                                                                        |

Do not create every role/action combination. Publish only supported combinations backed by actual handlers. Dispense finalization is a clinical command, not a generic CRUD approval button; its permission must be named for that act in the canonical product catalogue.

## 6. Actors and who appoints them

All templates below are proposed catalogue entries. Each has one authority kind; a person may hold several separately approved assignments. Reviewer authority must itself be current at decision time.

| Actor/template family            | Authority          | Normal scope                                      | Preconditions                                                                           | Who appoints / approves                                                       |
| -------------------------------- | ------------------ | ------------------------------------------------- | --------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| Tenant owner                     | Governance         | Tenant                                            | Verified ownership/bootstrap and recovery policy                                        | Reviewed onboarding or existing independent owner; never a self-service grant |
| Hospital administrator           | Administration     | Named hospital                                    | Active engagement and approved administrative remit                                     | Authorized tenant access approver; privileged review applies                  |
| Hospital/employer HR officer     | Administration     | Named hospital/employer workforce scope           | HR confidentiality/remit; active reviewed staff link                                    | Authorized employer/hospital access approver                                  |
| Department head                  | Supervision        | Department                                        | Current posting; designated leadership approval                                         | Hospital management; IAM approval by authorized independent reviewer          |
| Department staff administrator   | Administration     | Department and permitted units                    | Delegated HR fields/actions and intake limits                                           | Hospital HR/access authority; head may nominate                               |
| Department access administrator  | Administration     | Department                                        | Explicit grantable-template allowlist and scope ceiling                                 | Hospital privileged access approver                                           |
| Pharmacy corporate administrator | Administration     | Pharmacy organization or employer workforce scope | Independent pharmacy customer; approved business remit                                  | Pharmacy owner via independent privileged review                              |
| Outlet manager                   | Supervision        | Named outlet(s)                                   | Active employer engagement and outlet posting                                           | Pharmacy corporate access approver                                            |
| Outlet staff administrator       | Administration     | Outlet staff view/intake                          | Bounded intake/edit delegation                                                          | Employer HR/access authority                                                  |
| Dispensing pharmacist            | Clinical           | Outlet and authorized prescription context        | Current professional evidence, outlet authority, applicable local privilege/designation | Credential/governance reviewer plus independent access approver               |
| Cashier / stock clerk            | Administration     | Outlet/counter                                    | Training/engagement and process-specific assignment                                     | Authorized outlet/business approver; no dispensing by default                 |
| Clinical supervisor              | Clinical           | Named trainee, service and period                 | Supervisor's own qualifications/privileges and countersignature rules                   | Clinical governance                                                           |
| Auditor                          | Administration     | Approved audit scope and purpose                  | Independence; explicit data-class/read/export limits                                    | Authorized independent privileged approver                                    |
| Platform support operator        | Platform operation | Tenant-authorized support session                 | Ticket, reason, expiry, tenant authorization; read-only default                         | Separate just-in-time support process, never a standing tenant role           |

A pharmacist-in-charge is a separately evidenced designation, not a role alias that an IT administrator can assign as proof of legal eligibility. A hospital department head can be a supervisor without being authorized to dispense. A chain owner can manage the business without being allowed to view every patient's medication history.

### Practical access matrix

“Bounded” means a dedicated permission, exact scope, permitted fields and workflow checks; it never means all records under a tenant.

| Capability                                     | Hospital/employer HR         | Department staff admin      | Access admin                 | Outlet manager                            | Pharmacist                                         | Cashier/stock clerk                              |
| ---------------------------------------------- | ---------------------------- | --------------------------- | ---------------------------- | ----------------------------------------- | -------------------------------------------------- | ------------------------------------------------ |
| Staff intake                                   | Bounded                      | Bounded                     | No default                   | Only with separate staff-admin assignment | No default                                         | No default                                       |
| Operational team directory/postings            | Bounded                      | Own department              | Minimum review evidence      | Own outlet                                | Own operational team, if granted                   | Own operational team, if granted                 |
| Salary, bank/tax identifiers, private HR files | Separate sensitive HR grants | No default                  | No default                   | No default                                | Own self-service fields only                       | Own self-service fields only                     |
| Propose access                                 | Only if separately assigned  | Only if separately assigned | Allowlisted templates/scopes | Only if separately assigned               | No default                                         | No default                                       |
| Approve own access                             | Never                        | Never                       | Never                        | Never                                     | Never                                              | Never                                            |
| Verify professional evidence                   | Separate reviewer assignment | No default                  | Cannot waive                 | No default                                | Cannot self-verify                                 | No default                                       |
| Clinical dispensing                            | No default                   | No default                  | No default                   | Separate clinical assignment              | Explicit clinical permission and all preconditions | Never by clerk/cashier role                      |
| Stock correction                               | No default                   | No default                  | No default                   | Separate submit/review authority          | Only with stock assignment                         | Submit if granted; cannot approve own submission |
| Financial refund approval                      | No default                   | No default                  | No default                   | Separate financial assignment/limit       | No default                                         | Separate authority; no self-approval             |

## 7. Administration is delegation, not ownership transfer

The hospital creates its department, appoints its head, and may empower a pharmacy department administrator to create/manage the department's staff. Those staff still belong to the hospital's employer-owned register. The department administrator is not a tenant administrator and does not acquire other departments' access.

Two bounded operating modes are supported by this proposal:

1. **Central intake:** department admin submits intake; HR approves employment activation and sensitive changes. This is the proposed initial default.
2. **Delegated intake:** hospital/employer explicitly permits the department or outlet staff administrator to complete designated enrollment steps directly. It specifies allowed fields, unit IDs and employment categories; HR retains employer-wide governance. Access approval and credential verification remain independent.

Neither mode lets staff intake create a privileged user or a dispenser. Enrollment, login invitation, subject-link review, membership, access proposal, credential review and assignment activation are distinct steps. A staff record may have no login. An invited login may have no staff link or business assignment.

The access administrator may assign only templates on a reviewed `grantable_templates` allowlist and within their scope ceiling. Clinical eligibility is checked by its accountable owner. The administrator cannot edit their own allowlist, publish a more powerful template, approve their own access, widen a scope set, bypass independent approval, or reactivate expired evidence. Tenant-composed roles remain a proposal pending ORG-D6; custom labels cannot introduce new server permissions.

Temporary acting appointments are subset delegations with fixed expiry, no re-delegation, no longer validity than the delegator, and the delegate's own preconditions. Accountability remains visible on each action. Removal of the source authority invalidates dependent delegations.

## 8. End-to-end staff journeys

### A. Hospital-owned pharmacy

1. Verify tenant, hospital, accountable employer and product entitlement. Hospital administrator creates the canonical Pharmacy department through its structure owner.
2. Appoint the department head and bounded staff/access administrators through independent IAM review. Clinical capabilities are separate assignments.
3. PharmaConnect creates the hospital-owned pharmacy/outlets. Record current hospital/department responsibility bindings; an unreviewed affiliation remains non-authoritative.
4. Department staff admin creates an intake referencing the hospital employer and department. Existing staff may receive a posting rather than a duplicate HR record.
5. HR activates employment under the selected central/delegated policy. Review the login link; explicitly establish membership where required. Neither step grants business permission.
6. Create exact access proposals: operational staff administration at department scope, dispensing at named outlets, stock/finance roles separately. Capture approver, evidence, reason, expiry and template version.
7. IAM checks independent approver and current owner eligibility, commits approved state and audit, projects to OpenFGA, and activates only after required acknowledgments. The product checks current staff/link/clinical/object conditions on every consequential action.
8. A move, suspension, credential expiry, department retirement or exit removes dependent authority at the effective boundary. Views show access pending/unavailable instead of fabricated success while projections catch up.

### B. Independent pharmacy chain

1. Verify Tenant P, its legal employer, product entitlement and pharmacy organizations/outlets. No hospital is created.
2. For each employing Pharma organization, use its accepted ADR-0054 establishment scope and deliver its staff register and corporate HR/access administration through reviewed capability slices. A shared legal parent does not merge P1/P2 employment roots.
3. Corporate HR creates staff; a branch staff admin may perform only delegated intake/edit steps for B1. Branch postings and role assignments are separate records.
4. Assign B1 pharmacist, B1 cashier, B1 stock clerk or multi-outlet supervisor explicitly. A B1 administrator cannot grant B2 access. Corporate HR/private-file access does not flow to outlet managers.
5. A floating pharmacist has one employer engagement, dated B1/B2 postings and separate outlet assignments, with the appropriate evidence at each outlet. Scheduling clashes and access validity are separate checks.
6. New branches do not receive every corporate user's clinical/operational grants automatically. Use reviewed scope sets, impact preview and explicit activation.
7. Transfer or exit follows the same revoke-first lifecycle as hospital staff. A branch transfer is not a tenant switch or an employment ownership transfer.

### C. An independent pharmacy operates a hospital counter

The pharmacy remains the employer; the hospital owns its department and clinical records. The service relationship identifies both legal parties, hospital/department, operating outlet, permitted services, contract/evidence versions, effective dates and suspension/revocation conditions.

**Proposed first boundary:** no implicit cross-tenant user access. Each side operates on its own records in its own tenant. An authorized fulfillment integration transfers only the required prescription/order and completion facts under a separately reviewed contract. Hospital referral, common ownership, shared location and an affiliation row confer no access. The existing same-tenant affiliation table does not implement this proposed cross-tenant agreement.

If direct hospital-workspace access is later necessary, the hospital must explicitly invite/review a separate guest engagement/link and approve hospital-scoped assignments. The pharmacy's home-tenant role cannot authorize a hospital request. A tenant switch re-mints session context and clears/rekeys cached state; never merge assignments across tenant contexts. Ending the agreement revokes every relationship-dependent guest assignment and stops future disclosure, while preserving prior dispense/financial/audit evidence.

The hospital may approve who works on site and the pharmacy approves its employment/branch posting. Neither approval is automatically the other's employment, clinical privilege or data disclosure grant. Cross-party HR visibility is limited to service/credential evidence expressly needed by the agreement; salary/bank/tax/private HR history stays with the employer.

### D. Independent pharmacy supplies hospitals and shares with their teams

The [partner-collaboration design](partner-collaboration.md) extends the tree with separate trading relationships for each hospital, hospital-owned groups, named-user or typed group grants, and approved field/action policies. It permits explicit partner views and each party’s own workflow contributions; it grants no implicit cross-tenant workspace, stock administration or patient access. A clinical fulfillment contract, outsourced counter agreement and bulk procurement agreement remain distinct. This is an addition to the required business-integration review, not a deployed group-sharing feature.

## 9. Conceptual records and lifecycle

These are ownership requirements for later schema design, not migration instructions or a parallel wire-model source.

| Record                                    | Required facts and invariants                                                                                                                                                                                   |
| ----------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Department/sub-unit                       | Tenant, real hospital, canonical parent, kind, effective dates, version, retirement; no cycles or cross-hospital parent                                                                                         |
| Product responsibility binding            | Tenant, hospital/department and product-unit references, relationship type, validity, review and version; servicing alone is not ownership                                                                      |
| Employment/engagement                     | Owner tenant/employer/workforce scope, staff ID, category, status, effective dates; private HR history remains owner-scoped                                                                                     |
| Posting                                   | Employment, canonical unit/outlet, managerial reporting and dates; no implicit grant                                                                                                                            |
| Staff login link                          | Employer-scoped staff reference, verified subject, reviewer, validity/status and immutable change history                                                                                                       |
| Role template version                     | Authority kind, exact declared permissions/data classes, allowed scope types, preconditions, grantable list and publication review                                                                              |
| Assignment                                | Subject, tenant, template version, exact scope or immutable scope-set version, validity, source posting/delegation/relationship, requester, independent reviewer, reason, status, expected version and epochs   |
| Credential/privilege/designation evidence | Issuer/authority, verified scope, service/act, source/effective version, reviewer, expiry and revocation; software cannot manufacture qualification                                                             |
| Service relationship                      | Both independently verified parties/scopes, purposes, disclosure fields, permitted acts, policy/version, validity, approvers and suspension state                                                               |
| Audit evidence                            | Actor and acting/delegation context, owner tenant/scope, command/object IDs, transition, assignment/template/version, reason/evidence references, timestamp and result; no tokens or unnecessary HR/PHI payload |

Use separate state machines:

```text
Employment: draft → pending review → active → suspended / ended
Posting:    proposed → approved → effective → ended
Login link: proposed → independently reviewed → active → revoked
Assignment: proposed → owner verification → independent approval
          → projection pending → active → suspended / expired / revoked
```

Rejected proposals remain historical; they do not get edited into approved ones without a new reviewable revision. Assignment activation uses the current worktree's reviewed proposal/snapshot mechanisms where compatible. Exact existing status enums must be consulted before coding; the labels above describe the business journey.

| Trigger                          | Required access effect                                                                                                                  |
| -------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| Staff intake or login invitation | No business permission                                                                                                                  |
| Posting start                    | Propose access; approved future assignment may become effective only after current checks                                               |
| Transfer / posting end           | Stop old posting-dependent assignments at the effective instant; new scope requires approval                                            |
| Engagement suspension / exit     | Immediately deny dependent access, links and delegations; HR/audit history remains                                                      |
| One employer engagement ends     | Revoke assignments sourced from that engagement; separately valid employments remain; security-wide suspension may disable the identity |
| Credential / privilege expiry    | Deny dependent clinical actions at expiry; unrelated administrative assignment may remain                                               |
| Service agreement ends           | Deny relationship-dependent access/disclosure across both parties; preserve completed evidence                                          |
| Rehire / re-enrollment           | New reviewed link/assignment and membership generation as applicable; old snapshots cannot revive                                       |
| Template changes / scope moves   | Version and re-review consequential widening; do not silently append grants                                                             |

Credential and expiry checks use authoritative time at the command boundary; they do not rely solely on a nightly job or an event arriving. Emergency patient access is a separate clinician-only, patient-scoped break-glass path with reason, short expiry and retrospective review (ORG-AU/AC, #432). It never grants HR/access administration or bypasses pharmacist qualification, safe-stock checks or financial separation of duties. Downtime uses an institution-approved continuity process; authorization dependency failure is not permission to allow an online action.

## 10. Enforcement, consistency and failures

All UI/app business requests enter Global API with an explicit destination. Global API verifies routing/session context; the owning product reauthorizes the resource and action. UI visibility is a convenience, not a control. Keep the existing modular ownership and Postgres/OpenFGA stack; this design introduces no new network service or datastore.

Authorization reads current membership/engagement/link/assignment/required evidence versions. A successful graph check must be revalidated against current authority before commit. Permission and scope checks precede disclosure; scoped queries, exports, searches, counts, downloads and audit views need the same constraints as mutations. Database keys/RLS include verified owner tenant and relevant hospital/employer/outlet; client IDs/headers never select authority.

For commands, define the transaction's authorization point: lock/compare the relevant current scope/assignment and target versions, recheck effective validity, then commit the allowed transition and audit atomically. A revoke concurrent with a command serializes against this boundary; already committed physical actions are corrected/compensated with evidence rather than erased. Do not claim that an event can cancel an action already completed.

### Reliable activation and revocation

1. The owner commits the lifecycle/assignment decision, epoch or deny watermark, audit and outbox intent in one local transaction.
2. Workers project to OpenFGA and other owner-authorized projections outside database locks. Messages include only required scoped references/version/correlation, not payroll or whole staff profiles.
3. Consumers deduplicate and verify ownership, current state and versions. Stale/out-of-order messages cannot overwrite newer state. Projection failure retries with bounded backoff and a visible dead-letter/reconciliation queue.
4. Grant stays pending until current eligibility and required projection acknowledgments agree. Revocation denies through current owner authority before stale graph grants can be used; a cached positive answer cannot override the deny watermark.
5. **Before widening beyond existing hospital/department staff consumers, define a live authority gate for independent employment, outlet postings and clinical credentials.** An asynchronous HR event alone cannot meet immediate revocation. If freshness cannot be established, protected operations return unavailable until reconciliation. A documented command-level test must prove this boundary across each affected owner.

| Dependency / condition                                                 | Required behavior                                                                                              |
| ---------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| No valid identity                                                      | 401                                                                                                            |
| Out-of-scope object                                                    | 404, without revealing existence                                                                               |
| In-scope object, missing action permission or unsatisfied precondition | 403 with a safe stable reason code                                                                             |
| Authz, membership, current required eligibility or graph unavailable   | 503; no mutation, no cached allow fallback                                                                     |
| Wrong current session tenant / stale command version                   | Existing 409 contract where supported; never retry under a different tenant                                    |
| IAM/owner database unavailable                                         | Deny protected commands; show unavailable, not a zero count or empty fabricated team                           |
| Broker/projection worker unavailable                                   | Grants remain pending; current-state deny still protects revocations; alert and reconcile                      |
| Audit storage unavailable                                              | No sensitive disclosure or consequential commit unless the required durable audit intent can commit atomically |
| Optional notification unavailable                                      | Access control still takes effect; retry notification durably                                                  |
| Optional UI read projection stale                                      | Label freshness and restrict to non-authoritative data; never use it to grant access                           |

Preserve current v1 `{error,message}` plus correlation header behavior from the [HTTP contract](../../api/http-contract.md); do not silently introduce a different envelope. The current route gate can return 403 before object lookup; the 404 requirement concerns invisible domain objects, not a claim that all existing unauthorised routes already return 404.

Every new command must specify durable idempotency identity/fingerprint, replay result, changed-content conflict and `expected_version` semantics. A timeout after commit retrieves the original result rather than creating another assignment or stock act. These are requirements for new contracts, not claims of a universal current `Idempotency-Key` implementation. Assignment history and audit are append-only; retention/deletion follows reviewed policy and legal holds rather than a hardcoded period in this design.

## 11. Staff-management user experience

The workspace always shows the active customer and hospital or pharmacy/outlet. A person working for two customers chooses a context; their role badges are never unioned across those contexts.

| Workspace                 | Primary views/actions                                                            | Boundaries visible to the user                                                                 |
| ------------------------- | -------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| Hospital HR               | Employer staff, reviewed links, hospital postings, confidential HR processes     | Employer/hospital ownership and restricted sensitive fields                                    |
| Department → Staff        | Assigned staff, intake, posting requests, team rosters                           | Department scope; central versus delegated intake status                                       |
| Pharmacy → Outlet → Staff | Outlet assignments, intake where delegated, current qualification status         | Employer identity, outlet and effective dates; no copied employee master                       |
| Access administration     | Proposals, reviewer queue, assignment history, delegation, recertification       | Template, exact actions, exact scope, grantor, reviewer, reason, expiry and pending projection |
| My access                 | Own assignments and effective/expired/pending state                              | Separate clinical, supervision and administration authority                                    |
| Access review             | Who can do what, stale evidence, expiring assignments, departed staff exceptions | Field-level redaction and audit scope; no unrestricted patient drill-down                      |

“Create staff” asks for employer/context and enrollment fields, not an all-powerful role dropdown. “Assign access” previews the exact actions and outlets/departments affected; rejects escalation before submission; distinguishes Requested, Approved, Activating, Active and Revoked. A department head nomination does not display clinical authority until its separate review completes.

For each denied action, present a useful permitted explanation without revealing another unit's staff/object. Pending evidence, unavailable authorization and permission denial are distinct. Export and private-document downloads have separate disclosure review and audit. Recertification intervals and grace rules are employer-approved policy; unattested grants follow that policy rather than an invented universal interval.

## 12. Delivery slices and acceptance

This document authorizes no implementation, migration, production rollout or live credential change. Review the tree and ownership decisions first, then implement complete bounded slices.

| Slice                               | Complete outcome / exit gate                                                                                                                                                                 |
| ----------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0. Resolve decisions                | Confirm profile shorthand, apply accepted ADR-0054 employing-establishment scope, and review remaining branch/intake/product-scope/service decisions; record genuinely new decisions in ADRs |
| 1. Close administrator safety gap   | Remove blanket clinical grants from admin reachability under a reviewed migration; preserve legitimate explicit clinical assignments; prove both positive and negative cases                 |
| 2. Hospital department foundation   | Reuse the existing Organization owner and department-aware IAM authority; close verified consumer/binding gaps for head/admin assignments and retirement/transfer behavior                   |
| 3. Hospital pharmacy staff slice    | Intake → reviewed staff link → bounded IAM assignment → real department/outlet consumer → immediate current-state denial on suspension/exit; no legacy bypass                                |
| 4. Independent pharmacy staff slice | Real employer Workforce scope across all affected HR/file/access tables; chain/branch views, controlled posting/assignment, rehire and negative scope cases                                  |
| 5. Independent operator integration | Reviewed bilateral service contract, minimal authorized order/fulfillment exchange, termination behavior; guest access only as a separately reviewed need                                    |

### Minimum synthetic scenarios

| Scenario                                                                            | Expected result                                                                 |
| ----------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| Hospital pharmacy staff admin creates intake in D-PH                                | Allowed only by the intake policy and staff permission; no login/clinical grant |
| Same admin targets Laboratory or Hospital H2                                        | 404 for invisible target; no staff record created                               |
| B1 staff admin targets B2 or another pharmacy employer                              | Denied even within the same tenant                                              |
| B1 pharmacist has a B2-only unrelated read assignment                               | Cannot combine B1 dispense permission with B2 scope                             |
| Hospital/chain owner opens patient chart or dispenses without clinical assignment   | Denied                                                                          |
| Active pharmacist assignment but expired required credential                        | Denied at expiry even while broker is down                                      |
| Qualified pharmacist lacks outlet assignment                                        | Denied                                                                          |
| Department admin edits own role, allowlist or template permissions                  | Denied                                                                          |
| Requester/subject approves own access; stock submitter approves own correction      | Denied by the owning command                                                    |
| Independent pharmacy serves H1 but holds no hospital assignment/authorized exchange | No hospital record or HR visibility                                             |
| One person's employment ends in Tenant H but remains valid in Tenant P              | H-derived access ends; no cross-tenant private HR disclosure                    |
| Suspension races with a command                                                     | Defined serialization point; no post-revocation commit using stale authority    |
| Projection activation is delivered twice or old activation arrives after revoke     | One effective transition; old activation cannot restore access                  |
| Assignment expired, posting ended, outlet moved or department retired               | No silently widened/continued access; history retained                          |
| Rehire or membership re-enrollment                                                  | Old snapshots and links cannot revive                                           |
| Permission graph/current owner authority unavailable                                | 503 and no consequential commit/disclosure                                      |
| Sensitive HR export or private file requested by outlet manager                     | Denied without dedicated sensitive read/release authority                       |
| Clinical/stock action already committed, downstream billing fails                   | Preserve original action; expose pending recovery, no deletion/duplicate act    |

Contract review uses [Global API gates](../../api/global-connector.md), [schema ownership](../../api/schemas.md), [integration contracts](../../api/integration-contracts.md) and [delivery review](../../api/review-plan.md). Edit canonical JSON inputs and regenerate Go/Zod outputs. Once routes/permissions/events/migrations change, run `go run ./cmd/contractdocs` and `go run ./cmd/contractdocs -check` from `backend/`, plus the affected generators, package tests, typechecks/builds/lints and real consumer journey checks. None of these application changes is made by this design package.

### Migration and recovery constraints

Inventory existing roles, `pharmacy_outlet_access`, Workforce hospital grants, private-file authority and immutable assignment snapshots. Classify each legacy grant into an explicit role/scope; unmappable grants need review rather than a blanket wildcard. Add schemas/contracts compatibly, backfill with provenance, compare old/new decisions in non-authoritative shadow evaluation, resolve mismatches and cut over one consumer at a time. A consumer must use one authoritative path, never `legacy_allow OR new_allow` to bypass new restrictions.

Revocations, expiry and exclusion rules must protect every still-enabled legacy path during transition. Reconcile row counts, ownership, expected permissions and denied cases before disabling old writes. Keep historical records and recovery checkpoints. A UI rollback can hide a new workflow, but must not restore revoked/blanket admin permissions. If accepted writes change data ownership, recovery is forward reconciliation from durable history; reversing live ownership or applying migrations needs separately approved operational steps.

### Draft sizing and SLO assumptions, not measurements

This is an illustrative pharmacy-specific scenario, not a replacement for ADR-0051’s inherited Staff budgets (500,000/1.5m Staff at 12/36 months, 50 peak Staff requests/sec and 80:20 reads:writes). Ratify this scenario’s separate scope before capacity decisions. Use a planning envelope of 100 customer tenants, 10,000 active staff at month 12, 50,000 at month 36 and 12 assignments/staff: 120,000 then 600,000 current assignments. Retain versions/audit separately under an approved retention policy. At roughly 2 KB logical data per current assignment, this is about 240 MB then 1.2 GB before indexes, history and replication. An illustrative 1 million audited sensitive decisions/day at 0.5 KB each is about 183 GB/year before indexes/replicas; measure actual audit frequency and payload before capacity commitments.

Plan for 500 protected requests/sec peak, 20 access/lifecycle changes/sec during bulk operations, 95:5 ordinary reads:writes, and access command payloads below 16 KB excluding separately controlled attachments. Proposed targets: 99.9% monthly protected-path availability, p99 added authorization latency under 100 ms within a product request budget of 500 ms, grant projection p99 under 5 seconds, and immediate denial at the next command authorization boundary after authoritative revocation commit. Projection lag never grants a grace period. Reconcile these targets with the current five-second scoped-authorizer timeout and measure under load before promising them to customers.

Start with existing Postgres indexes/transactions, IAM/OpenFGA and outbox workers. No sharding, new policy engine, separate staff microservice or speculative generic business hierarchy is justified by this envelope. Incremental cost is assignment/history/audit storage plus measured worker/OpenFGA capacity on the existing stack; currency pricing requires region, retention, workload and budget inputs. Audit retention and decision volume dominate the illustrative storage, so financial cost remains unpriced pending those inputs.

## 13. Decisions for the design review

| ID     | Proposal / unresolved question                                                                                                                                                                                                                           | Accountable reviewers                         |
| ------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------- |
| SR-D1  | Settled by ADR-0054: use typed employing establishments and Workforce ownership; complete each consumer through its reviewed slice                                                                                                                       | Product, Workforce, platform/security         |
| SR-D2  | Hospital/pharmacy prefixes are profile aliases; shared canonical permissions and IAM assignments enforce them                                                                                                                                            | Product, IAM/security                         |
| SR-D3  | `a` means approve; access administration uses `m`; UI spells out actions                                                                                                                                                                                 | Product and UX                                |
| SR-D4  | Initial central intake with bounded delegated intake as an explicit employer policy                                                                                                                                                                      | Hospital/pharmacy operations, HR and security |
| SR-D5  | Branch initially equals outlet; regional/group structures only on verified need                                                                                                                                                                          | PharmaConnect and customer operations         |
| SR-D6  | Outsourced pharmacy stays separate; bilateral authorized exchange first, guest access separately reviewed                                                                                                                                                | Clinical/pharmacy, platform, privacy/legal    |
| SR-D7  | New product outlets do not automatically expand operational/clinical scope sets; preserve ADR-0055 department-descendant coverage                                                                                                                        | IAM/security and operational owners           |
| SR-D8  | Resolve ADR-0044 single-owner wording: it says privileged grants need a second person, but also describes immediate single-owner activation with later review. Do not implement a self-grant exception or claim notification equals independent approval | Product owner, security and governance        |
| SR-D9  | Standalone pharmacist evidence/operating-authority policy must use the real employer/outlet and applicable jurisdiction, not require a fabricated hospital privilege                                                                                     | Pharmacy governance, legal and IAM            |
| SR-D10 | Ratify workload/SLO/cost inputs and current-state eligibility gates before production commitments                                                                                                                                                        | Engineering, security and operations          |

SR entries are proposals or, where marked, references to already settled decisions; they do not replace accepted ORG decisions. ADR-0044's no-default-clinical-access, central department ownership and posting-proposes-access decisions remain the baseline. ORG-D2/D5/D6 need their own review before any affected feature relies on them.

Traceability: ORG-ST-01–07 (structure/history); ORG-AU-01–09 (authority and clinical separation); ORG-AC-01–08 (assignment, lifecycle, deny/failure and migration); IN-PH-02/04/05/06/10 (pharmacy act/evidence/outlet scope); IN-CL-10 and IN-FQ-06 via the organization authority reference. These are proposed Helivanta acceptance criteria, not regulatory certification. Country/state, service, licence and current source applicability require qualified review.

### Independent collaboration implementation candidate

The [delivery ledger](independent-collaboration-delivery.md) tracks the required
end-to-end work for Maharashtra and Odisha. The [membership implementation](partner-membership-implementation.md)
includes the current ownership tree, actor roles and endpoint/recovery contract.
Its [editable draw.io diagram](diagrams/09-independent-membership.drawio) and
[SVG preview](diagrams/09-independent-membership.svg) distinguish candidate
membership behavior from pending supplier integrations.
