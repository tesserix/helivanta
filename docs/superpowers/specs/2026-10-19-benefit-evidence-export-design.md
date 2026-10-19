# Benefit eligibility evidence and coverage export

Issue: #1106 slice C (parent #1035, root epic #979; export catalogue with #1066). Builds on the [dependents and benefits API](2026-10-18-dependents-benefits-design.md) and its [screens](2026-10-19-benefits-screens-design.md). It reuses the [tax declaration evidence](2026-10-19-tax-declaration-evidence-design.md) pipeline and the bounded CSV rules of [ADR-0034](../../adr/0034-pharmacy-stock-export.md).

## Problem

A coverage proposal states an eligibility basis, such as a marriage certificate or a birth certificate, but the approver checks it offline. The approver's note is the only trace, and the document is not kept with the proposal it supported. HR also cannot take the coverage in force on a date out of the system without opening every employee.

## Decision

- **Evidence is a workforce document whose record is a coverage proposal.** `workforce_documents` gains `record_kind='benefit_proposal'` with the single category `benefit_eligibility_evidence`. `record_id` is the proposal id and `record_version` is the proposal's `base_version + 1`. Upload, malware scanning, release, retry and download reuse the existing pipeline and worker unchanged. There is no second store or scanner and no new table. Binding evidence to the enrollment instead was rejected: the evidence must belong to the exact proposal that was decided.
- **Only a pending proposal takes evidence.** The database refuses a new evidence row unless its proposal is pending and belongs to the same tenant, hospital and staff member. The limit is 10 files per proposal, and an expired upload intent does not count toward it. Images only (`image/png`, `image/jpeg`, 10 MiB), the envelope's existing formats.
- **Decisions wait for scanning, and the decision freezes the set.** Approve, reject and withdraw each return 409 `evidence_scanning` while any of the proposal's files is `scan_pending`. In the decision's transaction every file gets `frozen=true`. Triggers refuse to unfreeze a file, to scan it again, or to decide while a file is unfrozen. Approved evidence stays readable as the record of what the approver saw. Reject and withdraw revoke the available files, because material for coverage that never took effect is not kept readable. These are the tax-evidence rules unchanged.
- **Benefits evidence is separate from HR documents, structurally.** The `document_isolation` policy admits `benefit_proposal` rows only through `workforce_benefits_allowed`. That needs `app.workforce_benefits`, which only benefits handlers set. Staff document, enrollment and payroll routes therefore cannot list, download or revoke benefits evidence even if called with its id. Download audits name the action `workforce.benefits.evidence_downloaded`.
- **One export, coverage in force on a date.** `GET …/hr/benefits/coverage.csv?as_of=YYYY-MM-DD[&plan_id=]` returns every approved enrollment whose coverage includes `as_of`, optionally for one plan. `as_of` is required so the file always states which day it describes. It follows ADR-0034:
  - Unknown, repeated or empty parameters get 400.
  - A 10-second deadline applies.
  - The export is all or error: above 1,000 rows or 8 MiB it returns 413 `benefits_export_too_large` before any file bytes.
  - CSV uses CRLF, formula-like text is prefixed with an apostrophe, and a header-only file means a genuinely empty result.
  - Responses are `no-store` and `nosniff` with a server-generated name, `benefits-coverage-<as_of>.csv`.
- **Minimum necessary columns.** Each row has the enrollment id, employee code and name, employment status, plan code, name and kind, and who is covered: the employee, or the dependent's name, relationship and status. It also has the coverage dates, the approved version and the approval time. Birth dates, phone numbers, eligibility bases, decision notes and evidence are excluded. They are not needed to reconcile who is covered, and each file copied out of the system enlarges what can leak.
- **Export has its own permission and audit.** `workforce.benefits.export` with a reader or manager hospital grant, and no other benefits permission implies it. Each generated file writes one `workforce.benefits.exported` audit row in the same transaction. The row carries the report id `workforce.benefits.coverage`, the filters, the row count and the SHA-256 of the bytes, but no cell values. If the audit cannot be written, no file is sent. The report id is stable, so the #1066 catalogue can list this report later without changing it.
- **One formula-safe CSV encoder.** Pharmacy's bounded buffer and formula-safe text move to `backend/pkg/csvsafe`, used by both modules. Two copies of a spreadsheet-injection rule would drift, so copying it was rejected.
- **One workforce evidence client and panel.** The tax evidence client and `DeclarationEvidence` become a shared workforce evidence module, parameterised by record kind, path and labels. Tax and benefits each supply only their scope. Supply licence evidence belongs to another module with its own schema and is not merged. A third copy of the upload, digest and receipt checks was rejected.
- **Fail closed.** An FGA failure is 503, a foreign tenant or hospital gets 404, and a member without the permission gets 403. A database or audit failure returns an error and never a partial or stale file. There is no cache.

## Permissions

| Action                                  | Permission                  | Hospital grant    |
| --------------------------------------- | --------------------------- | ----------------- |
| List and download a proposal's evidence | `workforce.benefits.view`   | reader or manager |
| Attach evidence or retry a failed scan  | `workforce.benefits.manage` | locked manager    |
| Export coverage in force on a date      | `workforce.benefits.export` | reader or manager |

## Routes

Proposal scope: `…/workforce/hospitals/{hospital}/staff/{staff}/benefit-enrollments/{enrollment}/proposals/{proposal}/evidence`.

| Route                                                 | Behaviour                                                                                                                                            |
| ----------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET …/evidence`                                      | The proposal's evidence, newest first, with `uploads_available`. 404 when the proposal is not this enrollment's                                      |
| `PUT …/evidence/{file}`                               | Registers a file (`filename`, `content_type`, `size`, `sha256`). Identical replay returns it. 409 `not_pending`, `evidence_limit`                    |
| `PUT …/evidence/{file}/content`                       | Streams the bytes, verified against size and digest, then queued for scanning                                                                        |
| `PUT …/evidence/{file}/retries/{attempt}`             | Re-queues a `scan_error` file                                                                                                                        |
| `GET …/evidence/{file}/content`                       | Released bytes of an `available` file; audited                                                                                                       |
| `GET …/hospitals/{hospital}/hr/benefits/coverage.csv` | `as_of` (required), `plan_id` (optional). `X-Export-Row-Count` and `X-Export-Sha256` headers. 400 `invalid_request`, 413 `benefits_export_too_large` |

The decision and withdraw routes gain 409 `evidence_scanning`.

## Data

Migration `0034_workforce_benefit_evidence`:

- Widens the record kind, category, record binding and frozen-kind checks on `workforce_documents`, and adds a partial index for `benefit_proposal` rows.
- Adds the `benefit_proposal` branch to the document creation trigger, which locks the proposal and checks pending status, staff and limit. It refuses with `workforce_benefit_evidence_pending` and `workforce_benefit_evidence_limit`.
- Adds a freeze guard (`workforce_benefit_evidence_frozen`). It also adds a decision trigger on proposals that refuses while scanning (`workforce_benefit_evidence_scanning`) and refuses unless every file is frozen, with nothing left readable when the proposal is not approved (`workforce_benefit_evidence_unsettled`).
- Adds the `benefit_proposal` branch of `document_isolation`, read and write through `workforce_benefits_allowed`.

## Events

No new subjects. Evidence uses the existing `document_scan_requested` and `document_changed` events, whose payloads carry identifiers only. The export is a synchronous read and publishes nothing.

## Screens

- **Evidence.** Each enrollment row offers "Evidence" for its pending proposal, and for the approved proposal behind its coverage. The panel lists files with their scan state and downloads available ones. A locked manager can attach while the proposal is pending. The review and withdraw forms show the proposal's evidence read-only and disable their buttons, with the reason stated, while a file scans. The list polls only while a file scans.
- **Export.** The hospital view shows "Export coverage" only with `workforce.benefits.export`. It has a date (required) and an optional plan, validated inline. The download runs through `useApiMutation` with an abort signal, so changing hospital cancels it and a late response never downloads. It checks the server's file name and content type and reports the row count.

## Compliance

- **SOC 2 CC6.1/CC6.3:** export is a separate least-privilege permission on top of an explicit hospital grant. Evidence sits behind the benefits RLS scope.
- **SOC 2 CC8.1:** decisions see fully scanned evidence, and the decided set is frozen.
- **HIPAA §164.312(b):** audit rows record every evidence download and every generated export. The export row records the file's digest, so a copy can be traced to the request that produced it.
- **HIPAA §164.502(b) minimum necessary:** the export omits birth dates, phones, notes and evidence.

This audit records that a file was generated, not that it was opened or kept. Policies and files in tests are synthetic.

## Tests

- **Database:** evidence refused for decided proposals, other staff and other hospitals. The limit holds, frozen files cannot be unfrozen or rescanned, and a decision is refused while a file scans or remains unsettled. A staff-document or payroll-scoped transaction cannot see benefits evidence.
- **API:** register, upload, scan through the real worker, then download. Permission, tenant and hospital denials; replay and the limit. Approve, reject and withdraw settle the set, and `evidence_scanning` blocks them.
- **Export, asserted on the actual bytes:** the as-of boundaries, the plan filter, formula hazards, a header-only empty file, and 413 at 1,001 rows with no file bytes. Also unknown, repeated and empty parameters, denials, the audit row's count and digest matching the body, and no audit on failure.
- **UI:** the evidence panel's states, upload with inline errors, decisions held while scanning, and the export form's validation, permission gating, download, abort and errors.

## Not in this slice

- PDF evidence (envelope format support) and staff self-service upload (#1132).
- Exports above 1,000 rows, which need a separately designed asynchronous path, and scheduled exports.
- Listing the report in the #1066 catalogue, which does not exist yet; this slice fixes the identifier it will use.
- Insurer file formats, premiums and payroll deductions.
