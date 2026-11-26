# Pharmacist verification completion — #169

The completion target is every requirement in [#169](https://github.com/tesserix/helivanta/issues/169), not the two deployed contract/context foundations. The [design and acceptance matrix](../../design/pharmacist-verification.md) remains the requirement ledger. The issue was found closed on 26 November and reopened because decisions, clinical authority and supply enforcement are absent.

## Boundaries and hazards

PharmaConnect owns recorded prescription copies, outlet stock, sales and verification evidence. MediCore owns signed prescriptions and amendments; the practitioner owner supplies professional identity and current registration observations. IAM and the relevant organization supply current operating assignments. Registration attestation explicitly does not establish clinical authority. No HR review, owner role, account link or supplier grant substitutes for clinical privileges.

The principal hazard is supply authorized by an unqualified actor or approval of a different prescription/fulfillment state. Consequential writes need current authoritative eligibility and an atomic guard in the active sale transaction. A source fingerprint alone is insufficient. Required safeguards are IN-PH-02/04/06 and ORG-AU-04/08, with the adverse cases in the existing matrix.

## Review workspace

Build the recorded-source review workspace against the existing audited context API before introducing decision controls. Display patient and prescriber names, council/registration, recorded dates, addresses, prescribed lines and remaining recorded allowances. Distinguish unavailable allergies, medication history, age, weight and flags from negative findings. Explain that the recorded copy is neither authenticated clinical source nor supply approval. Do not show opaque IDs as human labels.

The workspace is a candidate behind the same compiled `unreleased` capability as the API. Neither existing pharmacy pilot flags nor a provider toggle may expose it. Do not display pretend approval, second-check, query or emergency buttons while their commands do not exist.

Each read binds tenant, subject, pharmacy, outlet and prescription through the SDK. Cache keys contain all these coordinates. Account switching clears the view immediately, including cancellation of the switch; old data must require a fresh read before display. Refresh or focus revalidation hides earlier clinical context until its replacement is accepted. An error never presents previously loaded data as current. Unmount discards sensitive query data. A late response cannot repopulate a changed workspace.

Use the existing page-list flow and a focused review panel rather than a new independently maintained prescription list. The reviewed copy is returned by the owner; do not reconstruct a second wire model in the UI. Review focus moves to the panel heading and returns to its trigger on close. Loading, denied, unavailable and retry states remain accessible.

## Remaining completion sequence

1. Runtime practitioner owner integration and explicit professional-person/account linkage, current registration and accountable clinical privilege policy. Registration validity/revocation must be checked at both decision and supply, not only by a nightly tuple projection.
2. Signed clinical source and authorized context/proposed fulfillment, with source versions, provenance, unavailable facts and clinical-owner amendment semantics.
3. Durable prioritized queue, SLA/escalation and immutable per-attempt decisions/receipts. Bind exact context, source, fulfillment and effective policy; reject stale or changed retries.
4. Independent second check by a different professional person, plus governed prescriber query/response and patient-status delivery/recovery.
5. Database and handler enforcement in counter-sale stock/financial commit, including partial fills, concurrent mutation, API and worker paths. Keep the retired dispense endpoint retired.
6. Approved emergency policy, narrowly scoped exception authority, retained reason and retrospective deadline/escalation. Without accountable policy approval, emergency supply stays unavailable; this remains a recorded acceptance dependency, not completed functionality.
7. Complete UI/API/navigation/dashboard flags, real-database and E2E acceptance, focused local builds, protected CI, merge and signed Kargo deployment. Verify actual pharmacist/prescriber/patient outcomes and each requirement before closing #169.

These stages organize implementation; they do not reduce the agreed scope or authorize clinical policy. No live migration or clinical activation is part of the review-panel change.
