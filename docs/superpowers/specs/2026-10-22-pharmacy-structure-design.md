# PharmaConnect as pharmacy management

Resolves #1258 (part: structure and the pharmacy one-look tidy-up). Follows #1255 (one look) and #1275 (one panel surface).

## Problem

PharmaConnect reads as a set of unrelated screens, not one pharmacy management product:

- **Every page opens with an advert for another product.** A "Connected workspace: Supply Connect" card heads Dispenses, Medications, Product catalogue, Stock receiving and Expenses. For a pharmacist it begins "This account does not have…".
- **One page holds the whole inventory.** "Stock receiving" stacks eight sections:
  - the stock register;
  - batch safety;
  - lot registration;
  - locations;
  - movements;
  - reconciliation;
  - corrections;
  - the outlet access form.
- **The page menu shows only five page names, with repeated icons.** Two pages use a pill and three use a clipboard. It has no grouping. Under the hood, the backend already supports stock positions, receiving, expiry and safety, the catalogue, supplier payables, and outlet setup.
- **The dispense queue mixes finished and waiting rows.** It is one newest-first list, so dispensed rows sit above the pending ones a pharmacist must act on. Splitting it in the browser would be wrong: pending rows beyond the first page would never show.
- **Patients and Staff share one rail icon,** and Workforce repeats a gear on six pages and a calendar on seven.

## Decisions

- **D1 Page sections.** `ZonePage` gains an optional `section` label. The page panel renders a plain heading (`text-xs font-medium text-muted-foreground`, sentence case) before the first page of each section. A `zones.test.ts` check keeps each zone's sections contiguous, so the headings cannot repeat.
- **D2 PharmaConnect's pages**, in order:

  | Section    | Page              | Route                   | Permission                 | Content                                 |
  | ---------- | ----------------- | ----------------------- | -------------------------- | --------------------------------------- |
  | Dispensing | Dispense queue    | `/pharmacy`             | `pharmacy.dispense.read`   | D4                                      |
  | Dispensing | Medication list   | `/pharmacy/medications` | `pharmacy.medication.read` | unchanged                               |
  | Inventory  | Stock on hand     | `/pharmacy/stock`       | `pharmacy.stock.read`      | stock register, reconciliation, export  |
  | Inventory  | Receiving         | `/pharmacy/receiving`   | `pharmacy.stock.read`      | lots, locations, movements, corrections |
  | Inventory  | Expiry & safety   | `/pharmacy/safety`      | `pharmacy.stock.read`      | batch safety concerns                   |
  | Inventory  | Product catalogue | `/pharmacy/catalogue`   | `pharmacy.catalogue.read`  | unchanged                               |
  | Purchasing | Supplier bills    | `/pharmacy/accounts`    | `pharmacy.accounts.read`   | payables, payments, inventory audit     |
  | Setup      | Outlets & access  | `/pharmacy/setup`       | `pharmacy.access.manage`   | pharmacy/outlet registry, outlet access |

  The landing page stays the dispense queue: it is the pharmacist's daily work, and `/pharmacy` is what the smoke and journey specs use. The Sales section (India first, with tax configurable per country) is #172/#1147, a separate slice. It adds its pages under a "Sales" section here.

- **D3 One scope header.** All outlet-scoped pages, Inventory to Setup, share one `PharmacyWorkspace`. Its pharmacy and outlet pickers sit in the page heading, as today. These go:
  - the Supply Connect card (pharmacy pages only; Supply keeps its link to PharmaConnect);
  - the outlet caveat box.

  Setup holds the registry forms and the outlet access grant, which were appended to every page before.

- **D4 The queue is split on the server.** `GET /pharmacy/dispenses` takes an optional `status` (`pending` | `dispensed`). Any other value is a 400, failing closed instead of returning everything. The page shows "Waiting" (pending) and then "Dispensed", both newest first: the shared keyset pagination (`tenantdb.ApplyKeyset`) has one order, and an oldest-first queue needs a second keyset direction in the platform, which is not covered here. Each is its own paged `QueryState`. The OpenAPI contract and the contract docs record the parameter.
- **D5 Distinct icons.** Every rail zone, and every page within a zone, has its own icon. `zones.test.ts` enforces this for all zones, including Workforce, so it does not drift.
- **D6 Pharmacy files leave the surface legacy list** (#1275 D4): `pharmacy/*` is converted to `Panel`.

## Not covered

- Sales, billing, returns and tax (#172, #1147).
- An overview page with pharmacy KPIs. It needs measures the backend does not publish yet (a sales total, expiry counts by window). It is not filled with zeroes.
- The rail's own layout (a separate issue).
