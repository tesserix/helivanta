# Prescription register Implementation Plan

**Goal:** a read-only, audited register of every Schedule G, H and H1 supply on prescription (#1062).

**Spec:** `docs/superpowers/specs/2026-11-03-prescription-register-design.md`

**Decisions (product owner, 2026-11-03):** the serial is the invoice number and line. A Schedule G entry without addresses is shown as "not recorded"; the counter is unchanged.

### Task 1: Backend (`prescription_register.go`, test, `h1_register.go`, `catalogue.go`, archtest)

- [x] **Tests first:**
  - G, H and H1 entries listed, OTC excluded;
  - serial, addresses, manufacturer, batch and expiry, and returned units;
  - missing G addresses come back as `null`;
  - the class, product and patient filters, and invalid filters (400);
  - the owner is denied (403) and another tenant gets 404;
  - every successful read is audited;
  - the CSV shows `not recorded` and is audited; an oversized export is refused and records nothing.
- [x] **Implement:**
  - `GET /registers/prescriptions` (`platform.ListRoute`) and `/export`;
  - the shared `auditRegisterRead`, which the H1 register now uses too.
- [x] **Mutation checks:**
  - the class, product and patient predicates;
  - the OTC exclusion turned out to be redundant with the prescription join, so it was removed.

### Task 2: Shell (`prescription-register.tsx`, test, API helper, zone, page, workspace; counter endorsement)

- [x] **Tests first:**
  - the register's rows, including "not recorded";
  - the filters reach the URL;
  - narrowing to one patient and back;
  - the empty state and the period bound;
  - the counter's note names the register serial.
- [x] **Implement:** reuse `RegisterExport` from the H1 page.

### Task 3: Docs and gates

- [x] Domain doc, portfolio, and the superseded line in the counter-sale spec.
- [x] Run `contractdocs`, the access inventory, `lint-go`, turbo, and `-race`.
