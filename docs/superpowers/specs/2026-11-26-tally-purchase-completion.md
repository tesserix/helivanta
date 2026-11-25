# Tally purchase voucher completion (#1690)

Status: design for implementation, not deployed. Extends ADR-0076 and the
2026-11-21 design. PharmaConnect remains the owner of bill and settlement data.

## Verified gap

The existing export excludes posted supplier bills because their line amounts
have no GST rate/component evidence. Inferring tax or ITC from current catalogue
rates would rewrite historical evidence and violate IN-RC-04/10/11.

## Decision

Add append-only purchase-tax evidence per bill, independently versioned with a
replaces pointer, one successor per version, mandatory reason and intent hash.
Record supplier GSTIN (nullable for unregistered suppliers), supplier state,
place of supply, and one classification per original line: position, nature,
rate, four tax components and the recoverable portion of each component.
Validate exact sums against the original line, component jurisdiction rules,
rate/nature and positive GST needing a valid supplier registration. Values are
source-invoice evidence, not computed eligibility or a tax-rate recommendation.
Keep original bill totals, stock valuation and settlement history unchanged.

GET /accounts/payables/:id/purchase-tax returns current evidence or null;
PUT /accounts/payables/:id/purchase-tax/:evidence records a version. Both need
existing accounts-export permission and exact outlet access; writes need Editor
and expected-subject protection. Lock/version and content hashes enforce replay,
stale/conflicting intent refusal. Both tables have forced RLS, scoped foreign
keys, audit and operating-purpose restrictions. Cross-scope objects remain 404.

Include purchase invoices by issued_on in the existing bounded document query.
Fail with actionable missing-evidence error rather than silently omit bills.
Debit category/nature/rate heads for net plus landed, recoverable input-tax heads
per component/rate and separate nonrecoverable tax expense heads; credit the
supplier with original gross. Preserve stable identity, exact balancing,
deterministic ordering, map fence, bounded XML and atomic audited file release.

Expose evidence on bill detail, with a scoped versioned form. Preview counts
purchases and maps new heads. Gate new routes with pharmacy_payables. Canonical
models remain in contracts/pharmacy.json; regenerate SDK/backend/release docs.

## Verification and limits

Red/green tests cover grammar, classification validation, exact postings,
nonrecoverable tax and missing evidence. Owner HTTP/database tests cover RLS,
replay, concurrent successors, tampering, audit and repeated-file equality.
SDK/component tests cover forms, validation, scope and missing-evidence recovery.
No local apps, databases, Docker or Colima. Use focused local pure tests/builds
and CI-backed services for integration. Real TallyPrime import and live rollout
are not claimed without actual access, specific authorization and evidence.

The purchase extension also retains `pharmacy_tally_export_receipts`: an immutable,
forced-RLS record of each delivered file's period, ledger map, voucher count,
file SHA-256 and source-voucher fingerprint. It commits with the audit event before
file release; a failed receipt/audit commit releases no file. The receipt holds
no XML, patient details or duplicated monetary ledger.

Compatibility: the updated SDK requests `purchase_bills=true` for preview and
file download, verifies the full preview schema and requires the matching
`X-Export-Purchase-Bills: true` header. Omitting this option preserves the legacy
four-count response and sales/credit/payment-only file; it does not silently add
purchases to existing integrations. The option accepts one `true` or `false`
value; malformed/repeated values are 400. Export receipts record the chosen mode.
The original map/permission/date/bounds contracts remain unchanged. Consumers
wanting complete purchase export must use the updated SDK or opt in explicitly.

Purchase-enabled exports additionally cap source detail at 50,000 rows; they refuse an oversized
period before building vouchers. XML encoding checks its byte bound as each
voucher is written, returning no partial file. Detailed bills can therefore need
a shorter period even below the 5,000-document limit.

Purchase import identities use UUIDv5 in the URL namespace over
`https://helivanta.com/tally/purchase/<verified-tenant>/<verified-outlet>/<bill-id>`,
prefixed `helivanta-`. They remain distinct across tenants/outlets and from a
sale reusing the same document UUID. Existing sale/credit/payment identities
are unchanged. Source parts join by document kind as well as ID.
