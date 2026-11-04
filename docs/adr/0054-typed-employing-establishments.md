# ADR-0054: Typed employing establishments for standalone pharmacy staff

Date: 2026-11-04. Status: proposed. It needs product-owner acceptance before
any migration, route, permission or event is built. It proposes ORG-D8 and
records ORG-D9 as open. ORG-D2, ORG-D5 and ORG-D6 stay open. Related: #19,
#418, #429, [ADR-0008](0008-multi-hospital-tenancy-and-sharing.md),
[ADR-0024](0024-pharmacy-ownership-and-outlet-tenancy.md),
[ADR-0044](0044-organization-structure-and-access-defaults.md),
[ADR-0051](0051-shared-department-owner-boundary.md) and
[ADR-0053](0053-reviewed-managerial-posting-supervision.md). Delivery detail is
in the [standalone pharmacy employer specification](../superpowers/specs/2026-11-04-standalone-pharmacy-employers.md).

## Context

ADR-0024 makes `tenant -> pharmacy organization -> outlet` the pharmacy
hierarchy. It also says a standalone pharmacy must never need a synthetic
hospital. Workforce is the only Staff owner, but its HR boundary is the
hospital. Source on 2026-11-04 shows how deep that goes:

- `workforce_hospitals` is the scope registry. 126 of the 132 Workforce tables
  carry `hospital_id` in their keys. RLS (`workforce_staff_allowed`) checks
  `app.workforce_hospital`.
- IAM `ScopedAssignment` admits only `hospital` and `staff` scope kinds, and
  both require `HospitalID`. The platform router's scoped helpers
  (`ScopedHospitalListRoute`, `GETScopedHospital`) are hospital-keyed.
- Every Workforce route family sits under `/workforce/hospitals/{hospital}`.
  Several wire contracts emit `hospital_id`.

An `independent` or `chain` pharmacy organization therefore has nowhere to
employ staff. The [reusable Staff foundation](../superpowers/specs/2026-11-02-reusable-staff-foundation.md)
and the [effective postings specification](../superpowers/specs/2026-11-02-effective-staff-postings.md)
both leave this decision pending.

In practice, a pharmacy business employs its staff centrally, with one HR
record, one leave policy and one payroll, and posts them to branches. Each
branch is separately licensed premises. Staff transfer between branches, and
relief pharmacists cover different branches over time. A branch manager runs
one branch, while area or regional managers oversee groups of branches. The
premises-level registered-pharmacist requirement is verified per licence under
IN-PH-06 and IN-PH-10. This ADR asserts no licence rule. A hospital-run
pharmacy works differently: the hospital employs its pharmacy staff and posts
them to its Pharmacy department.

Inherited **unmeasured** budgets apply: 50 peak requests/s, 80:20 reads to
writes, p99 below 500 ms, 99.9% availability and 500,000/1.5 million Staff at
12/36 months. The planning bound for one pharmacy establishment is also
unmeasured: 10,000 branches and 50,000 Staff. That fits the overall Staff
budget. Branch checks are `(tenant, establishment, outlet)` key probes, and
lists keep the existing 200-record page cap. No new service, cache or store is
needed.

## Decision

1. **The HR boundary is a typed employing establishment** of kind `hospital`
   or `pharmacy`. One Workforce Staff master serves both kinds. A `pharmacy`
   establishment is exactly one Pharma organization with ownership type
   `independent` or `chain`, and its ID is that organization's ID. Hospital
   establishments keep their hospital IDs, so no existing row changes meaning.
   A `hospital_owned` pharmacy organization is never an employer (decision 6).
2. **Pharma confirms employers and branches through owner guards.** Workforce
   never reads Pharma tables. Pharma exposes `pkg/authz` guards that follow
   ADR-0053's structural-gate pattern: owner code runs inside the caller's
   guarded tenant command and takes shared locks through the caller's commit.
   - **Employer guard.** Confirms that a same-tenant organization exists with
     an eligible ownership type.
   - **Branch guard.** Confirms that an outlet belongs to that organization.
   - **Why a one-time check suffices.** Pharma organization and outlet rows are
     immutable (`pharmacy_catalogue_immutable`). Ownership type and outlet
     membership therefore cannot drift after registration. A future outlet
     closure must extend the branch guard (see Failure).
   - **Permission.** Every registration command needs current
     `workforce.hospital.manage`. That permission is renamed only through a
     reviewed permission migration, never silently.
3. **Kind is immutable and checked at every boundary.**
   - **Registry.** `workforce_hospitals` gains an immutable `establishment_kind`
     column. It defaults to `hospital`, which is a metadata-only add for existing
     rows. A `pharmacy` row records the ownership type that the employer guard
     observed, and an update trigger refuses any kind change.
   - **Key column.** Existing tables keep the physical `hospital_id` column,
     documented as the establishment key (see Rejected).
   - **Routes.** `/workforce/hospitals/{hospital}/…` is unchanged and admits
     only `hospital` establishments. A pharmacy ID there returns 404, the same
     as an unknown ID. A new `/workforce/pharmacies/{pharmacy}/…` family admits
     only `pharmacy` establishments, and each capability is mounted by its own
     reviewed slice. Each family resolves a distinct Go scope type, so a
     hospital-only handler does not compile against a pharmacy scope.
   - **Database.** Hospital-only Workforce tables, such as those referencing
     Organization departments, get a write guard requiring
     `establishment_kind = 'hospital'`. A bypass of the route layer therefore
     still fails closed.
   - **Wire and events.** The pharmacy family emits `pharmacy_id`, never
     `hospital_id`. No existing event subject or payload carries a pharmacy
     establishment until a reviewed schema version includes the kind.
4. **The IAM scope root is typed.** `ScopedAssignment` moves from `HospitalID`
   to an establishment root (`kind`, `id`) through an additive migration.
   Existing assignments become `hospital`-rooted, with unchanged IDs and
   epochs. Each template declares the establishment kinds it may be assigned
   on, and clinical and hospital-only templates refuse `pharmacy`. A pharmacy
   ID is never passed as `HospitalID`. #429 adds `department` and `outlet`
   scope kinds under the same root. Pharma's `pharmacy_outlet_access` stays
   its operational outlet grant, and Workforce grants none of it.
5. **Branches are Pharma outlets referenced by postings.**
   - **Target.** A pharmacy establishment's posting target is one of its own
     outlets. The branch guard confirms it inside each posting command.
     Organization does not mirror outlets as departments.
     Hospital postings keep department and sub-unit targets. Each posting has
     exactly one target kind, and it matches its establishment kind.
   - **Lifecycle.** Postings still propose and never grant (ORG-D4).
     ADR-0053's proposal, independent review, terminal decisions and
     supervision apply. A non-root branch posting is supervised by a managerial
     posting at the same outlet. The root is an approved establishment
     management responsibility.
   - **Areas and regions.** Grouping branches into areas or regions is ORG-D9
     and stays open. Until then, an area manager holds establishment-root
     management, and no area-scoped authority exists.
6. **A hospital-run pharmacy stays hospital-employed.** Staff of a
   `hospital_owned` organization are hospital Staff, posted to the Pharmacy
   department or a sub-unit. How a department runs an outlet, and the duties
   that follow, belong to #418 and #429 and are not decided here.
7. **Each employer has its own employment record.** A person working for a
   hospital and a standalone pharmacy has a Staff record in each establishment,
   linked to one login only through existing reviewed login links.
   Cross-establishment postings, secondment and borrowing need a separate
   agreement design. Cross-tenant borrowing stays prohibited.
8. **Professional authority stays with Pharma.** Pharma owns registered
   pharmacist status, pharmacist-in-charge designation and assistant
   supervision at a branch. Each references the Staff and branch posting
   revisions and needs a verified licence and premises record (IN-PH-06,
   IN-PH-10). A branch posting, `job_category = pharmacist` or an outlet grant
   never confers dispensing authority.

## Failure, migration and rollback

**Failure behaviour.**

- An unknown, foreign-tenant or wrong-kind establishment or branch returns 404.
- A missing permission returns 403. Unavailable authorization returns 503.
- An unavailable Pharma guard returns 503, and nothing is inferred or cached.
- An audit or outbox failure rolls back the command.

**Outlet closure.** Pharma has no outlet closure today. Adding one must make
the branch guard refuse closed outlets. It must also publish an event that
ends dependent branch postings and designations at its effective instant.

**Migration.** Migration is expand-only:

- the registry kind column and observed ownership type;
- the IAM root columns, backfilled from `hospital_id`;
- the pharmacy route family, added slice by slice.

**Rollback.** Rollback stops mounting pharmacy routes and keeps the additive
schema and evidence. Each slice needs its own migration IDs, tests and approved
production rollout.

## Rejected

- **A synthetic hospital per pharmacy.** ADR-0024 forbids it. It would create
  hospital trees, hospital discovery entries and patient-boundary meaning for a
  shop.
- **A Pharma-owned Staff master.** That would be a second employee master,
  duplicating leave, payroll, credentials and documents.
- **The tenant as employer.** A tenant can own several hospitals and pharmacy
  businesses, so tenant-wide HR scope widens access.
- **Renaming `hospital_id` in all 126 tables now.** That rewrites hundreds of
  SQL functions and Go queries without changing behaviour. Contracts and
  guards, not the physical column name, decide what consumers see. The rename
  may return later as a separate mechanical refactor.
- **One neutral `/workforce/establishments/{id}` family.** It would break v1 or
  give one hospital two route names.
- **ADR-0051's asynchronous request and observation for employers and
  branches.** It adds a pending state and retry path but no safety. The owner
  rows are immutable, and the guard checks current state under a lock inside
  the same command.
- **Outlets mirrored as Organization nodes.** That creates a second outlet
  master that drifts from Pharma's licensed premises.
- **A `hospital_owned` pharmacy as its own employer.** It splits one
  hospital's staff across two HR boundaries and bypasses department
  supervision.
