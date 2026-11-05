# Standalone pharmacy employers and branch postings

Refs [#19](https://github.com/tesserix/helivanta/issues/19). Implements the
accepted [ADR-0054](../../adr/0054-typed-employing-establishments.md). It
extends the [reusable Staff foundation](2026-11-02-reusable-staff-foundation.md),
[effective postings](2026-11-02-effective-staff-postings.md) and
[ADR-0053 supervision](../../adr/0053-reviewed-managerial-posting-supervision.md).

It uses ORG-AU-05, ORG-AC-01/02/04/05/08, ORG-ST-02/03, IN-PH-06 and
IN-PH-10. It applies ORG-D8 and leaves ORG-D9 open. Status: design accepted
with ADR-0054 on 2026-11-05; each slice still needs its own plan, migration IDs
and tests.

## Problem

An `independent` or `chain` pharmacy organization employs pharmacists,
assistants, branch managers and back-office staff. Workforce, the only Staff
owner, can employ people only in a hospital. The available choices are all
wrong:

- a fake hospital, which ADR-0024 forbids;
- a second Staff master inside Pharma;
- leaving standalone pharmacies without HR.

Real pharmacy businesses hire centrally and post staff to branches. Staff move
between branches, and a branch manager or area manager supervises them.

## Authority

These are the actors for a pharmacy establishment. Hospital actors are
unchanged.

| Actor                          | Authority kind        | Template                                                                           | Scope                      | Preconditions                                                                                                                       | Approver                                      |
| ------------------------------ | --------------------- | ---------------------------------------------------------------------------------- | -------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------- |
| Tenant owner or administrator  | Governance            | Existing tenant-administrator default holding `workforce.hospital.manage`          | Tenant                     | Current IAM membership                                                                                                              | ORG-D7 two-owner rule                         |
| HR administrator               | Administration        | Existing Staff templates, once they declare kind `pharmacy`                        | One pharmacy establishment | Independently approved assignment                                                                                                   | Independent IAM reviewer                      |
| Posting preparer and submitter | Administration        | Pharmacy-kind posting templates with branch choices in place of department choices | Pharmacy establishment     | Reviewed Staff actor link                                                                                                           | Independent IAM reviewer                      |
| Posting reviewer               | Supervision           | Pharmacy-kind `posting_reviewer`                                                   | Pharmacy establishment     | Not the preparer, submitter or subject                                                                                              | Independent IAM reviewer                      |
| Branch manager                 | Supervision           | No template. An approved managerial branch posting                                 | One outlet                 | Effective managerial posting at that outlet                                                                                         | Posting reviewer                              |
| Area or regional manager       | Supervision           | Establishment-root management responsibility until ORG-D9 is decided               | Pharmacy establishment     | Independently approved root responsibility                                                                                          | `workforce.posting.management.review` holder  |
| Registered pharmacist, PIC     | Clinical, designation | Pharma professional templates (#418)                                               | Outlet                     | Verified registration, an effective branch posting, a current outlet grant and a verified licence and premises record (IN-PH-06/10) | Pharma professional reviewer and IAM reviewer |
| Pharmacy assistant             | Operational           | Pharma assistant template (#418)                                                   | Outlet                     | An effective branch posting and a named eligible supervisor                                                                         | Independent IAM reviewer                      |

Leave keeps #1546's HR reporting line, which works unchanged inside a pharmacy
establishment. No posting, job category or outlet grant confers dispensing,
stock-release or HR authority.

## Objects and refusals

| Object                        | Owner                                              | Containing scope      | 404                                                      | 403                | 503                           |
| ----------------------------- | -------------------------------------------------- | --------------------- | -------------------------------------------------------- | ------------------ | ----------------------------- |
| Pharmacy establishment        | Workforce registry; organization facts from Pharma | Tenant                | Unknown, foreign-tenant, `hospital_owned` or wrong kind  | Missing permission | IAM, FGA or Pharma guard down |
| Staff and HR records          | Workforce                                          | Establishment         | Outside the caller's establishment or wrong route family | Missing permission | Authorization down            |
| Branch reference on a posting | Workforce reference to a Pharma outlet             | Establishment         | Outlet unknown or in another organization                | —                  | Pharma guard down             |
| Branch posting                | Workforce                                          | Establishment, outlet | Hidden posting                                           | Missing permission | Authorization or guard down   |
| Professional designation      | Pharma                                             | Outlet                | Outlet not granted                                       | Missing permission | Authorization down            |

A stale revision, closed source or ineligible state returns 409 with a stable
code. Duplicate retries return the retained result. A conflicting retry with
the same key returns 409.

## Design

**Registry.** An additive Workforce migration adds the following to
`workforce_hospitals`:

- `establishment_kind`, `hospital` by default, with `pharmacy` allowed;
- `pharmacy_ownership_type`, which is required exactly when the kind is
  `pharmacy`.

An update trigger refuses any change to either column.

`PUT /workforce/pharmacies/{pharmacy}` registers the organization. It runs the
Pharma employer guard inside the guarded command and commits the registry row,
audit and outbox together. An exact retry returns the retained row. A
`hospital_owned` organization or an unknown ID returns 404.

**Route families.** Each family is an establishment router whose admission
checks the registry kind after authorization: a wrong-kind ID answers 404 and an
unreadable registry answers 503. `pharmacyScope` reads only `{pharmacy}` and
returns a distinct Go type. The existing `scope(c)` keeps returning `uuid.UUID`
until slice 3 adds the distinct hospital type, when a handler first serves both
families.

- Handlers take the type they support, so a hospital-only handler cannot be
  mounted under the pharmacy family.
- The SQL session key stays `app.workforce_hospital`, so existing RLS works
  unchanged for both kinds.
- The platform router gets typed establishment variants of its scoped helpers.
  The hospital helpers remain.

**Hospital-only guard.** Tables that reference Organization departments or
hospital-only configuration get a `BEFORE INSERT OR UPDATE` guard. It refuses
rows whose establishment kind is not `hospital`.

The slice that adds the guard lists every such table it found. It proves each
listed table refuses a pharmacy establishment, and it proves existing hospital
writes still succeed.

**IAM root.** `ScopedAssignment` gains `EstablishmentKind` and `EstablishmentID`.

- An additive IAM migration backfills every existing assignment as `hospital`,
  with the same ID. Epochs and grants are unchanged.
- `HospitalID` stays populated for hospital roots and is never set for
  pharmacy roots.
- Each template declares `establishment_kinds`. The OpenFGA model and
  projection add the pharmacy root under the same tenant.
- Real-OpenFGA tests prove three things: a pharmacy-rooted assignment never
  satisfies a hospital check, and the reverse; revocation cuts over; an outage
  returns 503.

**Branch postings.** The posting owner gains a branch target, an outlet ID,
beside its department target. A database CHECK requires exactly one target and
that its kind matches the establishment kind.

This deviates from the wording of ORG-ST-02, which names a department or
sub-unit. It keeps that requirement's intent: the outlet ID comes from its
single owner, and no product department list is created. ORG-D8 records the
deviation. ORG-ST-03 is unchanged, so an outlet-scoped assignment never covers
a sibling outlet or the whole establishment.

- Each posting command runs the Pharma branch guard after the supervision lock
  and actor rows. That position matches the department gate in ADR-0053's lock
  order.
- Branch choices come from a Pharma-owned, bounded and paginated read with its
  own permission. Workforce does not list outlets itself.
- Supervision follows ADR-0053 with two adaptations. A non-root branch posting
  names an approved managerial posting at the same outlet. The root needs
  establishment-management responsibility.
- Self-supervision, cycles, interval gaps and cross-establishment supervisors
  are refused.

**Pharma consumer.** For a standalone organization, `/pharmacy/staff` reads the
pharmacy family with the caller's current Workforce scope. For a
`hospital_owned` organization, it keeps the hospital family. Neither reading
grants HR access from pharmacy membership.

## Delivery slices

Each slice is a vertical PR covering migration, API, SDK, UI, tests,
contractdocs and inventory. Each needs its own approved rollout. Migration IDs
are assigned when a slice starts, from the next free ID in each module.

1. **Establishment kind and registration.** Registry columns, immutability
   trigger, employer guard, the `PUT` and `GET` routes, the hospital-only write
   guard, establishment admission, the typed pharmacy scope, and UI to register a standalone organization
   as an employer. Wrong-kind 404 on both families, `hospital_owned` refusal
   and guard outage are tested.
2. **Typed IAM root.** Assignment root columns, backfill proof against a
   production-shaped fixture, template kind declarations, the OpenFGA model
   and projection, and kind-crossing denial tests.
3. **Core Staff in a pharmacy establishment.** The distinct hospital scope
   type, then directory, create and edit,
   access and identity on the pharmacy family, with `pharmacy_id` contract
   versions. The Pharma Staff screen consumes the family.
4. **HR lifecycle.** Enrollment, employment periods and reviews, credentials,
   documents and leave, including reporting-line routing. Each surface is
   mounted only after its own handler review. Payroll, duty rosters and
   overtime are listed separately and stay unmounted until reviewed.
5. **Branch postings.** Branch guard, branch choices permission, pharmacy-kind
   posting templates, branch targets, supervision adaptation and the posting
   UI with a branch picker.
6. **Pharma professional authority at branches.** This is #418's remaining
   pharmacist, PIC and assistant work. It consumes the branch posting and runs
   in parallel with the hospital pharmacy path. It is slice 4 of the
   [pharmacist outlet authority specification](2026-11-05-pharmacist-outlet-authority.md).

## Required tests

- **Registry and scope:**
  - wrong-kind and foreign-tenant IDs return 404 on both families;
  - a `hospital_owned` organization is refused;
  - an exact retry returns the retained row;
  - an attempted kind change is refused;
  - a direct SQL write to a hospital-only table under a pharmacy establishment
    is refused.
- **IAM:**
  - existing assignments keep their epochs and decisions after backfill;
  - a pharmacy-rooted grant fails hospital checks, and the reverse;
  - a template that declares only `hospital` cannot be assigned on a pharmacy;
  - real OpenFGA covers revocation and outage.
- **Postings:**
  - an outlet from another organization is refused;
  - a stale branch choice is refused;
  - supervisor at a different outlet, self-supervision, cycles, interval gaps,
    concurrent approvals and retained-command recovery are each covered;
  - no outlet grant follows from approval.
- **Lifecycle:**
  - exit, suspension and credential expiry end dependent authority on the
    effective date (ORG-D4);
  - a person employed by a hospital and a pharmacy keeps two records, and
    neither grants access to the other.
- **Browser:** real HR administrator, preparer, reviewer, branch manager and
  pharmacist sessions run through the Global API on the local stack.

Each new assertion must be shown failing before the code that satisfies it.

## Numbers and failure behaviour

The planning figures are unmeasured, as in ADR-0054. Staff budgets are
inherited, and one establishment is bounded at 10,000 branches and 50,000
Staff. Branch checks are key probes, and lists keep 200-record pages. No new
service, cache or store is added.

- When the Pharma guard is down, registration and posting commands return 503. Reads that do not need the guard are unaffected.
- When IAM or FGA is down, the request returns 503.
- When an audit or outbox write fails, the command rolls back.

## Not covered

- Branch grouping into areas or regions (ORG-D9).
- Cross-establishment secondment or borrowing.
- Payroll statutory rules for pharmacy employers.
- Outlet closure, which Pharma has not built.
- Hospital pharmacy department-to-outlet links and pharmacist duties (#418,
  #429).
- Renaming the physical `hospital_id` columns.
- Cross-tenant affiliation.

No licence, council or schedule rule is asserted here. Pharma verifies those
against the source register before activation.
