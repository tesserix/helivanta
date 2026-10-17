# Staff income-tax declarations

Issue: #1041 slice 5b (parent epic #979). ADR: [0029](../../adr/0029-payroll-configuration.md). Builds on [income-tax claims](2026-10-19-income-tax-claims-design.md), which defined the claim heads and left the staff member's own declarations for this slice, and on [staff identifiers](2026-10-18-staff-identifiers-design.md), whose Transit encryption and two-person lifecycle this slice reuses.

## Problem

Each tax year a staff member gives their employer the claims they want TDS to account for: rent paid and to whom, interest on a home loan and to whom, and the amounts under each deduction. In India this is Form 124 (Income-tax Rules 2026, r.205; it replaced Form 12BB from 2026-27). The employer must keep these particulars, including the PAN of a landlord once annual rent crosses the threshold, and must act only on claims it has checked.

The landlord's and lender's PANs belong to third parties. A PAN identifies someone to the tax authority, so it gets the same protection as a staff member's own PAN: Transit encryption bound to its row, a masked form for display, and an audited reveal.

## Decision

- **One declaration per staff member per tax year**, changed only through proposals that a second person approves, with the lifecycle used by staff identifiers: pending, approved, rejected, withdrawn. One pending proposal at a time.
- **A proposal states the whole declaration.** It does not patch the previous one, so an approved revision is the complete set of particulars that payroll may use.
- **Landlord and lender PANs are encrypted in OpenBao Transit** with the tenant's `helivanta-staff-<tenant>` key. The context is `helivanta/workforce/tax-declaration/v1|<hospital>|<staff>|<tax year>|<role>|<position>`, so a ciphertext copied to another staff member, year, party role or position fails authentication. A proposal can `keep` the PAN in force at the same role and position, which is valid because the context names no revision.
- **Rejecting or withdrawing erases the proposal's PAN ciphertext** in the same statement. The masked form stays for history.
- **The landlord PAN threshold comes from the rules, never a constant.** It is `landlord_pan_rent_above_paise` on the `hra` head of the income-tax revision for the staff member's elected regime, in force on the decision date clamped to the tax year. With no election, no revision, or no `hra` head, the threshold is unknown and **every landlord needs a PAN** (fail closed: missing configuration demands more evidence, never less). The check runs on proposal and again on approval, so a rule approved in between is honoured.
- **Claim codes are not matched against rules here.** Declarations record what the staff member claims; the TDS projection (slice 6) applies the rules in force and reports an unknown code as `not_allowed`, so it never reduces tax.
- **Fail closed on the vault.** If OpenBao is unreachable or unconfigured, a proposal with a new PAN and every reveal return 503 `identifier_vault_unavailable`. Reading masked declarations still works.

## Scope of this slice

A declaration has:

- `claims`: 0–24 `{code, amount_paise}`, codes unique, in the salary-head code format, amounts 1 to ₹1,000 crore.
- `hra`: optional. `metro` (city class) and 1–4 landlords, each `name` (1–150), `address` (1–300), `rent_paise` (annual rent paid to them, 1 to ₹1 crore) and `pan`. Rent paid is the sum over landlords.
- `home_loan`: optional. `interest_paise` (1 to ₹1 crore) and 1–4 lenders, each `name`, `address` and an optional `pan`.
- `reason` for the proposal (1–500).

PAN shape for third parties is any holder type, `^[A-Z]{5}[0-9]{4}[A-Z]$`, upper-cased. A party PAN is `{action: keep|set|none, value?}`; `keep` copies the in-force ciphertext at the same role and position and fails with 409 `nothing_to_keep` when there is none. `none` is refused for a landlord when the rent total exceeds the threshold, or always when the threshold is unknown. A lender's PAN may be `none`; no rule makes it mandatory, and inventing one is out of bounds.

Not in this slice:

- **Evidence documents** (rent receipts, lender certificates, investment proofs). They need a new document record kind with its own scan lifecycle and access rules, and land in slice 5b-ii. Until then, approval means the approver checked the evidence offline; the decision note records it.
- Leave travel, perquisites, other income and previous-employer income, which need the payroll run (#1040).
- Employee self-service submission.
- The TDS projection that consumes approved declarations (slice 6).

## Permissions

| Action                               | Permission                             |
| ------------------------------------ | -------------------------------------- |
| Read masked declarations and history | `workforce.payroll.view`               |
| Propose or withdraw                  | `workforce.payroll.configure`          |
| Approve or reject                    | `workforce.payroll.approve`            |
| Reveal a party PAN                   | `workforce.payroll.identifiers.reveal` |

## Routes

Hospital scope: `…/workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/tax-declarations/{year}`. `{year}` is `YYYY-YY`, as for the regime election.

| Route             | Behaviour                                                                                                                                                                     |
| ----------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …`           | `current` and `pending`, masked; 404 when none                                                                                                                                |
| `PUT …`           | Proposal on `expected_version` (0 first). 409 `proposal_pending`, `staff_inactive`, `nothing_to_change`, `nothing_to_keep`. 400 `landlord_pan_required`                       |
| `POST …/decision` | `proposal_id`, `expected_version`, `decision`, `note` (required to reject). 403 for the proposer. Approval re-checks the landlord threshold                                   |
| `POST …/withdraw` | Proposer only                                                                                                                                                                 |
| `GET …/history`   | Every revision, masked, with its outcome                                                                                                                                      |
| `POST …/reveal`   | `revision_id`, `role` (`landlord`/`lender`), `position`, `reason` (1–200). In-force revision, or a pending one when the caller is not its proposer. `Cache-Control: no-store` |

Cross-hospital and cross-tenant requests get 404.

## Data

Migration `0028_workforce_tax_declarations`, append-only, forced RLS on every table with writes gated by `app.workforce_payroll`:

- `workforce_staff_tax_declarations`: one row per (staff, tax year) with `version` and the `revision_id` in force, the deferred foreign key and advance trigger used by staff identifiers.
- `workforce_staff_tax_declaration_revisions`: the proposal and its decision, `hra_metro`, `home_loan_interest_paise`, and the identifiers lifecycle constraints: separation of duties, proposer-only withdrawal, rejection note, one pending per (staff, year), active staff on insert, immutable content except the decision.
- `workforce_staff_tax_declaration_claims`: (`revision_id`, `position`, `code`, `amount_paise`), unique code.
- `workforce_staff_tax_declaration_parties`: (`revision_id`, `role`, `position`, `name`, `address`, `rent_paise`, `pan_ciphertext`, `pan_masked`). `rent_paise` is set exactly for landlords. Ciphertext and mask are null together while pending or approved; a rejected or withdrawn revision's ciphertext is null. Content is immutable except that erase.
- Lines are insertable only in the transaction that creates their revision, and only while it is pending.
- A deferred constraint trigger requires HRA landlords exactly when `hra_metro` is set, and lenders exactly when `home_loan_interest_paise` is set.

Events through the outbox with an audit row:

- `helivanta.in.workforce.tax_declaration_changed.v1`: hospital, staff, tax year, revision, status, version.
- `helivanta.in.workforce.tax_declaration_party_revealed.v1`: hospital, staff, tax year, revision, role, position.

Neither carries amounts, names, addresses or PANs.

## Tests (synthetic values only)

- Module, against real PostgreSQL and OpenBao:
  - the full lifecycle and separation of duties, history and audit rows, event payloads;
  - erase on reject and withdraw, read back from the database;
  - `keep` copies ciphertext at the same position and a ciphertext moved to another position fails integrity;
  - landlord threshold: below and above a synthetic rule threshold, unknown threshold, and a rule approved between proposal and approval;
  - reveal permission, reason, audit and event; proposer cannot reveal their own pending values;
  - 503 when the vault is down or unconfigured, while masked reads succeed;
  - cross-hospital and cross-tenant 404, inactive staff 409;
  - a database scan that no plaintext PAN appears in any column.
- Database guards: each constraint and trigger refuses a direct insert or update, each proven to fail without its guard.
- Shell: masked, pending, restricted, failed and reveal states, each party editor's inline errors, and the decision flow.
