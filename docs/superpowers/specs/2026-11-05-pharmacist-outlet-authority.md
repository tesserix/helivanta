# Pharmacist authority at pharmacy outlets

Date: 2026-11-05. Status: design for product-owner review. Decisions D1–D7 are
proposed, not accepted. No migration, route, permission, template or event exists
yet. Resolves the design part of #418. Related: #19, #39, #169, #429, #1374,
[ADR-0024](../../adr/0024-pharmacy-ownership-and-outlet-tenancy.md),
[ADR-0043](../../adr/0043-tenant-practitioner-ownership.md),
[ADR-0054](../../adr/0054-typed-employing-establishments.md) decisions 4, 5 and 8,
and [ADR-0055](../../adr/0055-department-scoped-assignments.md). It is slice 4 of
the [department-scoped assignments specification](2026-11-04-department-scoped-assignments.md)
and slice 6 of the [standalone pharmacy employer specification](2026-11-04-standalone-pharmacy-employers.md).

## Problem

A drug inspector asks two things about a Schedule H1 supply: who released it, and
was that person's pharmacy council registration current that day. Helivanta cannot
answer either. Source on `01d4a284` shows why:

- **The pharmacist role is tenant-wide.** `authz.RolePharmacist` holds stock
  release, sale, prescription record and read, dispense fulfil and return
  (`pharmacy/module.go:196–207`). Pharma narrows it per outlet only through
  `pharmacy_outlet_access`, a `reader`/`editor` row that a `pharmacy.access.manage`
  holder upserts with no review or epoch (`pharmacy/catalogue.go:255`).
- **No registration is checked.** The H1 and prescription registers name
  `sales.actor_subject` as the pharmacist (`h1_register.go:66`,
  `prescription_register.go:72`). That is a login subject, not a registered
  person.
- **Licences hold historical text.** An outlet licence records a pharmacist name
  and registration number as licence text with one approve/reject review
  (`licences.sql:8,40`). It describes the licence, not a person's standing today.
- **Workforce holds HR facts.** `job_category = 'pharmacist'` and HR-reported
  enrollment credentials with independent credential reviews
  (`credential_validity.sql`) are employment evidence, not council verification.
- **The registration owner is not running.** Practitioner owns registration
  identity and verification (ADR-0043), with tenant-configured issuer policies and
  attestations. It is absent from `internal/bootstrap/modules.go`, so it is
  unregistered and undeployed (#39). Practitioner records have no link to a Staff
  member or login (#1374).

So anyone holding the pharmacist role and an editor outlet grant can release H1
stock today, and the register names their login.

## Authority

| Actor                         | Authority kind | Template                                      | Scope                 | Preconditions                                                                                                                                                                                  | Approver                                                    |
| ----------------------------- | -------------- | --------------------------------------------- | --------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------- |
| Registered pharmacist         | Professional   | `pharmacy.outlet_pharmacist.v1`               | Outlet                | Current verified registration (Practitioner), reviewed practitioner-to-Staff link (#1374), an effective posting serving the outlet, a current editor outlet grant, an approved current licence | Pharma professional reviewer, then independent IAM reviewer |
| Pharmacist in charge (PIC)    | Designation    | `pharmacy.outlet_pharmacist_in_charge.v1`     | Outlet                | Everything a registered pharmacist needs at the same outlet. At most one active PIC per outlet                                                                                                 | Pharma professional reviewer, then independent IAM reviewer |
| Pharmacy assistant            | Operational    | `pharmacy.outlet_assistant.v1`                | Outlet                | An effective posting serving the outlet, a current outlet grant and a named supervisor who holds a current pharmacist assignment at that outlet                                                | Independent IAM reviewer                                    |
| Pharma professional reviewer  | Administration | New permission `pharmacy.professional.review` | Pharmacy organization | Not the proposer or the subject                                                                                                                                                                | Existing IAM assignment review                              |
| Tenant owner or administrator | Governance     | Existing                                      | Tenant                | No implicit professional authority. Cannot self-approve                                                                                                                                        | ORG-D7 two-owner rule                                       |

An effective posting serving the outlet means a hospital Pharmacy department
posting whose department has a reviewed outlet binding (migration 0034), or a
pharmacy branch posting at that outlet once #19 slice 5 exists.

## Objects and refusals

| Object                         | Owner                   | Containing scope | 404                                         | 403                | 409                                              | 503                                   |
| ------------------------------ | ----------------------- | ---------------- | ------------------------------------------- | ------------------ | ------------------------------------------------ | ------------------------------------- |
| Outlet professional assignment | IAM, verified by Pharma | Outlet           | Unknown, foreign-tenant or ungranted outlet | Missing permission | Stale epoch, second PIC, ineligible precondition | IAM, FGA, Pharma or Practitioner down |
| Registration standing          | Practitioner            | Practitioner     | Unknown or unlinked practitioner            | Missing permission | Stale registration revision                      | Practitioner down                     |
| Regulated release              | Pharma                  | Outlet           | Ungranted outlet                            | Missing permission | `pharmacist_authority_required`, with the reason | Any owner guard down                  |
| Register entry                 | Pharma                  | Outlet           | Ungranted outlet                            | Missing permission | —                                                | Authorization down                    |

## Proposed decisions

1. **D1. Professional authority is an IAM outlet assignment.** It follows the
   ADR-0055 department pattern under the typed root of ADR-0054 decision 4: scope
   kind `outlet`, with Pharma as the verifying owner. Pharma confirms, through a
   `pkg/authz` guard inside the IAM command, that the outlet exists in the tenant
   and belongs to the named pharmacy organization. Pharma adds no private grant
   table. `pharmacy_outlet_access` stays the operational outlet grant and is a
   precondition, never a substitute.
2. **D2. Authority is derived at the command, never stored as a flag.** The
   regulated-release guard checks, under shared locks through commit:
   - the outlet assignment is current, with its epoch;
   - Practitioner reports a verified registration for the linked practitioner on
     the command's date in the issuer policy's calendar;
   - the actor's posting serving the outlet is effective;
   - the outlet's licence is approved and inside its validity window.

   Any missing fact denies with 409 and a reason. Any unavailable owner returns 503. Suspension, expiry or revocation therefore takes effect at the next
   command, with no grace period and no reliance on a token claim.

3. **D3. Practitioner is the only registration source.** Workforce credential
   reviews stay HR evidence and never become a second registration master. The
   link from practitioner to Staff and login is #1374's reviewed link. This spec
   does not design it.
4. **D4. One PIC per outlet, enforced in the database.** A partial unique index
   over current PIC assignments per outlet makes a second concurrent PIC fail with 409. A PIC change is one reviewed handover command that ends the old
   designation and starts the new one atomically. An outlet left without a PIC
   publishes `helivanta.in.pharmacy.outlet_pic_vacated.v1`, which carries only
   identifiers.
5. **D5. Which schedules need a pharmacist is tenant policy, never a code
   default.** Pharma reads a versioned, reviewed per-outlet policy naming the
   schedule classes (`H`, `H1`, `X`) whose release requires D2 authority. Each
   edition carries a review reference to qualified pharmacy review, like the
   Practitioner issuer policy. No policy ships by default. Whether an outlet with
   no active policy keeps today's behaviour or refuses regulated release is
   question Q1.
6. **D6. OpenFGA tuples are a projection, not the decision.** Pharma writes
   `verifier` and `in_charge` relations on `pharmacy_outlet` for lists and UI, and
   deletes them on revocation or expiry. Release decides from D2's locked owner
   reads. A daily expiry job warns at 60, 30 and 7 days and publishes coverage
   loss. It is not the enforcement point.
7. **D7. Registers name the authority, not only the login.** New H1 and
   prescription register rows store the assignment ID and epoch plus the
   practitioner registration revision used. Existing rows keep `actor_subject`
   and are shown as "registration not recorded". No lineage is invented for them.

## Questions for the product owner

- **Q1. Outlets with no active schedule policy.** Recommended: keep today's
  behaviour, mark their register rows "registration not recorded", and show the
  outlet as "pharmacist authority not configured" in Pharmacy setup. Refusing all
  regulated release in production on activation day would stop every outlet
  before any tenant could configure a policy. Activation is then per outlet and
  reviewed.
- **Q2. Locum and relief cover.** Recommended: a relief pharmacist holds a
  time-bounded `pharmacy.outlet_pharmacist.v1` assignment at each outlet they
  cover. No separate cover type until a tenant needs one.

## Not decided here

Pharmacy council registration rules, which schedules legally need a registered
pharmacist present, and PIC obligations differ by state and premises licence. The
India [source register](../../domain/india/sources.md) has no primary source for
them yet. Adding one, with qualified review, is an activation gate for D5
(IN-PH-06, IN-PH-10). This spec asserts no legal rule.

Also out of scope: the prescription verification workflow itself (#169),
prescriber verification, NDPS custodians, automated council lookups, passkeys and
terminals (#422–#424), and branch grouping (ORG-D9).

## Numbers

The inherited unmeasured budget applies: 50 peak requests/s, 80:20 reads to
writes, p99 below 500 ms and 99.9% availability. #418 asks for an authority read
at p95 below 120 ms on the dispense path. D2 is four indexed key probes inside the
release transaction, each keyed by tenant and outlet or assignment, so it adds no
round trip to another service. Planning bound: 10,000 outlets and 50,000 Staff per
establishment. Assignment lists keep the 50-record page cap. No new service, cache
or store is needed.

## Delivery slices

Each slice is a vertical PR with migration, API, SDK, UI, tests, contractdocs and
inventory, and its own approved rollout.

0. **Prerequisites, tracked elsewhere.** #39 registers and deploys Practitioner
   with a reviewed pharmacy council issuer policy. #1374 links a practitioner to a
   Staff login. Neither is started here. #1374 is assigned to another maintainer.
1. **Outlet scope kind and professional templates.** The IAM `outlet` scope kind,
   the Pharma outlet guard, the three templates, `pharmacy.professional.review`,
   proposal verification by Pharma and Practitioner, the PIC unique index and
   handover, the OpenFGA projection, and Pharmacy setup UI to propose and review.
   Release behaviour does not change in this slice.
2. **Schedule policy and the release gate.** D5's policy, D2's guard on stock
   release and sale for regulated lines, the 409 reasons, and D7's register
   columns. This is the first slice that changes dispensing, so it is activated
   per outlet.
3. **Assistants and coverage.** Assistant supervision checks, coverage reads,
   expiry warnings and the coverage-loss events.
4. **Standalone branches.** The same templates on pharmacy establishments,
   consuming #19 slice 5 branch postings.

## Required tests

- **Real Postgres, OpenFGA and the Global API.**
  - A pharmacist assigned at outlet A releases an H1 line at A and is refused at
    sibling outlet B with 404.
  - Expiry at midnight in the issuer calendar refuses the next release with 409
    `pharmacist_authority_required`, reason `registration_expired`, while stock
    receiving still works.
  - Suspension refuses the next release with no wait.
  - Two concurrent PIC proposals for one outlet produce one PIC and one 409.
  - An assistant is refused H1 and X release in every state.
  - A foreign tenant gets 404 for every object.
- **Owner outages.** IAM, OpenFGA, Pharma and Practitioner each return 503, and
  nothing is cached or inferred.
- **Rollback guard.** Removing the outlet-scope RLS policy makes the projection
  test fail.
- **Registers.** A new H1 row stores the assignment, epoch and registration
  revision. A legacy row shows "registration not recorded".
- **Browser, through the Global API.** A manager proposes a pharmacist at an
  outlet, a professional reviewer and an IAM reviewer approve, the pharmacist
  releases an H1 line, the registration is suspended, and the next release is
  refused with the reason shown.

## Rollback

Migrations are expand-only. Rollback stops mounting the routes and the release
gate and keeps assignments, policies and register columns as evidence. Ending the
gate restores today's behaviour and is recorded per outlet.
