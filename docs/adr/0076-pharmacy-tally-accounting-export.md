# ADR-0076: Tally accounting export from the pharmacy's own ledgers

Status: the original export core is deployed to the bounded pilot. The purchase
evidence extension is in development, not deployed. Refs #1690; current design:
[purchase completion](../superpowers/specs/2026-11-26-tally-purchase-completion.md).
Design: [spec](../superpowers/specs/2026-11-21-tally-accounting-export-design.md).

## Context and ownership

PharmaConnect owns the outlet's invoices, credit notes and supplier settlements.
Pharmacies keep their statutory books in Tally. An export must not become a
second ledger, guess ledger names, or invent a GST split the records do not hold.

## Decision

1. **The export is a read of immutable documents.** One accounting voucher per
   sale, credit note, supplier payment and payment reversal, in Tally's
   `Import Data` envelope. Amounts are the records' exact minor units. GST is
   split by the rate each line recorded. Nothing the export writes is stored
   back, and no table holds exported totals.
2. **Ledger names are the pharmacy's own, versioned per outlet.** A ledger map
   is an immutable record. Each new version names the one it replaces, and a
   unique key allows one successor, so concurrent edits are a 409 rather than
   a lost update. Heads follow a closed grammar, and rates come from the data,
   never from code. The export refuses while a needed head is unnamed, and
   refuses a `ledger_map` other than the one the client previewed, so the
   file never carries names its exporter did not see.
3. **Identity is stable.** `REMOTEID` is `helivanta-<document uuid>` and the
   voucher number is the document's own number. With deterministic ordering,
   the same records and map give byte-identical files.
4. **Owner-only, audited.** `pharmacy.accounts.export` belongs to the owner
   role, because the file combines sale-read and accounts-read data. Each
   export commits an audit row in its read transaction and logs its sha256.

## Options rejected

- **Purchase vouchers with one unsplit input-tax entry.** This would misstate
  input tax credit. The purchase extension resolves the missing evidence with
  immutable source-invoice classifications, explicit component recoverability
  and exact original-total validation; it never infers legal ITC eligibility.
- **Daily summary vouchers.** These are smaller files, but lose invoice-wise
  numbers and identities, so a re-import or audit cannot match an invoice.
- **Default ledger names** such as "Sales" or "Output CGST". A wrong default
  imports silently into the wrong ledger, so the export fails closed instead.
- **Pushing to a running Tally over HTTP.** That would need network reach into
  the pharmacy's LAN and Tally credentials. A file import keeps Tally offline.
- **A mutable, upserted map.** A mutable map loses which names an earlier
  export used. The audit row records the immutable map id instead.

## Consequences

- A busy counter exports a shorter period (5,000 vouchers, 8 MiB bound).
- Tally-side de-duplication on re-import depends on the importer's overwrite
  setting, so the screen says to import each file once.
- A real TallyPrime import remains an unexecuted acceptance item.
- IGST and UTGST output heads are implemented and pilot-tested with #1689.
  Purchase heads and source evidence are the current #1690 extension. Supplier
  credit settlements are distinct from sales credit notes and remain outside
  this document-voucher scope; they are not silently treated as cash payments.
