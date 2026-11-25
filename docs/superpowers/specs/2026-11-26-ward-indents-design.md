# Ward indents: request, partial issue and return — design

Issue: #1682 (builds on #1386 counter sale, #1387 admission billing, #1416 verified affiliation,
ADR-0055 department-scoped assignments and the hospital stocktake authority of 2026-11-23)
Date: 2026-11-26

## Why

A hospital pharmacy's main daily work is issuing medicines to wards against an indent and
taking unused medicines back. PharmaConnect can bill an inpatient sale to the admission (#1387),
but the counter sale is the request there. There is no indent a ward raises, no partial issue,
no running balance and no return against the indent. SupplyConnect's store indents (#1007) are
the hospital store, not the pharmacy outlet.

## Decisions

### D1 Pharmacy owns the indent

A ward indent is a Pharmacy record against one outlet. Its lines name that outlet's catalogue
products, so issue and balance are computed against the same product the pharmacist dispenses.
It records:

- the requesting hospital and department (the authority), and the ward, room and bed codes of
  the stay (the location);
- for a patient indent, the occupancy, admission and opaque patient ids, re-resolved from
  Pharmacy's `pharmacy_bed_occupancies` projection when raised;
- the Staff actor reference (staff, link, version, generation, epoch) and IAM assignment that
  authorized the raise, as the stocktake evidence does;
- hospital and department names as raised, read through the owners' purpose readers (D3), so
  the pharmacist's queue shows who asked without a second master.

States: `open` → `closed` (fully issued, or closed short by the pharmacist with a reason) or
`cancelled` (by the requester, only while nothing is issued). Closed and cancelled indents are
immutable. Each line keeps requested, issued and returned units; outstanding is requested less
issued.

### D2 Two slices, one issue

1. **Patient indents (this slice).** An issue is a #1387 admitted sale. A return is a #1386 C-1
   credit note on that sale. Batch allocation, schedule and prescription citations, NPPA
   ceilings, H1 register rows, GST, the admission-account tender, `AdmissionChargePosted` and
   `AdmissionChargeReversed` are therefore the counter's, not a copy.
2. **Ward-stock indents (next slice).** No patient and no sale. It needs new movement kinds
   (`ward_issue`, `ward_return`) and a re-pin of the movement source seals, and is limited to
   outlets whose verified affiliation is `owner` (an internal transfer, not a supply). #1682
   closes only when both are deployed.

### D3 Who may raise and read

The requester is hospital Staff with a department-scoped assignment, as for hospital
stocktakes. Two templates in `contracts/staff-access.json`, catalogue
`department-pharmacy-ward-indents-v1`:

| Template                                       | Permissions                                                                        |
| ---------------------------------------------- | ---------------------------------------------------------------------------------- |
| `pharmacy.department_ward_indent_reader.v1`    | `pharmacy.indent.read`, `workforce.ward_indent_actor.use`                          |
| `pharmacy.department_ward_indent_requester.v1` | `pharmacy.indent.read`, `pharmacy.indent.raise`, `workforce.ward_indent_actor.use` |

`workforce.ward_indent_actor.use` is a separate Workforce actor purpose. Governance,
procurement and stocktake actor purposes do not authorize an indent, and this purpose
authorizes nothing else. No role default grants any of these; an owner role, hospital grant or
posting grants none.

Every requester route is under `/hospitals/:hospital/departments/:department/…` and does what
`withHospitalStocktakeActor` does, without the pharmacy-department binding:

1. compound scoped-command guard;
2. Organization's current path for the department; `AuthorizeScope` for the operation and the
   actor purpose, each grant department-scoped and inside the path;
3. hold the path through commit (`WithinCurrentDepartment`) and the current Staff actor;
4. in the Pharmacy transaction, a hospital/department frame admits only this hospital's indents
   (RLS, D5).

A hidden or changed department is `404`/`409 source_changed`; IAM, Organization or Workforce
unavailable is `503`. The caller's own scopes are listed by IAM at
`/me/hospital-ward-indent-scopes` (hospital and department names under the purpose), the same
shape as `/me/hospital-pharmacy-stocktake-scopes`. The stocktake purpose checks, name readers
and scope listing are generalised over a closed two-value purpose set rather than copied; the
stocktake behaviour and its tests are unchanged.

### D4 Which outlet

The target outlet's pharmacy must hold a verified affiliation with the hospital (the latest
`pharmacy_affiliation_verifications` entry is `accepted`). That is the rule an admitted sale
already enforces, so an indent can never be raised where it could not be issued.
`…/ward-indent-outlet-choices` lists those outlets with outlet and pharmacy names.

The requester searches the outlet's active catalogue through `…/outlets/:outlet/ward-indent-products`
(name, strength, form, pack and unit only; no price, stock or supplier data) under a purpose
catalogue scope modelled on `pharmacy_order_catalogue_scope`. The bed is found with
`…/outlets/:outlet/ward-indent-occupant?bed_code=` or `&room_code=`, answering the open stay's
ids and codes only; an empty bed is `404 no_occupant` and a shared room `409 ambiguous`, as at
the counter.

### D5 Tables and row security

Migration `0076_pharmacy_ward_indents` adds `pharmacy_ward_indents`, `pharmacy_ward_indent_lines`
and `pharmacy_ward_indent_issues` (indent, issue id, sale id, line quantities), each with forced
RLS and the `operating_choice_purpose`/`operating_choice_delete` policies every pharmacy table
carries. The migration verifies the operating-choice and stocktake seals first and re-pins them
afterwards, as `0070_pharmacy_stocktake_source_recovery` does.

A row is visible to:

- the outlet's pharmacist through `pharmacy_outlet_allowed` (read; write needs `editor`);
- the requester only through a transaction-local frame (`app.pharmacy_indent_hospital`,
  `app.pharmacy_indent_department`, `app.pharmacy_indent_subject`) set after D3 and restored
  before the transaction returns. It admits that hospital and department only.

Triggers enforce the invariants, whatever the handler does:

- lines are fixed at raise; the header changes only `open` → `closed`/`cancelled`, with version
  and actor checks;
- an issue row requires an indent that is `open`, a sale posted in the same transaction to the
  indent's occupancy with an `admission_account` tender, sale lines whose products are indent
  lines, and cumulative issued units per line no greater than requested;
- cancel is refused once anything is issued.

### D6 Issue

`PUT /pharmacies/:pharmacy/outlets/:outlet/ward-indents/:indent/issues/:issue`
(`pharmacy.sale.create`) carries `{lines: [{line_number, quantity_units, prescription?}],
total_minor}`. `total_minor` is the total the pharmacist saw from `/sales/quote` for the same
lines. It is the one `admission_account` tender, so a changed price is `409 price_changed`, never
billed unseen. The handler locks the indent, builds a `saleInput` with
`admission: {occupancy_id}`, and posts it through the same `postSale` the counter uses (a
refactor of `putSale` with no behaviour change). It then inserts the issue row in the same
transaction. A replayed issue id returns the first result; a different body is `409`.

An issue that leaves nothing outstanding closes the indent. `POST …/ward-indents/:indent/close`
(`pharmacy.sale.create`, reason 5-200 characters) closes it short, which the domain calls
"short supply flagged".

### D7 Return

A return is the existing credit note on an issue's sale. `PUT …/credit-notes/:id` refunds
through `admission_account` only, receives units into a quarantine location with a reason, and
publishes `AdmissionChargeReversed`. A release puts the units back on a shelf. The indent reads
returned units from those credit notes; it stores no second copy. The indent page opens the
existing return form for the issue's sale.

### D8 Events

`helivanta.in.pharmacy.ward_indent_raised.v1` `{indent_id, outlet_id, hospital_id,
department_id, kind, admission_id?, raised_at}` and `ward_indent_closed.v1` `{indent_id,
outlet_id, status, closed_at}`. No patient id, names or products leave Pharmacy in these
events. Issues and returns publish the sale and credit-note events they already publish.

### D9 UI

- **Ward (shell route `/pharmacy/ward-indents`, permission `pharmacy.indent.read`):** pick
  your scope and an outlet, find the bed, add products and quantities, raise. Your department's
  indents, with a running balance per line, and cancel while nothing is issued. The MediCore
  ward board links an occupied bed to it.
- **Pharmacist (`/pharmacy/indents`, permission `pharmacy.sale.read`):** the outlet's open
  indents, oldest first. Issue all or part (quote, then issue), close short with a reason, and
  record a return against an issue.
- Both are in `packages/ui/src/zones.ts` and classified in `contracts/release-capabilities.json`,
  created disabled.

## Hazard and acceptance note

| Hazard                                  | Prevention or detection                                                                                                                                                                |
| --------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Medicine billed to the wrong patient    | The stay is re-resolved at raise and again by the sale at issue; an ended or moved stay is refused. The pharmacist confirms the patient through the audited lookup, as at the counter. |
| Over-issue, or the same issue twice     | The cumulative line limit and the issue id are enforced by triggers and keys.                                                                                                          |
| A scheduled drug without a prescription | The sale's citation rules apply unchanged. Nothing in an indent bypasses them.                                                                                                         |
| Returned stock resold unchecked         | Returns land in quarantine; only a release returns them to a shelf.                                                                                                                    |
| A nurse reading another ward's indents  | The requester frame admits one hospital and department after IAM, Organization and Workforce checks; failure is `404`/`503`.                                                           |
| Missed short supply                     | Closing short needs a reason and is shown on the indent and the ward's list.                                                                                                           |

Accountable reviewer: the pharmacy owner who enables the capability. Synthetic adverse cases are
in "Tests that must exist". This slice adds no clinical rule: no dose, frequency, substitution or
schedule rule is introduced.

## Not covered by this slice

- Ward-stock indents (slice 2 above).
- Approval of an indent by a ward in-charge before the pharmacy sees it. The domain lists
  in-charge approval; it needs its own maker-checker template and is a follow-up issue.
- Substitution of another brand at issue. The pharmacist issues the requested product or closes
  the line short.
- Electronic prescribing from MediCore; citations are pharmacy-recorded prescriptions, as at the
  counter.
- Returns of stock that left hospital custody (they use the counter return).
- Indent MIS reports (fill rate, turnaround).

## Tests that must exist

- Authority: no assignment, a reader raising, another department's indent, a retired Staff
  link, a hospital-scoped grant without the actor purpose and the stocktake purpose all refuse.
  Organization, IAM or Workforce unavailable is `503`. Another tenant sees `404`.
- Outlet: an unverified, rejected or other-hospital affiliation refuses raise and choices.
- Raise: an empty bed, an ambiguous room, an ended stay, a discontinued product and a duplicate
  product refuse. A replayed id returns the first indent; a different body is `409`.
- Issue: partial issue twice reaches the requested units and closes the indent; a third issue
  over the balance is refused by the trigger even when the handler is bypassed. A stay ending
  between raise and issue refuses. A changed price is `409`. A replayed issue id posts one sale.
- Return: a credit note on an issue's sale lowers the line's returned balance, quarantines the
  units and publishes one `AdmissionChargeReversed`.
- Row security: the requester frame sees its department's indents only, and nothing outside
  the frame; the pharmacist sees only outlets with access.
- e2e: indent → partial issue → return, across a ward requester and a pharmacist.
