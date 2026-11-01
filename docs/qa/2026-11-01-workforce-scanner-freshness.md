# Workforce scanner freshness acceptance

A normal Staff Admin UI upload was scanned with real ClamAV definitions about 28 hours old. PostgreSQL rejected the available receipt with `invalid scanner provenance` (SQLSTATE 23514). Files remained protected. Scanner policy already accepts definitions up to 48 hours old and warns through readiness after 36 hours; see #1242 and the [accepted design](../superpowers/specs/2026-10-18-scanner-definition-freshness-design.md).

Forward migration `0067_workforce_document_scanner_freshness` preserves the latest recovery trigger byte-for-byte except its 24-hour bound becomes 48 hours. Ownership, restricted worker identity, state transitions, retry clock, immutability, frozen evidence and future-date protections remain intact. Historical migrations are unchanged. Recovery retains the ledger and uses a reviewed forward correction.

## Local browser evidence

- Normal Staff Admin sign-in used the private local JSON fixture.
- Uploaded `synthetic-scanner-48h-acceptance-20261101.png` through Staff documents with Employment evidence and Staff enrollment selected.
- Real ClamAV and the narrow worker identity committed release; Refresh status displayed Ready.
- Clicked Download. The automation download event timed out, but Chrome saved the file. Both downloaded copies match all 179 original bytes (SHA-256 `a8cf1db44d3b6cb252b53506ac3daadba919e0bb2c0721901851d335d83e3c64`).
- Normal local owner startup upgraded the existing fixture database. Ledger 201 includes all four candidate migrations; PostgreSQL confirms the installed 48-hour function.
- Owner and worker readiness return 200; activation remains explicitly false.
- Private screenshot `scanner-48h-ui-ready.jpg` and structured evidence `scanner-48h-ui-acceptance.json` / `scanner-freshness-runtime.json` are in the existing reviewer UI evidence directory.

## Regression evidence

The initial 30-hour regression failed with the exact SQL error; its focused rerun passed after migration. Additional cases cover release at 47 hours despite readiness warning, refusal at 49 hours and six minutes in the future, no refused promotion/download, and independent SQL provenance rejection through the narrow worker role.

The first SQL denial assertion attempted scan_error → available directly. PostgreSQL correctly rejected the illegal transition first. The corrected test follows the worker's scan_error → scan_pending transition with the required attributable service actor before asserting provenance rejection. An intermediate run also confirmed that missing audit actors are rejected. No guard was weakened.

Validation commands and final results are recorded in the progress tracker. Initial script/generator runs lacked workspace dependencies; frozen offline installation and the repository's Node 24 runtime restored the checks.

## Remaining acceptance

Both earlier terminal uploads recovered through the normal Staff Admin Retry scan action after the existing 45-minute grace period. Each first workflow remains Failed in history; each second workflow is Completed. Refresh status shows both files Ready. No manual verdict or fabricated available status was persisted. Digital recipient UI acceptance, activation races/governance and consumer/lifecycle cutoffs remain open. The named production revision and all four migrations were subsequently approved and deployed; see the production release record below.

## Final validation checkpoint

The complete affected race suite passed: Workforce 547.734s and attachments 3.191s. Backend build, scoped vet/lint (0 issues), contractdocs generation/check, 23 canonical generators, 75 script tests and 15 operational inventory tests passed. CI initially detected the changed module source pin; re-review confirmed that only the embed/migration registration changed, and the ten hashes and generated inventory were refreshed without changing classifications or outstanding proof obligations.

## Supported recovery acceptance

The Staff Admin browser recovered files `c34e267e-3cf3-4e75-a1e2-77106acb1362` and `dfacd0cf-ebd1-44a0-bda3-c8eb74f9dd09` through Retry scan. The UI exposed that action only after the pending grace period, confirmed the requests, and displayed Ready after normal worker processing. Read-only Temporal descriptions confirm preserved failed attempt 1 and completed attempt 2 for each exact scoped workflow ID. Private evidence is `scanner-ui-recovery.json` and `scanner-both-ui-retries-ready.jpg`. This closes the scanner recovery test for these local files; it does not close the separate reviewer file-delivery or operational activation gates.

## Production deployment and delivery correction

Approved revision `02b1e2b7` and all four migrations are deployed; see the [release record](../deployment/releases/2026-11-01-staff-activation-scanner.md). Normal production Staff Admin UI upload, real scanner release, exact 179-byte browser download and subsequent audited revocation passed using an existing synthetic Staff record and existing owner hospital authority. Reviewer access remained restricted; activation stays off.

The earlier local reviewer artifact also exists and matches the original bytes after download completion. The earlier missing-file check was premature, so saved-file delivery is no longer an open gate. The newly UI-created source finding and employment terms were subsequently independently approved through the UI and reopened successfully; broader operational activation gates remain open.

## Fully UI-prepared source acceptance

The preparer created applicant `SYN-FULL-UI-20261101`, fixed-term revision 1 and separate identity/employment files through normal UI workflows, waited for real scanner release, bound the employment file to the exact terms revision, and submitted both person and employment findings through the UI. No signed API business-source setup was used for this case.

A separate reviewer signed in normally, downloaded the exact bound files and verified all 179 original bytes, examined the recorded terms, and approved both findings. Empty decision reasons were blocked. Reopening the reviews displayed Approved finding and Approved terms with their saved reasons. The directory still shows the applicant Inactive, so approvals alone did not activate employment or provision a login/access assignment.

This accepts the local non-digital source preparation and independent approval journey. Fresh-source activation, digital recipient and consumer/lifecycle enforcement remain separate gates. Private artifacts are `full-ui-person-approved.jpg`, `full-ui-employment-approved.jpg`, `full-ui-approved-staff-still-inactive.jpg` and `full-ui-source-case.json`.
