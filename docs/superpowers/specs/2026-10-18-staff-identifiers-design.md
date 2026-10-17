# Staff bank and tax identifiers

Issue: #1041 slice 4b (parent epic #979); first field class of #30. ADR: [0029](../../adr/0029-payroll-configuration.md). Builds on [staff salary assignment](2026-10-17-staff-salary-assignment-design.md).

## Problem

Payroll needs each staff member's salary bank account, PAN (for TDS and Form 16) and UAN (for the PF ECR). These identifiers can redirect salary or impersonate someone with the tax authority. Forced RLS and disk encryption alone are not enough: a database dump, a mis-scoped query or a leaked backup would expose them in clear. The SOC 2 register lists field-level encryption as an open decision; this slice decides it for this class of field.

## Decision

- **Encryption happens in OpenBao Transit, not the app.** The API never holds key material. It sends a value with a context and gets ciphertext back.
- **One Transit key per tenant**, `helivanta-staff-<tenant uuid>`. The first encryption creates it as a derived, non-exportable, non-deletable `aes256-gcm96` key. Separate keys per tenant support #30's tenant separation. Crypto-shredding a tenant means an operator deletes its key; the app's policy cannot do that.
- **Each ciphertext is bound to its row.** The derivation context is `helivanta/workforce/staff-identifier/v1|<hospital>|<staff>|<field>`. Ciphertext copied onto another staff member, field or tenant fails authentication.
- **Masked values are stored beside the ciphertext**, so list and detail views never decrypt anything.
- **A PAN fingerprint detects duplicates without decrypting.** It is a Transit HMAC of the normalised PAN, pinned to key version 1 so key rotation does not break it. A unique constraint keeps a PAN on one staff member per hospital.
- **Changes need two people**, using the proposal and approval lifecycle of salary assignments. Separation of duties is a table constraint. A rejected or withdrawn proposal has its ciphertext erased in the same statement.
- **Seeing a full value takes a separate permission and a stated reason.** Every view writes an audit row and an identifier-only event, in the same transaction that authorises it.
- **Fail closed.** If OpenBao is unreachable, refuses, or the API is not configured for it, proposals that set a value and reveals return 503 `identifier_vault_unavailable`. Masked views keep working. Partial configuration stops the API at boot.
- **Runtime OpenBao access is a deliberate reversal** of the ESO-only rule written into `helivanta-network-policies` (tesserix-k8s #393). ESO still delivers every boot secret. Only `helivanta-api` reaches OpenBao, and only to encrypt, decrypt and HMAC under `transit/*/helivanta-staff-*`. It logs in through its own Kubernetes auth role, separate from the ESO role, so ESO's token never gains Transit and the app's token never gains KV.

## Scope of this slice

- **Bank account:** account number (9–18 digits), IFSC (`AAAA0XXXXXX`) and account holder name, set and cleared together. The account number is encrypted; IFSC and holder name are stored plainly because they are not secret.
- **PAN:** `AAAAAnnnnA` with `P` as the fourth character (an individual's PAN), normalised to upper case.
- **UAN:** 12 digits.
- Each field in a proposal is `keep`, `set` or `clear`. `keep` copies the in-force ciphertext, which is valid because the context does not name a revision.
- PAN and UAN shapes are redacted from logs alongside the existing Aadhaar and ABHA shapes; a UAN begins `10`, which the Aadhaar rule (leading 2–9) never matched.
- Shell: a "Bank and tax identifiers" section in the Staff pay panel covering masked values, proposing, deciding (with reveal of the proposed values) and reveal of in-force values.

Not in this slice:
- Aadhaar, ABHA and patient fields (#30 continues).
- Penny-drop or name-match verification of bank accounts.
- UPI payouts.
- Bulk import.
- Employee self-service changes.
- Re-keying the PAN fingerprint after an HMAC key change, which would be an operator job that decrypts and re-HMACs.

## Permissions

| Action | Permission | Hospital grant |
| --- | --- | --- |
| Read masked identifiers and history | `workforce.payroll.view` | reader/manager |
| Propose or withdraw | `workforce.payroll.configure` | locked manager |
| Approve or reject | `workforce.payroll.approve` | locked manager |
| Reveal a full value | `workforce.payroll.identifiers.reveal` | locked manager |

## Routes

Hospital scope: `…/workforce/hospitals/{hospital}/hr/payroll/staff/{staff}`.

| Route | Behaviour |
| --- | --- |
| `GET …/identifiers` | `current` and `pending`, masked only; 404 when none |
| `PUT …/identifiers` | Proposal on `expected_version` (0 first): `reason` and `bank`, `pan`, `uan`, each `{action, …}`. An identical pending proposal is not replayed, because ciphertext is randomised; a second proposal is 409 `proposal_pending`. 409 `staff_inactive`, `pan_in_use`, `nothing_to_change` |
| `POST …/identifiers/decision` | `proposal_id`, `expected_version`, `decision`, `note` (required to reject); 403 for the proposer; a rejection erases the proposal's ciphertext |
| `POST …/identifiers/withdraw` | Proposer only; erases the proposal's ciphertext |
| `GET …/identifiers/history` | Every revision, masked, with its outcome |
| `POST …/identifiers/reveal` | `revision_id`, `field` (`bank_account`, `pan`, `uan`), `reason` (1–200). Allowed for the revision in force, or for a pending one when the caller is not its proposer. `Cache-Control: no-store` |

Cross-hospital and cross-tenant requests get 404.

## Data

Migration `0025_workforce_staff_identifiers`:

- `workforce_staff_identifiers` holds one row per staff member: `version`, `revision_id` in force, and the current `pan_index`, unique per `(tenant, hospital)`.
- `workforce_staff_identifier_revisions` holds each proposal and decision, with the same constraints as salary-assignment revisions. Per field it stores ciphertext and the masked form (`bank_account_ciphertext`, `bank_account_masked`, `bank_ifsc`, `bank_holder_name`, `pan_ciphertext`, `pan_masked`, `pan_index`, `uan_ciphertext`, `uan_masked`).
  - Checks: ciphertext and masked form are null together; ciphertext has the `vault:v` prefix; masks match their shapes; and rejected or withdrawn revisions have null ciphertext and index.
  - Content is immutable once inserted, except for that erase.
- Forced RLS on both tables; writes are gated by `app.workforce_payroll`.
- `staff_identifiers_changed.v1` (staff, revision, status, version) and `staff_identifier_revealed.v1` (staff, revision, field) go through the outbox with the audit row.

## Infrastructure (tesserix-k8s)

- **OpenBao chart:**
  - The bootstrap mounts `transit/` idempotently.
  - New policy `helivanta-api-transit` grants `create`/`update` on `transit/encrypt/helivanta-staff-*`, and `update` on `transit/decrypt/helivanta-staff-*` and `transit/hmac/helivanta-staff-*`.
  - New Kubernetes auth role `helivanta-api-transit` is bound to `helivanta/helivanta-api`, TTL 1h.
- **`openbao-namespace`:** admit `helivanta-api` pods on 8200 and its SPIFFE principal.
- **`helivanta-network-policies`:** egress from `helivanta-api` pods to OpenBao on 8200. Rewrite the ESO-only rule to state the narrowed exception.
- **`helivanta-api`:** `OPENBAO_ADDR`, `OPENBAO_TRANSIT_MOUNT` and `OPENBAO_AUTH_ROLE`.
- Chart tests pin the policy paths, the role binding and the network admission.

## Tests

- **Transit client:** tested against a real OpenBao 2.6.2 container. Covers wrong-context, wrong-tenant and tampered ciphertext refused; permission denied; token expiry re-login; and an unreachable vault returning a typed unavailable error.
- **Module:** tested against real PostgreSQL and OpenBao. Covers:
  - the full lifecycle and separation of duties;
  - erase on reject and withdraw;
  - duplicate PAN;
  - `keep` copying ciphertext;
  - reveal permission and reason, with audit and event;
  - cross-hospital and cross-tenant 404;
  - 503 when the vault is down or unconfigured;
  - and a database check that no plaintext identifier appears in any column.
- **Guards:** every new guard is mutation-checked.
- **Page tests:** masked, pending, restricted, failed and reveal states, and inline errors for each field.
