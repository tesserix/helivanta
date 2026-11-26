# Repeatable Tally accounting acceptance

Related: [#1690](https://github.com/tesserix/helivanta/issues/1690),
[owner guide](../design/1690-tally-accounting-export.md),
[PharmaConnect contract](../api/domains/pharmacy.md#tally-accounting-export-1690).

The Helivanta export is deployed and its UI/API pilot checks passed. Actual
TallyPrime import and independent ledger reconciliation remain pending. Installing
Tally or validating the source file does not satisfy that acceptance gate.

## Configure a test environment

Use an isolated, empty, zero-opening-balance company with synthetic records.
Record the Tally version, licence mode, exact company name, financial year,
currency, test period, source application revision and ledger-map version.
Confirm that the licence accepts the **original** voucher dates. Educational mode
may restrict dates; changing dates to evade that restriction invalidates the test.
Use an authorized licensed test company if the original dates cannot be imported.

For the current run, TallyPrime 7.2 is installed on `win-test-vm` and the company
is `Helivanta Qa 1690`, INR, FY 1-Apr-2026 to 31-Mar-2027. The company was created
without inventory/GST features for accounting-only acceptance. This is not an
inventory, statutory GST, tax eligibility or legal-books certification.

For another installation, select a different isolated company and use its own
expectations JSON. No VM name, address, company, date or ledger mapping is baked
into the verifier. The verifier is an offline Go CLI; it opens no network
connections, sends no credentials, creates no masters and imports no vouchers.
It does not add a new application endpoint, deployment or runtime dependency.

If remote access is needed, use the approved private connection. For the current
VM this is RDP through IAP. Do not expose RDP or Tally's HTTP interface publicly.
An HTTP-based import, if separately configured, must use a private tunnel and an
explicit target company. Retain its actual XML response. Never disable TLS
verification, print credentials, or assume a successful HTTP response means that
all vouchers were created. The offline checker supports file-based evidence from
either an authorized HTTP integration or a manual import/export workflow.

## Prepare source and expectations

1. Use an authorized Helivanta account, the intended tenant and exact outlet.
   Download through the existing Global API/UI with purchase bills included.
   Keep the original XML and its SHA-256 unchanged.
2. Export the needed-head list and configure the exact names in the target
   company. Review ledger groups and voucher-type settings with the test owner.
   Custom sale/credit-note types and Purchase/Payment/Receipt must preserve the
   source voucher numbers. Do not copy QA ledger grouping into real books.
3. Save a copy of [the current expectations](fixtures/tally-1690.json). Change the
   declared company, inclusive period, exact voucher-type counts and signed
   ledger balances for a different fixture. Currency is INR; configuration version
   is 1. Unknown fields are refused. Keep expectations independent of the observed
   import; do not recalculate them from a failed result to make it pass.
4. Check the source, from `backend/`:

   ```sh
   go run ./cmd/tallycheck \
     --config /secure/qa/expectations.json \
     --source /secure/qa/helivanta-vouchers.xml
   ```

   A successful run returns `status: source_validated`, source/configuration hashes
   and the voucher count. This proves source consistency only. The signed amount
   convention is the exported Tally convention: debit negative, credit positive.
   The fixture assumes zero opening balances and no unrelated transactions.

## Import once and collect independent evidence

1. Capture the selected company name and empty Day Book before importing. Back up
   the isolated company using Tally's supported backup workflow. Record who is
   performing the test and when.
2. Import the reviewed master fixture, if needed, separately from vouchers. Inspect
   its results before proceeding. Never mix master counts with voucher counts.
3. Import the unchanged voucher file **once** into the named company. Save the
   actual import-result XML or native `tally.imp`, plus import summary/errors and
   selected company screenshot. Do not manufacture an XML response from expected
   counts. A screenshot without a supported receipt still needs manual receipt
   review and cannot pass the CLI receipt gate.
4. If a timeout, disconnect or partial import occurs, stop. Inspect Day Book and
   import errors before deciding recovery. A stable `REMOTEID` does not establish
   Tally-side idempotency. Do not reimport automatically. Use a separately approved
   fresh company or Tally-supported recovery with retained evidence.
5. Independently export all imported vouchers for the exact period from Tally as
   XML. Include `REMOTEID`, date, type, number and ledger entries. Both
   `LEDGERENTRIES.LIST` and `ALLLEDGERENTRIES.LIST` representations are supported.
   Missing identities are refused; a number-only comparison is insufficient.
   An export format without those fields needs its own reviewed adapter rather
   than weakening the comparison. Keep source and observed files distinct.
6. Run reconciliation using the actual response and independently exported file:

   ```sh
   go run ./cmd/tallycheck \
     --config /secure/qa/expectations.json \
     --source /secure/qa/helivanta-vouchers.xml \
     --observed /secure/qa/tally-observed.xml \
     --import-response /secure/qa/tally-import-response.xml \
     > /secure/qa/reconciliation.json
   ```

   Exit zero with `status: vouchers_reconciled` requires exact voucher identities,
   dates, types, numbers and per-ledger amounts, matching source expectations,
   exactly the expected create count, and no alterations, deletions, ignored
   records, errors, cancellations, combinations or exceptions. Missing core
   counters, duplicate counters, duplicate identities, unbalanced entries,
   optional/cancelled vouchers and malformed/oversized evidence are refused.
   XML is bounded to 8 MiB, 5,000 vouchers and 50,000 entries; receipts/configuration
   to 64 KiB. There are no retries or writes to an accounting system.

## Complete the current acceptance gate

### Native TallyPrime evidence

For TallyPrime 7.2's native XML export, add `--observed-format tallyprime`.
The adapter decodes BOM-marked UTF-16 without replacement of malformed surrogates.
It escapes only the exact `&#4; Not Applicable` sentinel in five ignored metadata
fields: `CSTFORMISSUETYPE`, `CSTFORMRECVTYPE`, `VCHGSTCLASS`, `GSTCLASS` and
`GSTOVRDNISTAXONMRPAPPLICABLE`. It does not repair accounting values, identities,
other control references or malformed XML. Source XML remains under the strict
original parser. Evidence hashes always cover original bytes, not adapted text;
the result records the format and escaped-sentinel count.

For a manual import, retain the original `tally.imp`. Use `--import-log` instead
of `--import-response`, together with `--imported-file` set to the exact full
Windows path recorded in that log:

```sh
go run ./cmd/tallycheck \
  --config /secure/qa/expectations.json \
  --source /secure/qa/helivanta-vouchers.xml \
  --observed /secure/qa/tally-observed.xml \
  --observed-format tallyprime \
  --import-log /secure/qa/tally.imp \
  --imported-file 'C:\Helivanta-QA\1690\tally-vouchers.xml'
```

The supported native-log parser requires a complete, unique import block for
that file, the exact declared company, the expected create count and zero other
effects. Multiple attempts for the same file, missing/duplicate counters,
unsupported blocks, errors or truncated logs are rejected. Preserve the original
log; never trim failed attempts or manufacture an XML response. The native log
is receipt evidence, not cryptographic company provenance: independently inspect
the selected company and reports. `company_verified` deliberately remains false.

### First licensed import: failure retained

On 26 November 2026, the GOLD-activated QA installation accepted the original
25-November source dates. The master import created 21 and altered 3 masters,
with no errors/exceptions. The original voucher import created 20 vouchers with
zero alterations, deletions, combinations, ignored records, errors or exceptions.
Independent read-back nevertheless showed changed Payment, Receipt and Purchase
voucher numbers. **This run fails exact reconciliation.** A successful import
summary must not be reported as completed acceptance.

The exported built-in voucher types contain `NUMBERINGMETHOD=Manual` at the top
level but `Automatic / Auto Retain` in `VOUCHERNUMBERSERIES.LIST`. Validate the
effective series before importing; top-level settings alone are insufficient.
Keep the first company/evidence unchanged and use a separate empty synthetic
company for the next controlled attempt. The hosting and isolation review is
in [Tally hosting and tenant integration](../design/tally-hosting-and-tenant-integration.md).

For the 25-Nov-2026 fixture, require 20 vouchers: 6 sales, 8 credit notes,
3 purchases, 2 payments and 1 payment reversal. Inspect Day Book and supplier/bank
ledgers as well as the XML comparison:

| Evidence                                                    |            Expected INR |
| ----------------------------------------------------------- | ----------------------: |
| Local purchase: base + CGST + SGST                          |       100 + 9 + 9 = 118 |
| Interstate purchase: base + recoverable/nonrecoverable IGST |       100 + 6 + 6 = 112 |
| Union-territory purchase: base + CGST + UTGST               | 100 + 2.50 + 2.50 = 105 |
| Payments, reversal and net paid                             |       20 + 10 − 20 = 10 |
| LOCAL supplier closing credit                               |                     108 |
| IGST supplier closing credit                                |                     112 |
| UTGST supplier closing credit                               |                     105 |
| Bank net credit                                             |                      10 |

The CLI reports `declared_company` and `company_verified: false` deliberately:
voucher XML and a response do not independently establish which company produced
them. Review the company-name screenshot, opening state, date/licence acceptance,
Day Book, ledger balances, import receipt and hashes together before recording
remote acceptance or closing the issue. A source file copied into `--observed`
is not independent evidence. Financial data and screenshots belong in restricted
evidence storage; do not commit tenant exports, credentials or real personal data.

## Other systems and ongoing checks

This adapter verifies Tally accounting XML. Other Tally machines need different
configuration/evidence, not code changes. Another accounting system needs an
adapter for its actual import receipt and independently read-back postings,
including its company identity and replay rules. Preserve the same acceptance
sequence: scoped synthetic setup → original source → one controlled import →
authoritative read-back → exact reconciliation → retained evidence. Do not claim
compatibility from a generic configuration file alone.

Run the verifier's offline checks without starting Helivanta, Docker or Colima:

```sh
cd backend
go test -race -cover ./cmd/tallycheck
go vet ./cmd/tallycheck
go build -o /tmp/helivanta-tallycheck ./cmd/tallycheck
rm /tmp/helivanta-tallycheck
```

Unit tests use synthetic XML and temporary files. They do not certify a live
Tally installation. Preserve source, observed XML, import response, expectations,
reconciliation output and UI evidence as one run; record failures as failures.
