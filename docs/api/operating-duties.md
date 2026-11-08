# Independent stock operating duties

Unreleased implementation candidate; no production activation or full supplier-workflow completion is claimed. See [ADR 0064](../adr/0064-independent-operating-assignments.md) and [independent Staff identity](workforce-independent-identity.md).

Shared IAM owns the duty, immutable decisions, graph snapshot and acknowledgement. Workforce owns current Staff/account identity and employer access. The physical products own the outlet/premise and all stock records. Supplier IDs are never exposed as hospital IDs.

## Current candidate routes

Paths are relative to `/v1/iam/operating-duties/{duty}`. IDs are UUIDv7. Expected tenant and subject headers must each match the authenticated principal exactly. Responses are private, uncached and attributable.

| Method | Path                  | Behaviour                                                                            |
| ------ | --------------------- | ------------------------------------------------------------------------------------ |
| PUT    | empty                 | prepare an immutable duty for one exact current Staff/link and physical source       |
| GET    | empty                 | current lifecycle record; lifecycle activation is not a reusable authorization proof |
| GET    | `/proposal-receipt`   | maker-bound original preparation receipt, retained after later state/expiry changes  |
| PUT    | `/commands/{command}` | independently approve/reject, maker-withdraw, or revoke using expected version       |
| GET    | `/commands/{command}` | actor-bound immutable decision recovery                                              |

[operating-assignments.json](../../contracts/operating-assignments.json) owns the strict DTOs and initial templates. Run `node scripts/generate-operating-assignments.mjs` and `--check`. Shared identity descriptors derive from the Workforce canonical inputs. The generator also produces the database template-permission function; clients cannot supply arbitrary permissions.

The Staff-private directory is `GET /v1/iam/operating-employers/{kind}/{employer}/staff/{staff}/duties`. It requires the same current read and private employer-reader authority. It accepts only one `limit` and optional signed `cursor`, orders by descending `(created_at,id)`, and audits the disclosure. Staff and employer identifiers filter owner data; they never supply authority. SDK pages request at most 25 rows and reject duplicate, foreign-owner or inconsistent continuation results. Duty and retained decision payloads include `owner_tenant_id` for explicit workspace verification.

Initial templates are `supply.inventory_receiver.v1` and `pharmacy.inventory_receiver.v1`. Both permit own-source quarantined receiving/history and the published stock-reference purpose. They do not authorize release, dispensing, dispatch, hospital/patient access or financial approval. The pharmacy template does not yet have a product consumer in this candidate; its presence is not working pharmacy end-to-end evidence.

Preparation/revocation requires an actual IAM owner/admin or pharmacy-owner membership with the corresponding permission. Compliance can review; auditors can read. TenantAdmin is implicit. Current employer-manager access is additionally mandatory for new decisions, and reader/manager access for retained reads. The IAM membership and employer access rows remain locked through the command, so deletion cannot race a completed authorization check. Structural employer registration remains a separate TenantAdmin bootstrap action.

The target must have uninterrupted current IAM membership and a current active, independently reviewed Staff/account link under the typed employer. Preparation binds exact Staff/link, organization and premise/outlet versions. A separate reviewer approves that immutable basis; raw maker/target identity and known historically approved aliases cannot satisfy independent review. The original proposal remains immutable when withdrawn, rejected or revoked.

## Projection and current use

Approval responds `approved_pending_activation` and commits the snapshot, history, immutable command receipt, audit and outbox together. External graph publication runs after commit. Partial or unavailable projection leaves the duty ineffective and retries the same immutable key. Acknowledgement rechecks current membership, source and Staff basis under the owner command. A changed basis or expiry revokes the pending revision; a delayed acknowledgement cannot undo explicit revocation.

`active` means the exact projection was acknowledged. Each product action must still call the current operating authorizer. That check combines the graph with the exact current duty/epoch/acknowledgement, time interval, uninterrupted membership, current source and current Staff/link revision. The current credential-revocation watermark is checked under the existing credential lock through commit. Ordinary sign-out invalidates the old session; it does not erase a still-valid business duty.

The callback receives only a typed scope, permission, duty/snapshot/epoch and Staff/link reference. Private IAM and Workforce database purposes are restored before product work. No scoped permission enters tenant-wide resolution. Source/identity/graph failure denies the action; unavailable integrations are not represented as an empty or zero-valued stock report.

## Product consumption and limits

SupplyConnect receipt recording, bounded receipt reads, scoped barcode lookup and identity reconciliation accept this current operating scope in addition to their existing administrative observation path. The scope is rechecked for every request, including GET recovery. The stock owner sets only an exact receipt-purpose database context; it does not grant access to unrelated party, bin, delivery or directory APIs. Original intake remains quarantine with zero available units after product identity matching.

The drug-master command-bound reference holds the publisher's source lock through the consumer transaction's commit. Its system read cannot release the only publication lock before the stock write commits. Reconciliation preserves the observed receipt and canonical published pack/unit basis; it does not make stock sellable.

The independent Staff editor now lists scoped duties and supports approval, rejection, maker withdrawal and revocation. Supplier preparation chooses an actual current warehouse/depot and a reviewed account, binding their current versions. The SDK validates exact preparation/decision receipts; ambiguous saves freeze intent and recover using GET, while an explicit rejected original write allows refresh. Read refusal hides private results. These are component-test and real-owner API results, not completed OIDC/browser acceptance for a receiver-only account.

This candidate still needs receiver-only navigation and browser acceptance, pharmacy product consumption, qualified quality release/balances/reservations/dispatch, bilateral supplier grants, hospital receiving/discrepancies and financial composition. Current licence, professional and exact Maharashtra/Odisha policy checks remain necessary at the applicable commitment boundaries. These administrative/identity records are not those checks.

Candidate IAM migration: `0038_iam_operating_duties`; candidate source migrations remain unreleased. Prepare and verify the final public-repository release, exact migrations and Kargo image promotion before seeking the required named production approval.

### Stock-quality duty expansion

The additive `supply.stock_quality_reviewer.v1` template permits source-scoped
`supply.operations.read`, `supply.stock.review` and
`drugmaster.stock_reference.read`. It follows the same independent Staff/link,
review, projection acknowledgement and current eligibility gates as receiving
duties. It does not inherit receiving or dispatch permissions. See
[the stock review contract](supply-stock.md#independent-stock-quality-review-candidate--8-november-2026).
The original template SQL is retained in `operating_templates_v1.sql`; future
catalogue expansions apply through additive migrations rather than rewriting
migration 0038.
