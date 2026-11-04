# Audit every pharmacy read that names or points to a person (#1555)

Issue: #1555. Follows the audited registers (#1062, H1), patient-wise sales
(#1060) and the sales worklist (#987, #1554).

## Why

AGENTS.md: "Preserve audit consequences for sensitive reads as well as
writes." The registers, patient-wise sales, the admission resolve and the
patient lookup record who read them. The counter's own records do not: an
invoice shows the buyer's name and phone or the registered patient, a credit
note its recipient's name and address, a prescription its patient's name and
address, and nothing records who looked.

## Decisions (my calls)

1. **One mechanism.** Every audited read calls the existing
   `auditRegisterRead` inside the read's transaction, after the rows are
   read: an `audit_events` row (resource type, action, the filter _names_,
   never values) and an audit log line (with the values). An audit failure
   fails the read; no audit row, no data.
2. **What is recorded against what.**

   | Read                             | `resource_type`           | `resource_id`    | `action`          |
   | -------------------------------- | ------------------------- | ---------------- | ----------------- |
   | `GET …/sales/{id}`               | `pharmacy_sale`           | the sale         | `record_read`     |
   | `GET …/sales`                    | `pharmacy_sales`          | the outlet       | `list_read`       |
   | `GET …/credit-notes/{id}`        | `pharmacy_credit_note`    | the note         | `record_read`     |
   | `GET …/credit-notes`             | `pharmacy_credit_notes`   | the outlet       | `list_read`       |
   | `GET …/prescriptions/{id}`       | `pharmacy_prescription`   | the prescription | `record_read`     |
   | `GET …/prescriptions`            | `pharmacy_prescriptions`  | the outlet       | `list_read`       |
   | `GET …/prescriptions/open-lines` | `pharmacy_prescriptions`  | the outlet       | `open_lines_read` |
   | `GET …/sales/worklist`           | `pharmacy_sales_worklist` | the outlet       | `list_read`       |
   | `GET …/sales/{id}/summary`       | `pharmacy_sale`           | the sale         | `summary_read`    |

   A record read is recorded against the record, so "who opened this
   invoice" is one query on the existing `(tenant_id, resource_type,
resource_id)` index; a list page is recorded against the outlet, with its
   filter names.

3. **Always, not when a row happens to name someone**, for the sale,
   credit-note and prescription reads: nearly every row names or points to a
   person, and a rule that audits only some pages is a rule to get wrong.
   Fail closed.
4. **The worklist and summary only when an admission sale is in the
   response.** Their only pointer to a person is an admission sale's bed
   code; a page of walk-in and registered-patient rows carries none (#987
   decision 3).
5. **Refused reads record nothing.** A 400, 403 or 404 happens before or
   instead of the read, so its transaction never reaches the audit insert.
6. **The dispense queue is not covered here.** It is a tenant-wide clinical
   worklist fed by MediCore's visits, with its own counted list and
   insights routes; auditing it is a decision about the dispensing workflow
   (#1555 stays open for it).

## Not covered

- The dispense queue (decision 6).
- A screen listing who read a record.
- Reads outside PharmaConnect.
