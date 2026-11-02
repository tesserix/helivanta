# Pharma Staff specialization and audit scope

Status: implementation plan and source audit, 2026-11-02. Not a claim of current
professional verification, regulatory sign-off or new operating permission.

## Ownership and current evidence

- Organization owns the reusable hospital department/sub-unit tree (ADR-0051).
- Workforce owns canonical Staff identity, employment and postings. Its profile
  department vocabulary still needs explicit reconciliation with the tree.
- IAM owns user identity and explicit scoped capability assignments. Linking a
  user to Staff does not provision a pharmacy grant.
- Pharma owns outlets/premises, professional acceptance, product duty assignments
  and the operational command evidence. It references Staff/posting revisions;
  it must not create another employee/person master or write Workforce tables.
- Evidence owner releases protected files after current authorization, scan and
  digest/generation checks. A file upload is not verification.

Source reviewed: `backend/internal/modules/pharmacy/module.go`, `licences.go`,
`licences.sql`, `catalogue.sql`, `release.sql`, `prescriptions.go`,
`prescription_image_upload.go`, `h1_register.go`; registered reference
`docs/api/domains/pharmacy.md`; stories #417 and #418. Module roles currently
include pharmacist, pharmacy owner, pharmacy purchasing and pharmacy inventory.
The licence input currently stores pharmacist name/registration as licence
snapshot text; it is not a canonical Staff/professional-profile link. Preserve
legacy licence evidence and add an explicit reviewed reconciliation path; do not
silently reinterpret those strings as verified Staff identity.

The read-only Pharma Staff source has local browser acceptance. Professional
profiles, assistant supervision, PIC duties and staff-specific audit workspace
have not been implemented or accepted. Existing sale/stock/prescription controls
must retain their current independent guards while these are developed.

## Roles and capabilities to implement and test

These are duty/capability profiles, not automatic grants or an exhaustive fixed
list of hospital job titles. Department configuration can add profiles using the
same explicit permission model; names never imply authority.

| Profile | Intended responsibility | Separation and scope |
| --- | --- | --- |
| Pharmacy owner | Outlet governance, commercial setup, financial and licence administration | Current owner role does not read prescriptions or sign clinical supply; preserve that boundary. |
| Registered pharmacist | Product-owned dispensing and professional decisions | Require current accepted registration, active eligible employment/posting, current outlet grant and command-specific permission. Registration alone grants none of these. |
| Pharmacist in charge | Reviewed effective duty designation for an outlet/premise, with cover/replacement and gaps visible | A designation is separate from role membership, licence validity and current operating eligibility; never grant all permissions by title. |
| Pharmacy assistant | Attributed preparation and support under a named eligible supervisor | No pharmacist impersonation, independent dispensing finalization, stock safety release or H1 disclosure by default. Preparation versus signing needs distinct commands and evidence. |
| Inventory / purchasing | Existing scoped stock and procurement responsibilities | Preserve distinct permissions; stock receipt or payment authority does not confer clinical signing or professional verification. |
| Staff coordinator / manager | Prepare product profile, propose posting/duty changes, maintain authorized metadata | Related-role editing requires current explicit scope and expected version; no self-approval or privilege escalation through profile edits. |
| Professional reviewer | Review protected professional registration/evidence against named issuer/jurisdiction and applicable policy | Independent from preparer and subject. Approval records verification, not an IAM grant or an unbounded legal entitlement. |
| Access reviewer | Decide explicit scoped login/capability requests | Independent IAM approval and reviewed Staff/login link; revocation and denied/expired review fail closed. |
| Compliance / Staff auditor | Bounded Staff/duty/verification/access audit review and approved export | Separate from patient/prescriber disclosure; no management, signing or review capability follows from audit access. |

Do not add broad default clinical grants to tenant administrators, owners or new
assistant templates. Privileges must be independently effective and revocable at
exact tenant/hospital/department/outlet scope. Existing defaults are source
facts, not proof of complete professional eligibility enforcement.

## Audit coverage and implementation obligations

| Surface | Existing source evidence | Staff-specific addition / acceptance still required |
| --- | --- | --- |
| Canonical profile and department references | Shared Staff owner reads/writes; new organization revisions have atomic audit/outbox | Preserve correlation to exact Staff, employment, department and posting revisions; audit sensitive reads as well as changes. |
| Outlet membership and permission changes | `pharmacy_outlet_access_audit` triggers in `catalogue.sql` | Retained request/independent decision/revocation evidence, actor and reason, reviewed login link, capability and effective scope; never audit secret/token values. |
| Professional profile and registration | Licence snapshot text exists in `licences.go` | Protected evidence upload/finalize/read/download, issuer/source review, accept/reject/correct, expiry/renewal/revoke and duplicate/conflicting-command evidence. |
| PIC, cover and assistant supervision | No accepted Staff-specific duty implementation | Effective start/end, eligible supervisor, incompatible/overlapping duties, handover/cover and independent changes; retain history after resignation/removal. |
| Outlet licence / schedule review | Existing four-eyes licence review and schedule review tables/triggers | Correlate future Staff/PIC references without rewriting historical text or granting authority through licence review. |
| Stock receipt/correction/quarantine/release | Existing stock and release audit triggers | Attribute future Staff/posting/eligibility evidence at the command boundary and preserve product-specific release permission. |
| Prescription evidence and H1 disclosure | Protected image-read audit; H1 page/export audit with period, digest/count for export | Current reader and outlet scope, purpose and privacy controls; audit access must not disclose patient detail to owner or Staff auditor. |
| Sales, refunds and physical actions | Existing immutable sale/credit-note/payment records and audit triggers | Keep operator versus preparer/reviewer identity; same durable command cannot post twice; do not erase physical/completed actions on staff exit. |
| Suspension, disabled login, expiry and exit | Workforce lifecycle/operational inventory remains incomplete | Attempted command denial, revocation projection/retry and recovery must be attributable without leaking private rejection reasons into public logs. |
| Audit review/export itself | No accepted Pharma Staff audit workspace | Exact scope/period/action filters, bounded pages, current permission at read/release, export manifest/digest, disclosure audit and explicit unavailable/stale results. |

Audit records retain actor, tenant, narrower scope, opaque target, action/outcome,
correlation/command/revision, effective/recorded time and minimal changed-field
names. Protected reasons and registration/document content remain with their
owner; never duplicate them into ordinary logs, broadcasts or events. Audit/state/
outbox failure must roll back the command or withhold the sensitive read/export.
Authorization denials need a separate bounded security-event path because the
refused business transaction does not commit. Define it explicitly; do not claim
failed-transaction row triggers already provide refusal evidence.

## Delivery and acceptance sequence

1. Organization UI/local owner routing now has local positive acceptance. Reconcile HR department
   references and deliver canonical Workforce posting commands and read projection.
2. Add Pharma professional profiles referencing canonical Staff, scoped protected
   evidence and independent review. Preserve historical licence text separately.
3. Add effective outlet postings, PIC/cover and assistant supervision, with immutable
   receipts, stale-state protection and independently approved capability requests.
4. Integrate current employment, disabled-account, professional expiry/suspension,
   posting and outlet guards into each actual operational surface. Use owner
   contracts and fail closed when current eligibility cannot be established; event
   delivery alone is not immediate revocation proof. Demonstrate stale projection,
   retained sessions, concurrent cutoff and replay recovery before activation.
5. Build Staff audit workspace/export with its own least-privilege permission and
   disclosure controls. Test real manager, independent reviewer, pharmacist,
   assistant, owner, inventory/purchasing and auditor sessions through Global API.

For every lifecycle, test valid path, required fields, duplicate/changed retries,
conflicting concurrent edits, missing/foreign scope, self-review, expired/revoked
credentials, disabled/terminated/resigned Staff, dependency failure, rollback,
projection lag and current-scope file release. Include contractor expiry/renewal,
transfers, supervisor replacement, safe removal and rehire with retained history.
No issue is complete from a role label, green schema validator or source browser.

Regulated decisions use IN-PH-02/04/06/10 and the India source register. Record
state/UT, service/premise, issuer, authoritative source/version, validity and
qualified accountable review before activating consequential policy. No new
nationwide register, retention, council or licence rule is asserted here.

## OpenFGA boundary and proof

The current API builds `authz.NewClient`, runs platform reconciliation and passes
IAM's `NewCurrentAuthorization` into the server chain. OpenFGA's persisted model
currently provides `user`, `membership`, `role`, `perm`, `tenant`, `assignment`
and `scoped_permission` types. Role names, permissions, assignments and scope are
data; a department metadata row is not an OpenFGA permission relationship.

A real OpenFGA integration test now exercises the new organization routes with
actual tuples: pharmacist is denied, manager is allowed, manager revocation on the
same authenticated identity blocks writes while a separate reader relation still
permits reads, and a simulated upstream outage returns 503 for both reads and
writes. It uses the cached OpenFGA image, real Postgres and production middleware;
authentication identity is the explicit harness verifier. The focused test uses
the real FGA client and shared server permission/membership middleware; it does
not replace separate IAM database reconciliation tests. No static
permission resolver or always-member stub is used in that test.

Future Workforce postings and Pharma capabilities must use the existing explicit
scoped assignment/projection and current-authorization contracts, with database
owners retaining eligibility, exact tenant/resource scope and lifecycle guards.
Define any additional relation/model change canonically, test it with real OpenFGA,
retain projection/reconciliation migrations and acceptance evidence, and do not
activate a new operational permission before revocation, stale-projection,
cross-outlet and dependency-failure tests pass. Existing SQL outlet grants remain
part of current product guards; this work does not claim that the department tree
has already migrated every outlet scope or added inheritance to the FGA model.
