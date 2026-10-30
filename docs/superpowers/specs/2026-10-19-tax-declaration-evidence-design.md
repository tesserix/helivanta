# Tax declaration evidence

Issue: #1041 slice 5b-ii (parent epic #979). ADR: [0029](../../adr/0029-payroll-configuration.md). Builds on [staff income-tax declarations](2026-10-18-income-tax-declarations-design.md), which deferred evidence here, and on the [document image envelope](2026-10-15-document-image-envelope.md), which owns upload, malware scanning and release.

## Problem

A declaration proposal states rent, home loan interest and claimed amounts. The approver must check them against rent receipts, the lender's interest certificate and investment proofs before payroll relies on them. Today the approver checks offline and the decision note is the only trace. The evidence itself is not kept with the revision it supported.

MedNet (read-only review, 2026-10-19) has no per-employee declaration or evidence screen. Its Tax Setup keeps only exemption types with a yearly maximum, so this slice adds capability that the legacy app does not have rather than porting one.

## Decision

- **Evidence is a workforce document whose record is a declaration revision.** `workforce_documents` gains `record_kind='tax_declaration'` with the single category `tax_declaration_evidence`, and `record_id` is the revision id. Upload, scanning, release, retry and download reuse the existing pipeline and worker unchanged; no second store or scanner.
- **Only a pending revision takes evidence.** The database refuses a new evidence row unless its revision is pending and belongs to the same tenant, hospital and staff member. Once the revision is decided, its evidence set is frozen: approved evidence stays as the record of what the approver saw.
- **Decisions wait for scanning, fail closed.** No decision or withdrawal is taken while any of the proposal's evidence is `scan_pending`. Otherwise the approver would approve against files nobody could open. An `upload_pending` intent never received bytes. It does not block, and the decision freezes it so it can never receive any. Evidence rejected by the scanner, or that ended in `scan_error`, does not block either. The approver sees its state and decides.
- **The decision freezes the evidence set.** Each file gets `frozen=true` in the decision's transaction. Triggers then refuse to unfreeze it, to scan it again, or to decide while a file is still unfrozen or, unless approved, still readable. Freezing a `scan_error` file keeps its verdict, so the processing-identity guard allows that one update and nothing else.
- **Rejecting or withdrawing revokes the proposal's available evidence** in the same transaction. This matches the PAN ciphertext erasure: material for a proposal that never took effect is not kept readable.
- **Payroll evidence is separate from HR documents, structurally.** The RLS policy on `workforce_documents` admits `tax_declaration` rows only when the transaction sets `app.workforce_payroll_read='authorized'`, which only the payroll evidence handlers set. The existing staff document routes (`workforce.documents.manage`) therefore cannot list, download or revoke payroll evidence even if called with its id. Enrollment, credential and qualification rows keep their current rule.
- **Permissions:** attaching needs `workforce.payroll.configure`; listing and downloading need `workforce.payroll.view`. Every download is audited and served `no-store`, as staff documents are today. Receipts may show a landlord's PAN; payroll viewers already see declaration amounts and party names, so evidence sits at the same level as the declaration it supports.
- **Limits:** up to 10 evidence files per revision, images only (`image/png`, `image/jpeg`, 10 MiB). These are the envelope's existing formats; PDF receipts need the envelope to support PDF first and are not covered here.

## API

Under `/workforce/hospitals/{hospital}/hr/payroll/staff/{staff}/tax-declarations/{tax_year}/evidence`:

| Route                           | Permission                    | Behaviour                                                                                                                                           |
| ------------------------------- | ----------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET ?revision_id=`             | `workforce.payroll.view`      | Evidence for that revision, newest first; 404 when the revision is not this staff member's                                                          |
| `PUT /{file}`                   | `workforce.payroll.configure` | Registers a file (`revision_id`, `filename`, `content_type`, `size`, `sha256`); idempotent on identical replay; 409 `not_pending`, `evidence_limit` |
| `PUT /{file}/content`           | `workforce.payroll.configure` | Streams the bytes; verified against size and digest, then queued for scanning                                                                       |
| `PUT /{file}/retries/{attempt}` | `workforce.payroll.configure` | Re-queues a `scan_error` file                                                                                                                       |
| `GET /{file}/content`           | `workforce.payroll.view`      | Released bytes of an `available` file; audited, `no-store`                                                                                          |

A decision or withdrawal returns 409 `evidence_scanning` while any evidence of the proposal is unscanned.

## Events

No new subjects. Evidence uses the existing `document_scan_requested` and `document_changed` events, whose payloads carry identifiers only. `tax_declaration_changed.v1` is unchanged.

## Not covered

- PDF evidence (envelope format support).
- Staff self-service upload (#1132).
- Matching evidence amounts to declared amounts automatically; the approver checks them.

## Tests

- Database: evidence insert refused for decided revisions, other staff, other hospital; enrollment-path transaction cannot see payroll evidence; approval refused while scanning; reject and withdraw revoke.
- API: register, upload, scan through the real worker path, download; permission and tenant denials; limit and replay; 409s.
- UI: evidence list under each proposal with scan states, upload with inline errors, approve disabled with a stated reason while scanning.
