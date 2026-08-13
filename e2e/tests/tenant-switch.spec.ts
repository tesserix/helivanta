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
  return page.evaluate(async () => {
    const res = await fetch("/api/v1/iam/me/permissions");
    const body = await res.json();
    return body.data as string[];
  });
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
