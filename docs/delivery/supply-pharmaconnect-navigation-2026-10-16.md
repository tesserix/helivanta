# Supply Connect and PharmaConnect navigation repair

Status: implemented and validated locally; user approved commit, merge after required checks, and production rollout. Hosted checks and release evidence follow below. Source branch `fix/pharmaconnect-workspace-navigation`, based on main `db7376f`.

## Reported problem and findings

The production browser was signed in as the intentionally restricted independent licence reviewer. Its Supply compliance permissions did not include pharmacy operations. Normal sign-out and sign-in using the existing YES HOSPITAL test owner restored the pharmacy, clinical and administrative navigation; no role, object grant or production setting was changed. Production catalogue displayed TEST Release Pharmacy, TEST Release Outlet and TEST Release Tablet from the real API.

Separately, the navigation registry called the module Pharmacy and gated the entire zone on dispensing permission. A catalogue-permitted account therefore lost its valid catalogue link. A failing regression reproduced this (`expected undefined to be PharmaConnect`).

## Change and integration boundary

The registry now labels the module PharmaConnect. It still filters every page by its own permission; the module appears only when at least one page is permitted, and its entry URL falls back to the first permitted page. Default dispense bookmarks and all existing URLs are preserved. This is navigation only; it does not grant API permissions or outlet access.

A shared Connected workspace panel links Supply Connect to the user's permitted dispensing, catalogue, receiving, expense/audit and medication screens. All five pharmacy screens link back to the authorised supplier directory. Restricted reviewers see an explicit pharmacy-access explanation with no pharmacy links or data requests. Dashboard descriptions now explain the existing pharmacy and supplier screens.

Assets are private supplier profiles, medicine/inventory and financial records. Relevant threats are another tenant or a user with narrower permissions. The existing verified API session, per-route permissions, explicit party/outlet grants and forced PostgreSQL RLS remain the trust boundary; a UI link is never authorization. No endpoint, schema, identity grant, trading eligibility, event contract or NATS/Temporal behavior changed. The existing permission query is shared; the panel adds no new unique API endpoint.

## Commands actually run

- Red: `pnpm --filter @helivanta/ui test src/zones.test.ts` failed on the missing catalogue-only PharmaConnect entry; green rerun passed all 8 tests.
- New connection tests initially failed because the requested panel did not exist. After implementation, the connection, Supply workspace and pharmacy workspace suites passed all 17 tests.
- `pnpm -r test`: passed 182 API, 91 shared UI, 295 shell and 2 script tests (570 total).
- `pnpm -r lint`: passed, including the updated navigation E2E source.
- `pnpm -r type-check`: passed, including generated-contract checks.
- `pnpm --filter @helivanta/shell build`: passed with required local public OIDC configuration. First build without that configuration failed its environment check; no validation was disabled.
- Prettier check on all changed code/test files and `git diff --check`: passed.

Full checks used Node 24. The first focused red/green runs used the shell's Node 26 default before selecting the required Node 24 path. No Go code changed; Go, migrations and dependency versions were untouched. Full Go tests were not rerun for this UI repair. The existing separate main CI reaper defect remains tracked in #1176.

## Browser acceptance

The updated production build ran on `http://localhost:4308`. A temporary, loopback-only read-only fixture outside tracked source forwarded permitted GET requests to the actual production API with the existing synthetic owner's session kept server-side. All mutation methods were rejected locally (verified PUT -> 405). No credentials, fixture route, proxy or production session is part of the product diff. This is local frontend-to-real-API acceptance, not a new local database CRUD or production authentication test.

Observed through browser controls:

- Supply directory loaded the persisted synthetic Pakistan distributor and showed PharmaConnect links with expanded navigation.
- Catalogue showed the saved pharmacy, outlet, SKU and pending regulatory review state.
- Receiving opened for the same pharmacy/outlet and its product selector enabled after data loaded.
- Expenses/audit loaded TEST-BILL-20261016-001: INR125.25 billed, INR25.10 paid, INR0 credits and INR100.15 outstanding. Inventory audit loaded 25 received/on-hand tablets, batch TEST-LOT-20261016, expiry2030-12-31 and INR125.25 acquisition/stock value.
- Medications rendered its real empty formulary state; dispensing opened its queue.
- The reverse supplier link returned to the directory.
- At390x844, document and body width both390 with all five workflow links rendered in one column. Desktop viewport restored. Production owner and local preview tabs kept open.
- Browser console-log collection was unavailable because the browser extension requested an update. No claim of a completed console audit is made.

Desktop screenshot: `/tmp/helivanta-supply-pharma-connected-local.png`. Mobile screenshot: `/tmp/helivanta-supply-pharma-mobile-local.png`.

## Remaining scope and release

This repair connects the existing implemented screens. Broader B2B offers/orders, fulfilment, country-approved regulatory/trading policy and other open #1133/#1135/#979 stories are not completed by navigation. The legacy formulary/dispensing slice is not claimed to implement a full inventory-deducting B2B workflow. No real customer data was copied or changed.

The production browser now uses the ordinary authorised test owner. The user approved commit/push/merge and production rollout for this repair on 2026-10-16. Required hosted checks must pass before merge; #1176 remains a separate CI defect. No schema migration is required.
