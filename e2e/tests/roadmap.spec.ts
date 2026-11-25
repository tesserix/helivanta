import { test, expect } from "@playwright/test";

// #1837: what is not available yet lives on a public /roadmap, one click from
// the home page, and the home page carries no Planned or Future badge.

test("the roadmap is public and reached from the home page's nav", async ({ page }) => {
  await page.goto("/");
  await expect(page.getByText(/^(Planned|Future)$/)).toHaveCount(0);
  await page
    .getByRole("navigation", { name: "Main navigation" })
    .getByRole("link", { name: "Roadmap" })
    .click();
  await expect(page).toHaveURL(/\/roadmap$/);
  await expect(page.getByRole("heading", { level: 1, name: "Roadmap" })).toBeVisible();
  await expect(page.getByText("What comes next")).toBeVisible();
});

test("the roadmap loads without a session", async ({ request }) => {
  const response = await request.get("/roadmap", { maxRedirects: 0 });
  expect(response.status()).toBe(200);
});
