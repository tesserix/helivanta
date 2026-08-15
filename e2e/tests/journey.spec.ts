import { expect, test } from "@playwright/test";
import { login, specAdmin, specPharmacist } from "./support/login";

// The cross-zone journey with authorization actually enforced: an admin
// creates a visit that fans out to pharmacy and lab, and a
// differently-privileged pharmacist sees only what pharmacist holds
// permission for — in the sidebar, on the dashboard, and when navigating
// directly to a route the UI would otherwise hide the action on.

test("admin creates a visit and it lands in pharmacy and lab", async ({
  page,
}) => {
  await login(page, specAdmin());

  const patient = `Journey Patient ${Date.now()}`;
  await page.locator('a[href="/medicore/opd"]').first().click();
  await expect(page).toHaveURL(/\/medicore\/opd$/);
  await page.waitForLoadState("networkidle");

  await page.getByLabel("Patient name").fill(patient);
  await page.getByRole("button", { name: "Create visit" }).click();
  await expect(page.getByText(patient).first()).toBeVisible();

  // visit_created flows through JetStream into both consumers.
  await page.goto("/pharmacy");
  await expect(page.getByText(patient).first()).toBeVisible({
    timeout: 30_000,
  });

  await page.goto("/lab");
  await expect(page.getByText(patient).first()).toBeVisible({
    timeout: 30_000,
  });
});

test("pharmacist sees only the pharmacy zone, in the sidebar and on the dashboard", async ({
  page,
}) => {
  await login(page, specPharmacist());

  // Scoped to the zone rail (aria-label="Zones") — see smoke.spec.ts for
  // why an unscoped query is ambiguous even though the dashboard's cards
  // (apps/shell/app/page.tsx) apply the same visibleZones(can) filter.
  const zoneNav = page.getByRole("navigation", { name: "Zones" });
  await expect(zoneNav.getByRole("link", { name: "Pharmacy" })).toBeVisible();
  await expect(zoneNav.getByRole("link", { name: "MediCore" })).toHaveCount(0);
  await expect(zoneNav.getByRole("link", { name: "Lab" })).toHaveCount(0);

  // The dashboard is the first thing every user sees and must honour the
  // same "only doors that will open" contract (packages/ui/src/zones.ts)
  // as the sidebar — scoped by href since the dashboard's card titles are
  // per-page ("OPD"/"IPD"), not the zone name.
  const dashboard = page.getByRole("main");
  await expect(dashboard.locator('a[href="/pharmacy"]')).toBeVisible();
  await expect(dashboard.locator('a[href^="/medicore"]')).toHaveCount(0);
  await expect(dashboard.locator('a[href="/lab"]')).toHaveCount(0);
});

test("pharmacist cannot create a visit even by navigating directly", async ({
  page,
}) => {
  await login(page, specPharmacist());
  await page.goto("/medicore/opd");
  await page.waitForLoadState("networkidle");

  // The UI hides the action entirely (Can permission="medicore.visit.create"
  // in apps/medicore/components/visit-panel.tsx); the API is the real
  // enforcement point, so the form must be absent rather than merely
  // disabled.
  await expect(page.getByRole("button", { name: "Create visit" })).toHaveCount(
    0,
  );
});

test("pharmacist can dispense a visit created by admin", async ({ page }) => {
  // Self-contained rather than relying on a pending dispense left over by
  // another test: create the visit as admin, hand off to the pharmacist,
  // and prove the dispense actually completes — not just that the button
  // is present.
  await login(page, specAdmin());

  const patient = `Journey Dispense ${Date.now()}`;
  await page.locator('a[href="/medicore/opd"]').first().click();
  await expect(page).toHaveURL(/\/medicore\/opd$/);
  await page.waitForLoadState("networkidle");
  await page.getByLabel("Patient name").fill(patient);
  await page.getByRole("button", { name: "Create visit" }).click();
  await expect(page.getByText(patient).first()).toBeVisible();

  // /logout is a same-origin-checked POST now, not a GET a test (or an
  // attacker's <img> tag) can navigate to directly — see
  // apps/shell/app/logout/route.ts and #781. Every zone renders the same
  // HmsShell sign-out control, including the medicore page we're on.
  await page.getByRole("button", { name: "Sign out" }).last().click();
  // Sign-out (lib/sign-out.ts) is a MULTI-HOP redirect chain: HMS
  // dashboard -> Zitadel's end_session endpoint (which is what actually
  // clears Zitadel's own SSO cookie) -> back to HMS's /login -> /login's
  // own effect immediately redirects onward to Zitadel's authorize
  // endpoint. Calling login() (which itself starts with a fresh
  // page.goto("/")) before that chain has fully settled does not merely
  // race a URL assertion — it can WIN the race and cancel the
  // still-pending navigation to end_session outright, so Zitadel's
  // session is never actually cleared. The very next authorize() call
  // then succeeds silently, using admin's still-live SSO session instead
  // of prompting the pharmacist for real credentials — observed live in
  // an earlier version of this wait, which raced on a URL check instead
  // and reached the dashboard as admin without ever calling login() at
  // all.
  //
  // The provable, unambiguous signal that the chain finished — and that
  // Zitadel's SSO session was genuinely cleared, not silently reused — is
  // the credential form itself actually rendering. A manual trace of this
  // exact sequence (docs kept in the PR body) confirms sign-out's
  // end_session call carries `id_token_hint` and Zitadel does land back on
  // a real Loginname prompt once it's processed; waiting for that prompt
  // here, rather than for an intermediate URL Playwright may not observe
  // as a distinct "landed" state, is what makes this deterministic.
  await expect(page.getByLabel(/Loginname|Email|Login Name/i).first()).toBeVisible({
    timeout: 20_000,
  });
  await login(page, specPharmacist());

  await page.goto("/pharmacy");
  await expect(page.getByText(patient).first()).toBeVisible({
    timeout: 30_000,
  });
  await page
    .locator("li", { hasText: patient })
    .getByRole("button", { name: "Dispense" })
    .click();
  await expect(
    page.locator("li", { hasText: patient }).getByText(/Dispensed/),
  ).toBeVisible();
});
