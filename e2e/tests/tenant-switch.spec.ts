import { expect, test } from "@playwright/test";
import { login, specAdmin } from "./support/login";

// Tenant switching, proved by what the *new* session can do rather than
// by the toast. `make seed` gives this spec's own admin account two
// hospitals with deliberately different roles (scripts/seed-dev.mjs):
// tenant_admin in the first, pharmacist in the second. So a switch that
// really re-mints
// the session visibly costs this user the MediCore and Lab zones, while
// a switch that only toasts and reloads — the bug this journey guards —
// leaves the old tenant_id claim in the cookie and every zone still
// there.
const SECOND_TENANT_ID = "22222222-2222-2222-2222-222222222222";

// The permission-resolution endpoint answers for whichever tenant the
// session's token claims, so it is the most direct read of "which
// hospital am I actually in" available to the browser.
async function permissions(
  page: import("@playwright/test").Page,
): Promise<string[]> {
  // The switch reloads the page, and the poll below reads permissions
  // across exactly that window — so an evaluate can be torn down mid-flight
  // with "Execution context was destroyed". That is a navigation, not an
  // answer: it must be retried once the new document is up, never reported
  // as a permission set. Returning a sentinel instead would satisfy the
  // `.not.toContain` assertion for the wrong reason, which is the failure
  // mode worth avoiding. expect.poll does not retry a callback that throws,
  // so the retry has to live here.
  for (let attempt = 0; attempt < 10; attempt++) {
    try {
      return await page.evaluate(async () => {
        const res = await fetch("/api/v1/iam/me/permissions");
        const body = await res.json();
        return body.data as string[];
      });
    } catch (err) {
      if (!/Execution context was destroyed/.test(String(err))) throw err;
      await page.waitForLoadState("domcontentloaded");
    }
  }
  throw new Error(
    "the permissions probe was destroyed by a navigation 10 times running; the page is reloading in a loop, not switching hospital",
  );
}

test("switching hospital re-mints the session and changes what the user can do", async ({
  page,
}) => {
  await login(page, specAdmin());

  const zoneNav = page.getByRole("navigation", { name: "Zones" });
  await expect(zoneNav.getByRole("link", { name: "MediCore" })).toBeVisible();
  expect(await permissions(page)).toContain("medicore.visit.read");

  // The picker only renders for multi-hospital users, so its presence is
  // itself part of the seed contract.
  const picker = page.getByLabel("Hospital");
  await expect(picker).toBeVisible();
  await picker.selectOption(SECOND_TENANT_ID);

  // The switch reloads once the new session cookie is in place.
  await expect(page.getByRole("heading", { name: "Departments" })).toBeVisible({
    timeout: 30_000,
  });

  // The assertion that fails if the switch is a no-op: in the second
  // hospital this user is only a pharmacist.
  await expect
    .poll(async () => permissions(page), { timeout: 30_000 })
    .not.toContain("medicore.visit.read");
  expect(await permissions(page)).toContain("pharmacy.dispense.read");

  await expect(zoneNav.getByRole("link", { name: "Pharmacy" })).toBeVisible();
  await expect(zoneNav.getByRole("link", { name: "MediCore" })).toHaveCount(0);
  await expect(zoneNav.getByRole("link", { name: "Lab" })).toHaveCount(0);
});
