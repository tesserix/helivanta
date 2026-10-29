# Organization, roles and access knowledge

[API contracts](../../api/README.md) · [India knowledge](../india/README.md) · researched 2026-10-26 · proposed target, not implemented behavior

Read this before changing roles, permissions, staff access, departments, approvals, hospital-configured workflows, or behavior that differs between public and private customers. Requirements tagged `ORG-*` are proposed Helivanta acceptance criteria. They describe the target, not deployed behavior; the [current implementation](#current-implementation) section records what the code does today. Nothing here is a legal opinion: statutory roles and duties need the source review described in the [India source register](../india/sources.md).

## Choose the topic

| Work                                                                                              | Reference                                                             |
| ------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| Who decides what: governance, management, administration, clinical, supervision, platform         | [Authority and roles](authority-and-roles.md)                         |
| Hospitals, departments, wards, outlets, lab sections, bases; what Staff and each product holds    | [Structure and departments](structure-and-departments.md)             |
| Permissions, role templates, scoped assignments, delegation, staff lifecycle, privileged access   | [Access model](access-model.md)                                       |
| Hospital-built workflows, approvals, forms, escalations and custom fields on top of product flows | [Configurable workflows](configurable-workflows.md)                   |
| India country pack: government and private hierarchy, titles, money, quality and reporting        | [Public and private sectors](../india/public-and-private-sectors.md)  |
| Existing tenancy, sharing and session design                                                      | [Tenancy and identity](../../architecture/02-tenancy-and-identity.md) |

## Rules that apply everywhere

1. **Authority follows the assignment, not the title.** A Medical Superintendent who still operates holds a management assignment and a separate clinical one. Each is scoped, audited and revoked on its own.
2. **Every assignment names its scope.** Tenant, hospital, department, sub-unit, outlet, lab section, premise or base. A missing scope is an error, never "everything".
3. **Clinical authority needs credential, privilege and assignment.** HR job category, a posting or an administrator's wish cannot make someone a prescriber, dispenser or result signatory.
4. **Granting needs a person; revoking never waits for one.** Postings and job changes propose access for approval. Leaving, suspension, posting end or credential expiry removes dependent access automatically.
5. **Management sees the organization; clinicians see their patients.** Owners, executives and administrators get aggregate and operational views by default. Identifiable patient data needs a clinical, audit or break-glass basis.
6. **Configuration extends; it never removes a platform control.** Hospitals add steps, approvals, fields and escalations. Consent, identity confirmation, drug-schedule controls, audit, tenant/hospital isolation and separation of duties stay.
7. **One owner per fact.** Workforce owns people and postings, IAM owns logins and assignments, the organization structure owns departments, each product owns its records. Nobody keeps a private copy of another owner's list.
8. **Sector is a profile, not a fork.** Public and private customers use the same model with different templates, titles, hierarchy levels and billing plans.
9. **Country is a pack, not a fork.** Helivanta serves hospitals in any country and launches in India first. Titles, public-sector levels and statutory designations come from a versioned country pack; the model above never hardcodes one country.

## Vocabulary that must remain distinct

| Term                     | Meaning for Helivanta                                                                                                                                         |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Tenant                   | Contracted customer: a private group, a single hospital, a pharmacy chain, or a public-health authority deployment                                            |
| Legal entity             | Contracting, licensing and data-controller identity. Mapped explicitly to hospitals; common ownership does not merge legal responsibility                     |
| Administrative level     | Public-sector level above facilities (state, district, block). Supervises facilities; holds no patient records of its own                                     |
| Hospital (care facility) | Mandatory data-access boundary inside a tenant ([ADR-0008](../../adr/0008-multi-hospital-tenancy-and-sharing.md)). A PHC, CHC or district hospital is one too |
| Department               | Organizational unit with a head, staff, services and a cost centre: Cardiology, Nursing, Pharmacy, Pathology, Finance                                         |
| Sub-unit                 | A unit inside a department: ward, ICU, theatre suite, lab section, satellite pharmacy, OPD clinic, counter                                                    |
| Location                 | Physical place: campus, building, floor, room. A separate tree from departments; a ward is where the two meet                                                 |
| Product unit             | Product-specific scope: pharmacy and outlet (PharmaConnect), party and premise (SupplyConnect), base (EmergencyConnect)                                       |
| Staff record             | Workforce HR fact about a person employed or engaged by a hospital. Not a login                                                                               |
| Login identity           | Authenticated Zitadel subject. Not an employee record                                                                                                         |
| Staff link               | Reviewed binding between a staff record and a login identity                                                                                                  |
| Posting                  | Effective-dated fact that a staff member works in a department or sub-unit. Proposes access; does not grant it                                                |
| Permission               | Code-declared action `<module>.<resource>.<action>`, with a data class. The unit the server checks                                                            |
| Role template            | Named bundle of permissions with one authority kind, such as `pharmacy.dispensing_pharmacist`. Shipped by the platform or composed by a tenant                |
| Assignment               | Subject + role template + scope + validity + grantor + reason + approval. The only thing that confers access                                                  |
| Entitlement              | Product subscription of a tenant or hospital. Required, never sufficient                                                                                      |
| Credential               | Evidence of professional registration (council registration, HPR). Proves qualification, not local permission                                                 |
| Privilege                | The hospital's reviewed grant to perform named clinical services. Required for clinical templates                                                             |
| Designation              | Statutory or accountable-person role held by a named individual: licence holder, pharmacist in charge, occupier, grievance officer                            |
| Managerial supervision   | A head or in-charge overseeing a team's work in their departments                                                                                             |
| Clinical supervision     | A qualified clinician overseeing a trainee's clinical acts, with countersignature where required                                                              |
| Delegation               | Bounded, time-limited transfer of a holder's assignment to a deputy                                                                                           |
| Break-glass              | Emergency, patient-scoped, time-limited access with reason, notification and retrospective review                                                             |
| Workflow definition      | Versioned, tenant-authored process built on a product's extension points and run by the platform                                                              |

## Current implementation

Verified against source on 2026-10-27. These are the gaps the target model closes.

- Role membership is tenant-wide. `iam_members` holds `tenant_id`, `subject` and `role_key` only: no scope, validity, grantor, reason or approval ([`iam/module.go:122`](../../../backend/internal/modules/iam/module.go)).
- `tenant_admin` receives every declared permission, clinical ones included, because `GrantsFor` appends it to each grant ([`platform/reconcile.go:92`](../../../backend/internal/platform/reconcile.go)). [ADR-0008](../../adr/0008-multi-hospital-tenancy-and-sharing.md) says privileged tenant administrators get no clinical access automatically; the code and the accepted decision disagree.
- The OpenFGA model includes versioned `scoped_assignment` and `scoped_permission` primitives alongside legacy tenant roles ([`authz/model.go`](../../../backend/pkg/authz/model.go)). IAM has reviewed hospital-scoped assignment proposals, membership generations, immutable snapshots, scope epochs, durable projection and a current-state authority provider. Workforce business routes have not opted into that authority; staff/link/object checks and target-bound RLS remain required. Department, outlet and base consumers are not implemented. Graph tuples alone are not application authority.
- System roles are a fixed catalogue ([`authz/authz.go`](../../../backend/pkg/authz/authz.go)). `staff_member` establishes tenant membership with no declared business permissions. IAM must grant membership explicitly; enrollment or linking does not assign it. Custom roles are representable but not creatable.
- Narrower scope lives in separate per-module grant tables, each with its own vocabulary:

  | Table                             | Levels or roles                                                                           | Source                                                     |
  | --------------------------------- | ----------------------------------------------------------------------------------------- | ---------------------------------------------------------- |
  | `workforce_hospital_access`       | reader, manager                                                                           | `backend/internal/modules/workforce/schema.sql:15`         |
  | `patient_hospital_access`         | reader, registrar                                                                         | `backend/internal/modules/patientmaster/hospital.sql:12`   |
  | `pharmacy_outlet_access`          | reader, editor                                                                            | `backend/internal/modules/pharmacy/catalogue.sql:32`       |
  | `emergency_base_access`           | reader, dispatcher, supervisor                                                            | `backend/internal/modules/emergency/schema.sql:21`         |
  | `supply_access_grants`            | 13 roles, optional premise, `valid_from`, `expires_at` (at most 366 days), reason, status | `backend/internal/modules/supply/access_scope.sql:3`       |
  | `supply_party_access`             | 8 roles                                                                                   | `backend/internal/modules/supply/parties.sql:22`           |
  | `workspace_country_policy_access` | reader, editor, reviewer                                                                  | `backend/internal/modules/workspace/country_policy.sql:75` |

  `supply_access_grants` is the most complete design and the closest to the target assignment record.

- `workforce_staff` records job category and employment status; workforce also owns reviewed hospital-local `workforce_login_links`, immutable private history and inactivation revocation. `workforce_staff_allowed()` still checks legacy `workforce_hospital_access`. Inactivation revokes current links but does not yet revoke IAM assignments or legacy grants ([`workforce/schema.sql`](../../../backend/internal/modules/workforce/schema.sql), [`login_links.sql`](../../../backend/internal/modules/workforce/login_links.sql)).
- No department entity exists. Issues #22, #287 and #616 each describe one; #429 depends on it.

## Open decisions

Agents must not settle these silently. Record the decision in an ADR before building on it. [ADR-0044](../../adr/0044-organization-structure-and-access-defaults.md) accepted ORG-D1, ORG-D3, ORG-D4 and ORG-D7 as recommended.

| ID     | Decision                                                                                                                   | Recommendation                                                                                                                                       |
| ------ | -------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| ORG-D1 | Which module owns the department and sub-unit master (#22, #287, #616 overlap)                                             | Accepted: A platform organization-structure owner. FacilityConnect owns physical locations; workforce owns postings; products reference IDs          |
| ORG-D2 | Tenant mapping for a public-health authority                                                                               | One tenant per state or authority deployment, with district and block administrative levels and one hospital boundary per facility                   |
| ORG-D3 | Whether the tenant owner may hold any clinical access by default                                                           | Accepted: No. Clinical access only through a separately assigned clinical template                                                                   |
| ORG-D4 | Whether a posting grants access automatically                                                                              | Accepted: No. It proposes; an authorized approver activates. Ending a posting revokes automatically                                                  |
| ORG-D5 | Who may publish a workflow that changes a clinical or medication process                                                   | A tenant clinical-governance approver in addition to the workflow publisher                                                                          |
| ORG-D6 | Whether tenants may define new permissions                                                                                 | No. Tenants compose templates from platform-declared permissions; new actions arrive with code                                                       |
| ORG-D7 | How a tenant with a single owner satisfies second-person approval for privileged grants, and how owner access is recovered | Accepted: Require two owners for multi-hospital tenants; single-owner grants notify every owner and enter review, and recovery uses verified support |

## Accepted Staff lifecycle migration policy

[ADR-0049](../../adr/0049-reviewed-employment-access.md) records the product owner’s
2026-10-29 decision: require reviewed Staff/employment links for hospital
operational access and review legacy grants before enforcement. Governance and
recovery remain separate. This is not ORG-D6 and does not close the open decisions
above or establish runtime migration/enforcement.

## Related issues

Access: #40 (RBAC), #417–#421 (accounts), #426–#434 (ABAC, care relationship, facility, department, temporary grants, break-glass, privileged access, approval workflows), #443 and #445 (audit), #798 (platform operator binding). Structure: #22, #287, #616. Workflows and configuration: #6, #455, #562, #563, #592, #706. Public sector: #243, #250, #489.

## Design references

Outside practice that informed the target, not requirements: Azure separates [privileged administrator roles](https://learn.microsoft.com/en-us/azure/role-based-access-control/built-in-roles/privileged) from job-function roles and recommends [job-function roles first](https://learn.microsoft.com/en-us/azure/cloud-adoption-framework/ready/azure-setup-guide/manage-access). Epic groups security points into security classes and user templates, with the role chosen by login department ([summary](https://pivotpointconsulting.com/insights/guides/how-to-determine-security-and-access-in-epics-hyperspace)). ERPNext binds workflow transitions to roles and restricts users to departments or branches through [user permissions](https://docs.frappe.io/erpnext/user/manual/en/role-based-permissions). OpenFGA documents [user-defined roles](https://openfga.dev/docs/modeling/custom-roles) and [intersection checks](https://openfga.dev/docs/modeling/multiple-restrictions). ICTHospital (GPLv3, reviewed at `e506dfb`) shows the patterns to avoid: one role per user, no expiry, and a blank branch meaning every branch.

## Agent skill

[hms-organization-access](../../../.agents/skills/hms-organization-access/SKILL.md) routes work to the right reference above. The matching `.claude/skills/` link exposes the same file.
