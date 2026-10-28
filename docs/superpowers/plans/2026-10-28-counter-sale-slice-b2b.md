# Counter sale slice B-2b implementation plan: selling against a prescription

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this
> plan task by task.

**Goal:** a pharmacist sells Schedule G, H and H1 medicines at the counter. Each such line
cites a recorded prescription line for the same product, within the units and dispensings
the prescriber allowed. A sale can optionally record a walk-in buyer.

**Spec:** `docs/superpowers/specs/2026-10-27-counter-sale-design.md` (#1386): B5, B6 and
"B-2b decisions" 1–7, with the sources from Rule 65.

**Architecture:** migration `0017_pharmacy` (`prescription_sale.sql`):

- a new nullable `prescriber_address` column on `pharmacy_prescriptions`;
- a new insert-only table, `pharmacy_prescription_lines`;
- `pharmacy_sale_lines` widened to the classes G, H and H1, with a nullable citation of a
  prescription line;
- the replaced `pharmacy_sale_line_guard`, which locks the cited prescription line and
  checks product, units, dispensings, date and addresses;
- nullable `buyer_name` and `buyer_phone` columns on `pharmacy_sales`.

The Go handlers repeat the guard's checks only to give the cashier a reason. The database is
the authority.

## Global constraints

- **Prescription lines** go in the prescription PUT body as `lines`, 1–20 of them:

  | Field                 | Rule                                        |
  | --------------------- | ------------------------------------------- |
  | `prescribed_as`       | 1–200 characters, trimmed, no control chars |
  | `product_id`          | a product of the outlet                     |
  | `quantity_units`      | 1–100000, the total across all dispensings  |
  | `dispensings_allowed` | 1–12                                        |

  `prescriber_address` is optional, 1–500 characters, trimmed. Both are part of the intent
  digest. A replay of a prescription recorded before this change is unaffected: an absent
  address adds nothing to the digest. A body without `lines` is now 400.

- **`GET /prescriptions/:id`** adds `lines`, each with `units_remaining` and
  `dispensings_remaining`.
- **`GET /prescriptions/open-lines?product_id=`** (`pharmacy.prescription.read`) returns, for
  the counter, the lines for that product that still have units and dispensings left. It
  returns at most 50, the newest prescription first.
- **Sale and quote line:** `prescription: {prescription_id, line_number}` is optional on a
  line:
  - it is required when the product's class in force is G, H or H1;
  - it is refused (`not_sellable`) on an OTC line.
- **Sale:** `buyer: {name?, phone?}` is optional. `name` is 1–200 characters; `phone` matches
  `^\+?[0-9]{6,15}$`.
- **Refusal reasons** (409 `not_sellable`):

  | Condition                         | Reason                                                                                                                      |
  | --------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
  | no prescription cited             | "SKU is Schedule H and needs a prescription"                                                                                |
  | the cited line is another product | "the prescription line is for another product; a prescribed medicine is not substituted"                                    |
  | over the units left               | "the prescription allows N more units of SKU"                                                                               |
  | no dispensings left               | "the prescription for SKU has been dispensed as often as the prescriber allowed"                                            |
  | H or H1 without both addresses    | "a Schedule H or H1 prescription needs the prescriber's and the patient's address; record the prescription again with them" |
  | dated after the sale              | "the prescription is dated after today"                                                                                     |

- **Events:** `sale_posted.v1` is unchanged. It carries no buyer or prescription data.
- **The not-covered list** is the spec's.

## Review focus

1. Two tills dispensing the last allowed units or dispensing of one prescription line at the
   same moment: exactly one succeeds.
2. One sale whose FEFO allocation splits a prescription line over two batches counts as one
   dispensing, and the units are summed.
3. A replayed sale (same id) does not count a second dispensing.
4. The database guard alone refuses a substituted product, an over-quantity line and an OTC
   line carrying a citation, when the handler is bypassed by a direct insert.
5. A B-2a replay of the same prescription body without `prescriber_address` still returns 200,
   and with `lines` absent returns 400.

---

### Task 1: Migration `0017_pharmacy`

**Files:**

- create `backend/internal/modules/pharmacy/prescription_sale.sql`;
- modify `module.go` (embed it as `0017_pharmacy`);
- test `prescription_sale_test.go`.

- [ ] **Write the failing test** `TestTheDatabaseHoldsPrescriptionSalesToTheirPrescription`.
      Direct inserts as the outlet editor:
  - a substituted product raises `pharmacy_sale_line_source`;
  - an over-quantity line raises;
  - an OTC line with a citation raises;
  - an H line without addresses raises;
  - an UPDATE of a prescription line raises.
- [ ] **Implement:**
  - lines PK `(tenant_id,outlet_id,prescription_id,line_number)`, with an `id` column for
    audit;
  - the FKs to the prescription and the product;
  - the `pharmacy_catalogue_immutable` trigger and forced RLS through
    `pharmacy_outlet_allowed`;
  - the guard, which takes `SELECT … FOR UPDATE` on the cited line before summing, so
    concurrent sales serialise and the sum sees committed rows.
- [ ] **Run the test, then commit.**

### Task 2: Prescription lines and the prescriber's address in the API

**Files:** `prescriptions.go`, `prescriptions_test.go`.

- [ ] **Failing tests:**
  - `TestAPrescriptionRecordsWhatWasPrescribed`: lines round-trip, and remaining counts
    equal the totals;
  - the B-2a replay case from Review focus 5;
  - a line naming another outlet's product returns 404;
  - more than 20 lines returns 400.
- [ ] **Implement:** insert the lines in the same transaction. Add `loadPrescriptionLines`
      and the `open-lines` route.
- [ ] **Run, then commit.**

### Task 3: Selling against a prescription line

**Files:** `sale.go`, `sale_test.go` (the B-1 refusal test now expects the "needs a
prescription" reason), `prescription_sale_test.go`.

- [ ] **Failing tests:**
  - `TestAScheduleHSaleDispensesAgainstItsPrescription`: posts, decrements the remaining
    count, and the invoice line shows the citation;
  - `TestAPrescriptionIsDispensedOnlyAsOftenAsAllowed`: a second sale is refused at
    `dispensings_allowed=1`, and allowed at 2;
  - `TestAPrescriptionLineIsNeverSubstitutedOrExceeded`;
  - `TestTwoTillsCannotOverDispenseOnePrescription` (Review focus 1);
  - `TestASplitAllocationIsOneDispensing` (Review focus 2);
  - the replay case (Review focus 3);
  - `TestHAndH1NeedBothAddresses`;
  - `TestScheduleGIsPrescriptionOnly`;
  - `TestAWalkInBuyerIsOptionalAndNeverPublished`.
- [ ] **Implement:**
  - `saleProducts` stops refusing G, H and H1;
  - `priceSale` resolves and checks citations;
  - the digest includes the citation and the buyer;
  - the inserts write `prescription_id` and `prescription_line` on every allocated line.
- [ ] **Run, then commit.**

### Task 4: Contracts and docs

- [ ] Update:
  - the archtest matrix: no new permission;
  - the unpaginated allowlist for `open-lines`, which is bounded to 50;
  - the `contractdocs` regeneration;
  - the "Counter sale" and "Prescription evidence" sections of `docs/api/domains/pharmacy.md`.
- [ ] **Commit.**

### Task 5: The counter and prescription screens

**Files:** `packages/api/src/pharmacy-prescriptions.ts` and `pharmacy.ts`; `prescriptions.tsx`
and `counter-sale.tsx` with their tests.

- [ ] **Prescription form:** add the prescriber's address and repeatable lines. Each line has
      the product (`FormSelectField`), the drug as written, the units and the dispensings.
- [ ] **Counter:** when a basket product's class is not `none`, a required "Prescription"
      select lists `open-lines` for it. The basket line carries the citation. The tender form has
      optional buyer name and phone. After posting, a prescription sale shows "Endorse the
      prescription: note this outlet's name and address and today's date on it".
- [ ] **Tests:**
  - an H product without a prescription line cannot be priced;
  - with one selected, the citation is sent;
  - the buyer fields are optional;
  - the endorsement reminder shows for prescription sales only.
- [ ] **Run the frontend gate, then commit.**
