import { readFile } from "node:fs/promises";
import { expect, test } from "@playwright/test";
import { choose, owner, readyOutlet, temporaryRole } from "./support/pharmacy-counter";

test("count an outlet, require independent review and download the verified register", async ({
  page,
  browser,
}) => {
  test.setTimeout(180_000);
  const fixture = await readyOutlet(page, browser);
  const { cashier, outlet, shelf, batch } = fixture;
  try {
    const administrator = await owner<{ tenant_id: string; subject: string }>(
      page,
      "GET",
      "/iam/me/permissions",
    );
    const endReviewer = await temporaryRole(
      page,
      administrator,
      cashier,
      "pharmacy_owner",
      "pharmacy.stock.correct",
      "TEST end temporary stocktake reviewer authority",
    );
    try {
      await page.goto("/pharmacy/stock?view=counts");
      await page.getByRole("button", { name: "Start stocktake", exact: true }).click();
      await choose(page, "Quarantine destination", "TEST Return quarantine · TEST-Q");
      await page
        .getByRole("textbox", { name: "Start reason" })
        .fill("TEST monthly physical stocktake");
      await page.getByRole("button", { name: "Freeze book and start count" }).click();
      await expect(page.getByRole("heading", { name: "Stocktake 1", exact: true })).toBeVisible();

      for (const [status, quantity, completed] of [
        ["quarantine", "0", 1],
        ["sellable", "18", 2],
      ] as const) {
        const bucket = page
          .getByRole("row")
          .filter({ has: page.getByRole("cell", { name: new RegExp(` · ${status}$`) }) });
        await bucket.getByRole("button", { name: /^Record count/ }).click();
        const physical = page.getByRole("textbox", { name: "Physical count (tablet)" });
        await expect(physical).toHaveValue("");
        await physical.fill(quantity);
        await page
          .getByRole("textbox", { name: "Count reason" })
          .fill(`TEST counted ${status} shelf`);
        await page.getByRole("button", { name: "Save physical count" }).click();
        await expect(page.getByText(new RegExp(`${completed} of 2 buckets counted`))).toBeVisible();
      }
      await page.getByRole("button", { name: "Review or close stocktake" }).click();
      await page
        .getByRole("textbox", { name: "Decision reason" })
        .fill("TEST all shelves observed");
      await page.getByRole("button", { name: "Record decision" }).click();
      await expect(page.getByText("Awaiting independent review", { exact: true })).toBeVisible();

      await page.getByRole("button", { name: "Review or close stocktake" }).click();
      await page.getByRole("combobox", { name: "Decision", exact: true }).click();
      await expect(page.getByRole("option", { name: "Approve and post differences" })).toHaveCount(
        0,
      );
      await page.keyboard.press("Escape");
      await page.getByRole("button", { name: "Close entry" }).click();

      await cashier.goto(page.url());
      await cashier.getByRole("button", { name: "Review or close stocktake" }).click();
      await expect(cashier.getByRole("combobox", { name: "Decision", exact: true })).toContainText(
        "Approve and post differences",
      );
      await cashier
        .getByRole("textbox", { name: "Decision reason" })
        .fill("TEST independent shelf and book review");
      await cashier.getByRole("button", { name: "Record decision" }).click();
      await expect(cashier.getByText("Posted", { exact: true })).toBeVisible();
      await expect(cashier.getByRole("button", { name: "Review or close stocktake" })).toHaveCount(
        0,
      );

      const downloading = cashier.waitForEvent("download");
      await cashier.getByRole("button", { name: "Download stocktake register" }).click();
      const download = await downloading;
      expect(download.suggestedFilename()).toBe("stocktake-1-register.csv");
      const file = await download.path();
      expect(file).not.toBeNull();
      const csv = await readFile(file!, "utf8");
      expect(csv).toContain("book_units,counted_units,difference_units");
      expect(csv).toContain(",20,18,-2,");
      expect(csv).toContain(",posted,4,");
      const balance = await owner<{ on_hand_units: number }>(
        cashier,
        "GET",
        `${outlet}/stock/balance?location_id=${shelf}&batch_id=${batch}`,
      );
      expect(balance.on_hand_units).toBe(18);
    } finally {
      await endReviewer("TEST end temporary stocktake reviewer authority");
    }
  } finally {
    await fixture.context.close();
  }
});
