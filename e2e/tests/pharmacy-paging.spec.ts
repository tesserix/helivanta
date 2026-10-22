import { expect, test, type Page } from "@playwright/test";
import { login } from "./support/login";

// #1295: the Dispense queue pages 25 at a time with Previous / Next, keeps
// the page in the URL, and never guesses the way back after a reload.
//
// The dev database is shared and other specs dispense while this runs, so
// the queue's total moves. This spec searches Waiting for its own run's
// patients (D9): 51 of them make exactly three pages, 25 + 25 + 1.

async function createVisits(page: Page, n: number, stamp: number) {
  const failures = await page.evaluate(
    async ({ n, stamp }) => {
      const bad: string[] = [];
      for (let i = 0; i < n; i++) {
        const res = await fetch("/api/v1/medicore/visits", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ patient_name: `Paging Patient ${stamp}-${i}`, department: "OPD" }),
        });
        if (!res.ok) bad.push(`${res.status} ${await res.text()}`);
      }
      return bad;
    },
    { n, stamp },
  );
  expect(failures).toEqual([]);
}

const firstPatient = (page: Page) =>
  page.getByRole("listitem").first().locator("span").first().textContent();

test("the dispense queue pages forward and back and survives a reload", async ({ page }) => {
  test.setTimeout(120_000);
  await login(page);
  const stamp = Date.now();
  await page.goto("/pharmacy");
  await createVisits(page, 51, stamp);
  const mine = `/pharmacy?tab=waiting&q=${encodeURIComponent(`Paging Patient ${stamp}-`)}`;

  // visit_created reaches pharmacy through JetStream; wait for all 51.
  const range = page.getByRole("navigation", { name: "Pages" }).locator("p");
  const total = 51;
  await expect(async () => {
    await page.goto(mine);
    await expect(range).toHaveText(`1–25 of ${total}`, { timeout: 2_000 });
  }).toPass({ timeout: 45_000, intervals: [2_000] });
  const pageOne = await firstPatient(page);
  // Oldest first: the run's first visit heads the queue.
  expect(pageOne).toBe(`Paging Patient ${stamp}-0`);

  await page.getByRole("link", { name: "Next" }).click();
  await expect(page).toHaveURL(/page=2/);
  await expect(range).toHaveText(`26–50 of ${total}`);
  const pageTwo = await firstPatient(page);
  expect(pageTwo).not.toBe(pageOne);

  await page.getByRole("link", { name: "Previous" }).click();
  await expect(page).not.toHaveURL(/page=/);
  await expect(range).toHaveText(`1–25 of ${total}`);
  expect(await firstPatient(page)).toBe(pageOne);

  // Previous is a link, so it added a history entry: the browser's Back
  // returns to page 2. A reload keeps page 2's rows. Page 1 always starts at
  // the beginning, so Previous from page 2 is still known after a reload.
  await page.goBack();
  await expect(page).toHaveURL(/page=2/);
  await page.reload();
  await expect(range).toHaveText(`26–50 of ${total}`);
  expect(await firstPatient(page)).toBe(pageTwo);

  // Page 3 after a reload cannot know where page 2 began: no guessing (D5).
  await page.getByRole("link", { name: "Next" }).click();
  await expect(page).toHaveURL(/page=3/);
  await page.reload();
  await expect(range).toHaveText(`51–51 of ${total}`);
  await expect(page.getByRole("link", { name: "Previous" })).toHaveCount(0);
  await page.getByRole("link", { name: "First page" }).click();
  await expect(range).toHaveText(`1–25 of ${total}`);

  await page.getByRole("link", { name: "Dispensed" }).click();
  await expect(page).toHaveURL(/tab=dispensed/);
  const active = page.getByRole("link", { name: "Dispensed" });
  await expect(active).toHaveAttribute("aria-current", "page");
  // The active tab is marked in the accent colour, measured on the page.
  const colours = await active.evaluate((link) => {
    const probe = document.createElement("div");
    probe.style.background = "var(--hms-accent)";
    document.body.append(probe);
    const accent = getComputedStyle(probe).backgroundColor;
    probe.remove();
    const bar = link.querySelector("[data-tab-indicator]");
    return { accent, bar: bar ? getComputedStyle(bar).backgroundColor : null };
  });
  expect(colours.bar).toBe(colours.accent);
});
