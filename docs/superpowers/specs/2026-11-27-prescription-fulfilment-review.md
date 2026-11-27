# Proposed prescription fulfilment for clinical review

This is the next source-binding component of #169, following ADR-0077 and the
complete pharmacist-verification design. It does not approve supply. The trusted
verified-person and qualified clinical-authority source remains unresolved.

PharmaConnect owns recorded prescriptions, their remaining supply allowance and
this read-only projection. A reader with `pharmacy.prescription.read` and current
outlet access may propose a positive quantity for named recorded lines. Global
API remains the public entry point. Account/tenant preconditions constrain the
verified principal and never grant authority.

The server locks the exact prescription parent in the same scope used by active
sale constraints, reloads remaining quantities/repeats and checks the caller's
source-state precondition. Empty, duplicate, foreign, exhausted and overfilled
line selections are rejected. Product IDs come from the owner record, never from
a caller assertion. Input order is canonicalized without mutating caller data.

The response contains the checked source, normalized selected lines and a
versioned SHA-256 fulfilment identity. It is distinct from a clinical
`reviewed_state_hash`: policy, qualified actor, signed clinical source and clinical
context are still required before approval. Reader identity does not alter the
proposal's identity, so separate authorized reviewers can inspect the same
proposal. Changing the source, owner/outlet, product or quantity changes it.

The route is a no-store POST read, compiled unreleased. Audit failure prevents
disclosure. It creates no approval, stock reservation, invoice or payment. Stock
availability and clinical eligibility are not inferred from prescription allowance.
The response says `state: proposed`, `stock_reservation: not_reserved` and
`clinical_authority: not_evaluated`. No persisted command/migration is required.

The UI collects explicit quantities, clears the projection when quantities or
source/account change and offers current-source refresh after a conflict. It uses
recorded medicine names, never backend account/product IDs as labels.

Required tests cover canonical identity, changed source/quantity, distinct readers,
input boundaries/unknown authority assertions, remaining allowances, permissions,
foreign parent/tenant, audit failure, no sale/stock/financial writes and account
transition protection. Database tests run hosted; no local database/app/container
is started. This component does not complete or release the approval/dispensing
workflow and cannot be presented as its authorization token.
