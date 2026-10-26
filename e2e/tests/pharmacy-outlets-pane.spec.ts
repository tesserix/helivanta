import { expect, test } from "@playwright/test";
import { api, expectLabelsInNames, expectList, freshOutlet, uuidv7 } from "./support/pharmacy";

// #1303 E6: Outlets & access lists the pharmacy's outlets, and one opens
// beside the list with its details, its access and its tax registration.

for (const [label, size] of [
  ["1366", { width: 1366, height: 768 }],
  ["1920", { width: 1920, height: 1080 }],
] as const) {
  test(`an outlet opens beside the outlet list at ${label}`, async ({ page }) => {
    await page.setViewportSize(size);
    const { outlet } = await freshOutlet(page);
    const root = outlet.split("/outlets/")[0];
    await api(page, "PUT", `${root}/outlets/${uuidv7()}`, {
      code: "TEST-OVW-2",
      name: "TEST Second Outlet",
      country_code: "IN",
      timezone: "Asia/Kolkata",
    });
    await page.goto("/pharmacy/setup");
    const first = page.getByRole("button", { name: "Open TEST-OVW", exact: true });
    await expect(first).toBeVisible();
    const row = first.locator("xpath=ancestor::li");
    await first.click();
    const pane = page.getByRole("region", { name: "Outlet TEST Overview Outlet" });
    await expect(pane.getByRole("heading", { name: "Outlet access" })).toBeVisible();
    await expectLabelsInNames(page);
    await expect(pane.getByText("Asia/Kolkata")).toBeVisible();
    const list = (await row.boundingBox())!;
    expect((await pane.boundingBox())!.x).toBeGreaterThanOrEqual(list.x + list.width);
    // Already granted by the fixture: granting again says so.
    await pane.getByLabel("Grant my account pharmacy access to this outlet").check();
    await pane.getByRole("button", { name: "Grant pharmacy access" }).click();
    await expect(page.getByText("Pharmacy access granted")).toBeVisible();

    await page.reload();
    await expect(pane).toBeVisible();
  });
}

test.describe("on a phone", () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test("an outlet takes the screen, and Back returns to the list", async ({ page }) => {
    await freshOutlet(page);
    await page.goto("/pharmacy/setup");
    await page.getByRole("button", { name: "Open TEST-OVW", exact: true }).click();
    await expect(page.getByRole("region", { name: "Outlet TEST Overview Outlet" })).toBeVisible();
    await page.goBack();
    await expectList(page, "/pharmacy/setup", {});
    await expect(page.getByRole("button", { name: "Open TEST-OVW", exact: true })).toBeVisible();
  });
});
