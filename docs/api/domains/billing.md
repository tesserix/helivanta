# Hospital billing

[Contract index](../README.md) · slice 1 implemented: admission accounts.

The `billing` module owns the hospital's ledger (#193). Other modules post to it through
published events; none writes its tables. Hospital invoices, tariffs, interim and final bills,
deposits and discounts are not built yet (#193, #197, #199, #200).

## Admission accounts (#197 slice 1)

- **One account per admission**, opened by whichever event names the admission first:
  `medicore.bed_occupancy_started.v1`, or a pharmacy charge. It closes on
  `medicore.admission_released.v1`. An account keeps its hospital and patient; an event naming
  the admission with another patient is refused.
- **Entries are append-only:** `charge` or `reversal`, keyed on (source module, source
  reference, kind), so a replay never posts twice, and a replay that differs is refused. A
  reversal names a recorded charge of the same admission and currency, and never exceeds it,
  however many arrive at once: the database takes them one at a time per charge.
- **A reversal that arrives before its charge is parked** in `billing_parked_reversals`, not
  refused (the bus would dead-letter it), and applied when the charge is recorded. Charge,
  parking and applying are serialised per charge.
- **A late charge**, after release, is still recorded. The account stays closed.
- **First source:** pharmacy's `admission_charge_posted.v1` and `admission_charge_reversed.v1`
  ([pharmacy](pharmacy.md#ward-medicine-billed-to-the-admission-1387)). The pharmacy GST
  invoice remains the tax document; the entry records that the patient owes the hospital that
  amount against it, so revenue is counted once.

| Route                                                                           | Behaviour                                                                              |
| ------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `GET /billing/hospitals/:hospital/admissions/:admission/account`                | Status, open and close times, and totals per currency: charged, reversed, outstanding. |
| `GET …/admissions/:admission/entries`                                           | A page of entries, newest first.                                                       |
| `PUT` and `DELETE /billing/hospitals/:hospital/access/:subject`; `GET …/access` | Billing grants.                                                                        |

- **Reading** needs `billing.account.read` (nurse, receptionist) plus a billing grant at the
  hospital, locked through the request. Without the grant the answer is `404`.
- **Grants** need `billing.access.manage`, which only tenant administrators hold.
- **Row security** shows a reader only the hospitals they hold a grant for. Only billing's named
  consumers write.
