import { expect, test } from "@playwright/test";
import { login } from "./support/login";

// #576 D1: the overview is /pharmacy; the queue moved to /pharmacy/dispense,
// and a shared queue link with its parameters still opens the queue.
test("an old queue link opens the queue at its new address", async ({ page }) => {
  await login(page);
  await page.goto("/pharmacy?tab=waiting&q=Nobody");
  await expect(page).toHaveURL(/\/pharmacy\/dispense\?tab=waiting&q=Nobody$/);
  await expect(page.getByRole("link", { name: "Waiting" })).toHaveAttribute("aria-current", "page");
});
