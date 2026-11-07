# SupplyConnect stock intake and reconciliation

Implementation candidate on the independent supplier workflow branch. This contract has not been released.

SupplyConnect owns distributor warehouse/depot stock; PharmaConnect continues to own outlet stock. The current candidate records physical intake in quarantine and attributable product/unit observations. Quantity availability, quality release, reservations, dispatch and destination receipts require the remaining workflow implementation.

Canonical inputs: [supply-stock.json](../../contracts/supply-stock.json) and [stock-products.json](../../contracts/stock-products.json). Run `node scripts/generate-stock-products.mjs` followed by `node scripts/generate-supply-stock.mjs`; both support `--check`. Owner migrations, generated Go, generated Zod, strict SDK and warehouse UI use these inputs. Registered routes and permissions are exported through [the registry](reference/README.md).

## Owned physical receipt

Paths below are relative to `/v1/supply/parties/{party}/operating-locations/{premise}/stock` and travel through Global API for browser/app consumers.

| Method | Path                                          | Permission                                               | Owner checks                                                                                              |
| ------ | --------------------------------------------- | -------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| GET    | `/products/by-gtin/{gtin}`                    | `supply.operations.read` or exact reviewed receiver duty | Current owned warehouse/depot scope; published stock-purpose fields only                                  |
| PUT    | `/receipts/{receipt}`                         | `supply.operations.write`                                | Current inventory/administrator access to this owned, active warehouse/depot                              |
| GET    | `/receipts/{receipt}`                         | `supply.operations.read`                                 | Current permitted operating access; archived history restricted to administrator/auditor                  |
| GET    | `/receipts`                                   | `supply.operations.read`                                 | Same scope, bounded signed keyset page                                                                    |
| PUT    | `/receipts/{receipt}/identity-checks/{check}` | `supply.operations.write`                                | Current inventory/administrator access, active premise, current source and exact next observation version |
| GET    | `/receipts/{receipt}/identity-checks/{check}` | `supply.operations.read`                                 | Current operating/history access; reads retained evidence                                                 |

Receipt and check identifiers are client-generated UUIDv7 values. The server derives tenant and actor from refreshed authentication; expected tenant/subject headers guard a changed browser session. IDs supplied by a caller provide no authority. Both tables force RLS, retain immutable rows and refuse deletion/truncation. Sensitive reads produce audit evidence.

A receipt retains an exact product-version reference, lot, date-only expiry, counted units, unit code, external source-document line and reason. Its product reference starts unverified. The immutable intake snapshot states `stock_status:quarantine`, `product_identity_verified:false` and `available_units:0`. These fields describe the original intake; they are not an aggregate stock availability report. A later product observation preserves the receipt.

Counts are positive safe integers through `9007199254740991`. Codes/strings are bounded, trimmed and exclude control characters; dates must be actual calendar dates. An expired physical item can still be recorded in quarantine. Release/commitment eligibility must evaluate the premise-local expiry separately.

A source document line is unique within `(tenant, party, premise)`, including when another receipt ID is submitted. Concurrent identical PUTs return one creation and the same durable receipt to every successful replay. Changed content or another actor on an existing command is a conflict. A GET recovery reauthorizes current access: previous authorship does not survive an access cutoff.

## Current product observation

Drug master is the sole product/formulation owner. Its `stockproduct.Guard` exposes the exact current active product version, current formulation version, pack/unit, governed classification and labels. The process-local callback holds the catalogue publication boundary until the owning stock transaction completes. Consumers perform their own tenant/resource authorization before using that boundary and do no external network work inside its callback.

The read-purpose API is `GET /v1/drugmaster/products/by-gtin/{gtin}/stock-reference`, with `drugmaster.stock_reference.read`. It is routed through Global API. Existing clinical catalogue permissions remain unchanged. This purpose permission is limited to the declared pharmacy/supply roles and remains subject to normal tenant authorization.

The warehouse UI uses the source-scoped `/products/by-gtin/{gtin}` route instead of requiring catalogue-wide authority. The product handler refreshes identity and checks the exact operating scope before calling drug master's published stock-reference owner. Extra query parameters and invalid barcodes are rejected; inaccessible sources return 404 and unavailable catalogue/audit returns 503. Successful reads audit the source location. Duty revocation immediately blocks this route, including for the receiver who originally recorded stock. The existing generic drug-master route retains its original permission checks.

A stock identity input chooses an exact source version and explicitly confirms pack size and base-unit quantity. `pack` requires the original count's `pack` unit; `unit` requires the source's exact unit of issue. Multiplication checks the safe-integer bound before computing. Mismatched units, stale versions, inactive/recalled products or changed sequence produce conflicts. Source unavailability produces 503 and no new observation.

Each observation increments its receipt's observation version, retains the source-version evidence and actor, and audits/publishes within the same owner transaction. Observations are historical evidence. A later recall allows authorized GET recovery of the old observation but blocks a new match against the obsolete product version. Consequential stock commands must recheck current source/quality/licence/relationship/staff/policy state.

## Recovery and failure

- Source access changes serialize against receipt operations; inactive destinations are also rejected at the database write boundary.
- Audit/outbox failures roll back both physical evidence and its event. Responses omit raw database errors.
- Lost responses recover using the original command ID and intent. The UI retains uncertain input, freezes editing and recovers with GET before recording another count.
- Pages validate tenant, organization, premise, bounds and continuation shape in the SDK. Read refusal or dependency failure hides cached records and write controls.
- Events carry only source resource identifiers/version under the originating tenant: `helivanta.in.supply.stock_receipt_recorded.v1` and `helivanta.in.supply.stock_identity_recorded.v1`. They disclose no stock quantity, product labels, prices or credentials.

## Remaining operating gates

The candidate establishes neither bilateral collaboration nor an effective partner grant. Current employer/posting authority, independent quality decisions, verified source/destination licence and activity applicability, reviewed state/category/operation policies, object/field/file disclosure, source reservations/dispatch, destination discrepancy/quarantine/receipt and invoice/payable/credit/settlement reconciliation remain additional implementation and acceptance work. Initial live trade is scoped to India, Maharashtra and Odisha, with actual participants and qualified reviewers still to be supplied.
