# Prescription register for scheduled-drug supplies (#1062)

Resolves: #1062. Builds on prescription-linked sales (#1386 B-2b), the
Schedule H1 register (#1386 C-2) and credit notes (#1386 C-1).

## Why

The counter keeps a separate Schedule H1 register (Rule 65(3)(h)). The
legacy screens (reports 451 and 722) also list every scheduled-drug supply by
date, batch, expiry, quantity, quantity returned, schedule and doctor. Rule
65(3)(1) requires that same record for every supply made on a prescription.
Today the outlet has no such view for Schedule G or H. The counter-sale spec
(C-2) treated the invoice as that record, but the invoice does not carry the
prescriber's or the patient's name and address.

## The law, as read (2026-11-03)

Drugs Rules 1945, Rule 65(3), CDSCO 2022 compilation (knowledge source S09),
pages 41–42:

- **65(3)(1):** the supply of any drug, other than Schedule X, on a registered
  medical practitioner's prescription is recorded at the time of supply in a
  prescription register. The serial number of the entry is entered on the
  prescription. Each entry gives:
  - (a) its serial number;
  - (b) the date of supply;
  - (c) the prescriber's name and address;
  - (d) the patient's name and address;
  - (e) the drug and quantity;
  - (f) for Schedule C, H and H1, the manufacturer, batch number and expiry;
  - (g) the signature of the registered pharmacist who supplied it.
- **Cash memo instead:** for drugs supplied in the original containers, the
  same particulars may be kept in serially numbered cash or credit memo books
  instead. The outlet opts for this in writing to the Licensing Authority
  (65(3)(2)).
- **Proviso (ii):** a drug other than Schedule C, H or H1 that is supplied in
  the manufacturer's original unopened container need not be entered. The
  prescription must then be stamped with the supplier's name and the date.
- **65(3)(h):** H1 supplies also go in a separate register. That register
  already exists and is unchanged.

## What it does

- **A read, never a copy.** The prescription register is read from the
  insert-only records: each sale line that cites a prescription (classes G, H
  and H1). It cannot be edited and cannot drift from the facts, as the H1
  register cannot.
- **Each entry shows:**
  - the serial (invoice number and line) and the date of supply;
  - the prescriber's name, registration number and address;
  - the patient's name and address, and the registered patient id when the
    sale named one;
  - the product and generic name, and the class;
  - the manufacturer, batch and expiry;
  - the units supplied and the units returned since on credit notes;
  - the pharmacist who sold it.
- **Missing addresses.** A Schedule G prescription may lack them, because only
  H and H1 sales require them (B-2b). Such an entry shows each one as not
  recorded, rather than hiding the supply.
- **Filters:**
  - a period of at most 366 outlet days (required);
  - the class (G, H or H1; all by default);
  - a product;
  - a registered patient.
- **Routes:**
  - `GET …/registers/prescriptions` is paged, newest first.
  - `GET …/registers/prescriptions/export` is a CSV, oldest first, refused
    above 1,000 entries.
  - Both require `pharmacy.prescription.read` and the outlet grant, as the H1
    register does.
- **Every read and export is audited,** in the read's own transaction (as the
  H1 register is), with the filters going to the structured audit log.
- **The counter's endorsement note** names the serial to write on the
  prescription: the invoice number and line.

## Activation review

These go to the outlet's registered pharmacist and compliance reviewer before
the outlet relies on the register:

- whether the outlet keeps the register, or has opted for cash memos with the
  Licensing Authority;
- whether invoice-and-line is an acceptable serial;
- whether Schedule G supplies without addresses meet proviso (ii) at that
  outlet (original unopened containers, stamped prescriptions).

## Not covered

- Schedule X and NDPS registers (#165, #171); Schedule C (not sold at the
  counter).
- Searching by patient name.
- Printing as a bound register.
- Sales to other licensees (65(4)).
