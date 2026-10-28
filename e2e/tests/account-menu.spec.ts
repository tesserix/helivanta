import { expect, test } from "@playwright/test";
import { login, specAdmin } from "./support/login";
import { signOut } from "./support/sign-out";

// #1428: the header names who is signed in and their role in THIS hospital
// only. The seeded admin is tenant_admin here and pharmacist in a second
// hospital, so a role leaking across tenants shows up as an extra item.
test("the account menu names the signed-in person and their role here", async ({ page }) => {
  const admin = specAdmin();
  const name = `${admin.email.split("@")[0]} Seed`;
  await login(page, admin);

  await page.getByRole("button", { name: `Account: ${name}` }).click();

  const menu = page.getByRole("dialog", { name: "Account" });
  await expect(menu.getByText(admin.email)).toBeVisible();
  await expect(menu.getByRole("list", { name: /^Role at / }).getByRole("listitem")).toHaveText([
    "Tenant Admin",
  ]);

  await page.keyboard.press("Escape");
  await signOut(page);
  await expect(page.getByRole("heading", { name: "Dashboard", level: 1 })).not.toBeVisible();
});
