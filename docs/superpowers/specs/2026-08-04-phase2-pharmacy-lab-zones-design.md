# HMS Phase 2 — Pharmacy & Lab Zones, Two-Rail Chrome

Date: 2026-08-04
Status: approved
Builds on: `2026-08-04-hms-repo-setup-design.md` (phase 1, merged as PR #754)

## Goal

Replicate the medicore zone pattern for Pharmacy and Lab with a real
thin-slice domain, wire the spec's cross-zone journey (login → OPD visit
→ pharmacy dispense → lab result) through JetStream events, and replace
the duplicated single-panel `HmsShell` with a shared two-rail sidebar
(tesserix-home pattern and color tokens) consumed by every zone app.

## Decisions

- **D1 — Real thin-slice domain, not scaffolds.** Pharmacy and Lab get
  real entities (dispenses, orders) instead of cloned ping modules.
- **D2 — Full journey including a `medicore` backend module.** A small
  visits module publishes `visit_created`; pharmacy and lab consume it
  to create pending work. The `reference` module stays untouched as the
  wiring exemplar.
- **D3 — Two-rail chrome shared via `packages/ui` (`@hms/ui`).** Left
  icon rail switches zones; secondary panel shows the active zone's
  pages. Ported from tesserix-home's `AdminSidebar`, including its
  dark-slate `--sidebar-*` tokens. Zone nav lives in one registry file.
- **D4 — Consumers get the event's tenant GUC.** `events.Bus.handleMsg`
  sets `app.tenant_id` from the event envelope inside the consumer
  transaction so consumers can write tenant-scoped rows under forced
  RLS. Without this, D2 is impossible (phase 1 consumers only wrote
  non-tenant tables).
- **D5 — All sidebar links are plain `<a>` tags.** Cross-zone must be a
  hard navigation (phase 1 spec D3); using `<a>` uniformly keeps the
  shared component free of per-app `next/link` coupling.

## Platform extension: tenant-scoped consumers (D4)

In `backend/pkg/events/bus.go` `handleMsg`, after the idempotency claim
and before `c.Handle(...)`:

- If `evt.TenantID` parses as a UUID, run
  `SELECT set_config('app.tenant_id', ?, true)` in the same tx.
- If `evt.TenantID` is empty or invalid, leave the GUC unset (RLS then
  yields zero rows for tenant tables — same failure mode as today).

Covered by a new bus test: a consumer inserts into an RLS-forced table
and the row lands under the event's tenant; a second tenant sees
nothing.

## Backend modules

All three follow the `reference` module pattern: `platform.Module`
implementation in `backend/internal/modules/<name>/module.go`, forced
RLS + `USING`/`WITH CHECK` policy + `(tenant_id, created_at DESC)`
index on every tenant table, routes registered under the authenticated
`/v1` group, module tests mirroring `reference/module_test.go`.
Migration IDs are globally unique (`0001_medicore`, `0001_pharmacy`,
`0001_lab`).

### medicore

- Table `medicore_visits`: `id uuid PK`, `tenant_id uuid`,
  `patient_name text`, `department text CHECK (department IN
('OPD','IPD'))`, `status text DEFAULT 'open'`, `created_at`.
- `POST /medicore/visits` `{patient_name, department}` → creates the
  visit and publishes `hms.in.medicore.visit_created.v1`
  (`VisitCreated` v1, data: `{visit_id, patient_name, department}`) in
  the same transaction (outbox).
- `GET /medicore/visits` → newest-first list (limit 100).
- No consumers.

### pharmacy

- Table `pharmacy_medications`: `id`, `tenant_id`, `name`,
  `strength text`, `created_at`. `POST /pharmacy/medications`,
  `GET /pharmacy/medications`.
- Table `pharmacy_dispenses`: `id`, `tenant_id`, `visit_id uuid`,
  `patient_name text`, `medication text DEFAULT ''`, `status text
CHECK (status IN ('pending','dispensed')) DEFAULT 'pending'`,
  `dispensed_at timestamptz`, `created_at`.
- Consumer `pharmacy-visit-intake` on
  `hms.in.medicore.visit_created.v1`: inserts a pending dispense for
  the visit (idempotent via the platform's `processed_events` claim).
- `POST /pharmacy/dispenses/:id/dispense` `{medication}` → flips a
  pending row to `dispensed`, stamps `dispensed_at`, publishes
  `hms.in.pharmacy.dispense_recorded.v1` (`DispenseRecorded` v1, data:
  `{dispense_id, visit_id}`). 404 if not found (RLS makes cross-tenant
  identical to missing); 409 if already dispensed.
- `GET /pharmacy/dispenses` → newest-first list.

### lab

- Table `lab_orders`: `id`, `tenant_id`, `visit_id uuid`,
  `patient_name text`, `test_name text DEFAULT 'CBC'`, `status text
CHECK (status IN ('pending','completed')) DEFAULT 'pending'`,
  `result_value text`, `resulted_at timestamptz`, `created_at`.
  Result value is embedded — no separate results table this phase.
- Consumer `lab-visit-intake` on `hms.in.medicore.visit_created.v1`:
  inserts a pending order for the visit.
- `POST /lab/orders/:id/result` `{result_value}` → completes the order,
  stamps `resulted_at`, publishes `hms.in.lab.result_ready.v1`
  (`ResultReady` v1, data: `{order_id, visit_id}`). 404 / 409 as above.
- `GET /lab/orders` → newest-first list.

### Wiring & tests

- `cmd/api/main.go` registers medicore, pharmacy, lab after reference.
- Per-module tests (testcontainers) covering routes, RLS isolation, and
  the 409 transitions.
- One integration test: create a visit via the medicore route → assert
  a pending pharmacy dispense AND a pending lab order appear for that
  tenant (and not for another tenant) via JetStream delivery.

## Shared chrome: `packages/ui` (`@hms/ui`)

New workspace package `packages/ui`:

- `src/hms-shell.tsx` — client component, two-rail desktop sidebar
  ported from tesserix-home `AdminSidebar`, simplified: no mobile
  drawer, no collapsible groups, no tooltips dependency if `@tesserix/web`
  Tooltip is unavailable to the package (fall back to `title` attrs).
  - Left rail (`w-16`, `bg-sidebar`): HMS mark, zone icons
    (lucide-react: LayoutDashboard=Dashboard, HeartPulse=MediCore,
    Pill=Pharmacy, FlaskConical=Lab), sign-out at bottom.
  - Secondary panel (`w-56`, `bg-sidebar`): active zone label + flat
    page list. Active state via `aria-current="page"`, styling
    `bg-sidebar-accent` exactly as tesserix-home.
- `src/zones.ts` — single registry:
  `[{key, label, icon, href, pages: [{label, href}]}]` for shell (`/`),
  medicore (`/medicore` → OPD `/medicore/opd`, IPD `/medicore/ipd`),
  pharmacy (`/pharmacy` → Dispenses `/pharmacy`, Medications
  `/pharmacy/medications`), lab (`/lab` → Orders `/lab`). Adding a zone
  is one entry here.
- `styles.css` — tesserix-home sidebar tokens layered after
  `@tesserix/web/styles`:
  `--sidebar:#0f172a; --sidebar-foreground:#e2e8f0;
--sidebar-primary:#ffffff; --sidebar-primary-foreground:#0f172a;
--sidebar-accent:#1e293b; --sidebar-accent-foreground:#f1f5f9;
--sidebar-border:#1e293b; --sidebar-ring:#ffffff;` (light theme;
  HMS is light-only for now, matching phase 1).
- Consumed as a source package (`"@hms/ui": "workspace:*"`, apps'
  Tailwind `@source` includes `../../packages/ui/src`) — no build step,
  matching how zone apps already scan `@tesserix/web`.
- The per-app `components/hms-shell.tsx` copies in shell and medicore
  are deleted; all four apps render `HmsShell` from `@hms/ui` with
  `active` = current path.

## Zone apps

- `apps/pharmacy`: port 4303, `basePath: "/pharmacy"`, config cloned
  from medicore. Pages: `/` (dispenses list: patient, medication input,
  Dispense button; pending rows highlighted) and `/medications`
  (create + list). Data via same-origin `/api/*` fetches with
  credentials, as medicore's ping panel does today.
- `apps/lab`: port 4304, `basePath: "/lab"`. Page: `/` (orders list;
  pending rows get an inline result input + Save that POSTs the
  result).
- `apps/medicore`: OPD page gains a "New visit" form (patient name,
  department fixed OPD) + visit list above the existing ping panel.
- `apps/shell` `next.config.ts`: add pharmacy and lab rewrites
  (`PHARMACY_URL` default `http://localhost:4303`, `LAB_URL` default
  `http://localhost:4304`), mirroring medicore's two-line pattern.
- Makefile/README/`packages/config` untouched except docs mentioning
  the new ports; `turbo dev` picks the new apps up via workspace globs.

## E2E

Extend the Playwright smoke to the full spec journey: login → create
OPD visit in `/medicore/opd` → `/pharmacy` shows the pending dispense →
dispense it → `/lab` shows the pending order → enter a result → order
shows completed. Event propagation is asynchronous — the test polls the
zone pages (Playwright auto-retrying expects) rather than assuming
immediacy.

## Out of scope

- OpenFGA authorization model (issue #1) — authn-only, as phase 1.
- IPD workflows beyond the existing stub page.
- Medication stock/inventory tracking; result reference ranges.
- Mobile apps; mobile drawer for the new sidebar.
- Consuming `dispense_recorded` / `result_ready` anywhere — published
  for future phases, only asserted in module tests.
