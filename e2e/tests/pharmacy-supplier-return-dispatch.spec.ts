import { expect, test, type Page, type TestInfo } from "@playwright/test";
import { choose, owner, readyOutlet } from "./support/pharmacy-counter";
import { uuidv7 } from "./support/pharmacy";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

async function attachSupplierEvidence(
  page: Page,
  info: TestInfo,
  name:
    | "commercial-return-note-print"
    | "supplier-credit-recovered"
    | "supplier-credit-matched"
    | "supplier-credit-match-reversed",
) {
  const path = info.outputPath(`${name}.png`);
  await page.screenshot({ path, fullPage: true });
  await info.attach(name, { path, contentType: "image/png" });
}

async function financial<T>(page: Page, method: string, path: string, body?: unknown): Promise<T> {
  const actor = await owner<{ subject: string; tenant_id: string }>(
    page,
    "GET",
    "/iam/me/permissions",
  );
  const response = await page.evaluate(
    async ({ actor, method, path, body }) => {
      const response = await fetch(`/api/v1${path}`, {
        method,
        headers: {
          "Content-Type": "application/json",
          "X-Helivanta-Expected-Subject": actor.subject,
          "X-Helivanta-Expected-Tenant": actor.tenant_id,
        },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      return { status: response.status, body: await response.text() };
    },
    { actor, method, path, body },
  );
  expect(response.status, `${method} ${path}: ${response.body}`).toBeLessThan(300);
  return JSON.parse(response.body) as T;
}

test("supplier return preparation and physical dispatch recover response loss without duplicate facts", async ({
  page,
  browser,
}, testInfo) => {
  test.setTimeout(180_000);
  const fixture = await readyOutlet(page, browser);
  const { outlet, batch, quarantine, shelf, stamp } = fixture;
  try {
    const movements = await owner<{ data: { id: string; kind: string }[] }>(
      page,
      "GET",
      `${outlet}/stock/movements?batch_id=${batch}`,
    );
    const receipt = movements.data.find((row) => row.kind === "receipt");
    expect(receipt).toBeDefined();
    await owner(page, "PUT", `${outlet}/stock/holds/${uuidv7()}`, {
      batch_id: batch,
      from_location_id: shelf,
      quarantine_location_id: quarantine,
      quantity_units: 4,
      reason: "TEST short-dated outward return",
      physical_disposition: "moved_to_quarantine",
    });
    const payable = uuidv7();
    await owner(page, "PUT", `${outlet}/accounts/payables/${payable}`, {
      payee_reference: "TEST-EXT",
      payee_name: "TEST External Supplier",
      document_reference: `TEST-BILL-${stamp}`,
      issued_on: "2026-11-01",
      due_on: "2026-11-30",
      currency: "INR",
      lines: [
        {
          category: "inventory",
          receipt_id: receipt!.id,
          description: "TEST original delivery",
          net_minor: 10000,
          tax_minor: 1800,
          recoverable_tax_minor: 1800,
          landed_minor: 0,
        },
      ],
    });
    await page.goto("/pharmacy/accounts?tab=supplier-returns");
    await choose(page, "Original supplier bill", new RegExp(`TEST-BILL-${stamp}`));
    await choose(page, "Original receipt", /batch/);
    await choose(page, "Quarantine location", /recorded/);
    await page.getByLabel("Return units", { exact: true }).fill("4");
    await page.getByLabel("Expected credit (INR)", { exact: true }).fill("15.00");
    await page.getByRole("button", { name: "Add return line", exact: true }).click();
    await page
      .getByLabel("Return reason", { exact: true })
      .fill("TEST supplier agreed to collect short-dated units");
    let returnId = "",
      preparations = 0;
    await page.route("**/accounts/supplier-returns/*", async (route) => {
      if (route.request().method() !== "PUT") {
        await route.continue();
        return;
      }
      preparations++;
      const response = await route.fetch();
      expect(response.status(), await response.text()).toBe(200);
      const created = (await response.json()) as { id: string };
      returnId = created.id;
      await route.abort("failed");
    });
    await page.getByRole("button", { name: "Prepare supplier return", exact: true }).click();
    await expect(page.getByText(/The preparation result is unconfirmed/)).toBeVisible();
    await page.unroute("**/accounts/supplier-returns/*");
    page.once("dialog", (dialog) => dialog.accept());
    await page.reload();
    await page.getByRole("button", { name: "Check preparation result", exact: true }).click();
    await expect(page.getByText(/Return 1 prepared/)).toBeVisible();
    expect(preparations).toBe(1);
    const beforeDispatch = await owner<{ on_hand_units: number }>(
      page,
      "GET",
      `${outlet}/stock/balance?location_id=${quarantine}&batch_id=${batch}`,
    );
    expect(beforeDispatch.on_hand_units).toBe(4);
    await choose(page, "Reviewed outlet licence", new RegExp(`TEST-DL-${stamp}`));
    await page.getByLabel("Outward document reference", { exact: true }).fill(`TEST-OUT-${stamp}`);
    await page.getByLabel("Recipient name", { exact: true }).fill("TEST Supplier Driver");
    const deviceTime = await page.evaluate(() => {
      const now = new Date();
      return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}T${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}:${String(now.getSeconds()).padStart(2, "0")}.${String(now.getMilliseconds()).padStart(3, "0")}`;
    });
    await page.getByLabel("Handoff time (device time)", { exact: true }).fill(deviceTime);
    await page
      .getByLabel("Dispatch reason", { exact: true })
      .fill("TEST four units physically handed to supplier");
    await page.getByRole("checkbox", { name: /physically handed/ }).check();
    let writes = 0;
    let dispatchPath = "";
    await page.route("**/accounts/supplier-returns/*/dispatches/*", async (route) => {
      if (route.request().method() !== "PUT") {
        await route.continue();
        return;
      }
      writes++;
      dispatchPath = new URL(route.request().url()).pathname.replace(/^\/api\/v1/, "");
      const response = await route.fetch();
      expect(response.status(), await response.text()).toBe(200);
      await route.abort("failed");
    });
    await page.getByRole("button", { name: "Record physical dispatch", exact: true }).click();
    await expect(page.getByText(/The dispatch result is unconfirmed/)).toBeVisible();
    await page.unroute("**/accounts/supplier-returns/*/dispatches/*");
    page.once("dialog", (dialog) => dialog.accept());
    await page.reload();
    await page.getByRole("button", { name: "Open return 1", exact: true }).click();
    await page.getByRole("button", { name: "Check dispatch result", exact: true }).click();
    await expect(page.getByText(/Dispatch verified for return 1/)).toBeVisible();
    expect(writes).toBe(1);
    const dispatched = await financial<{ state: string; lines: unknown[] }>(
      page,
      "GET",
      dispatchPath,
    );
    expect(dispatched.state).toBe("dispatched");
    expect(dispatched.lines).toHaveLength(1);
    const downloaded = page.waitForEvent("download");
    await page
      .getByRole("button", { name: "Download commercial return note", exact: true })
      .click();
    const note = await downloaded;
    expect(note.suggestedFilename()).toBe("supplier-return-1.html");
    const notePath = await note.path();
    if (!notePath) throw new Error("The commercial note download was not retained.");
    const noteHTML = await readFile(notePath, "utf8");
    expect(noteHTML).toContain(`TEST-BILL-${stamp}`);
    expect(noteHTML).toContain(`TEST-OUT-${stamp}`);
    expect(noteHTML).not.toContain(returnId);
    const printPreview = await page.context().newPage();
    try {
      await printPreview.emulateMedia({ media: "print" });
      await printPreview.setContent(noteHTML);
      await expect(
        printPreview.getByRole("heading", { name: "Commercial supplier return note", exact: true }),
      ).toBeVisible();
      await expect(
        printPreview.getByRole("table", { name: "Returned batches" }).getByRole("row"),
      ).toHaveCount(2);
      await expect(printPreview.getByRole("cell", { name: "4", exact: true })).toBeVisible();
      await expect(printPreview.getByText(/Not a tax invoice/)).toBeVisible();
      await attachSupplierEvidence(printPreview, testInfo, "commercial-return-note-print");
      const printDirectory = process.env.HELIVANTA_E2E_PRINT_DIR;
      if (printDirectory) {
        const screenshot = await printPreview.screenshot({ fullPage: true });
        await mkdir(printDirectory, { recursive: true, mode: 0o700 });
        await writeFile(join(printDirectory, "supplier-return-note.png"), screenshot, {
          mode: 0o600,
        });
        const pdf = await printPreview.pdf({
          format: "A4",
          printBackground: true,
          preferCSSPageSize: true,
        });
        await writeFile(join(printDirectory, "supplier-return-note.pdf"), pdf, { mode: 0o600 });
      }
    } finally {
      await printPreview.close();
    }
    const balance = await owner<{ on_hand_units: number }>(
      page,
      "GET",
      `${outlet}/stock/balance?location_id=${quarantine}&batch_id=${batch}`,
    );
    expect(balance.on_hand_units).toBe(0);
    const shelfBalance = await owner<{ on_hand_units: number }>(
      page,
      "GET",
      `${outlet}/stock/balance?location_id=${shelf}&batch_id=${batch}`,
    );
    expect(shelfBalance.on_hand_units).toBe(16);
    const bill = await owner<{ outstanding_minor: number }>(
      page,
      "GET",
      `${outlet}/accounts/payables/${payable}`,
    );
    expect(bill.outstanding_minor).toBe(11800);
    const outletParts = outlet.split("/");
    await page.goto(
      `/pharmacy/accounts?tab=supplier-credits&pharmacy=${outletParts[3]}&outlet=${outletParts[5]}`,
    );
    await page.getByRole("button", { name: "Record supplier credit", exact: true }).click();
    await choose(
      page,
      "Dispatched supplier return",
      new RegExp(`Return 1.*TEST External Supplier.*TEST-BILL-${stamp}`),
    );
    await expect(page.getByLabel("Actual credit amount (INR)", { exact: true })).toHaveValue("");
    const creditReference = `TEST-CN-${stamp}`;
    await page.getByLabel("Supplier credit reference", { exact: true }).fill(creditReference);
    await page.getByLabel("Credit issue date", { exact: true }).fill("2026-11-18");
    await page.getByLabel("Actual credit amount (INR)", { exact: true }).fill("7.50");
    await page
      .getByLabel("Credit record reason", { exact: true })
      .fill("TEST actual partial credit issued by supplier");
    let creditId = "",
      creditWrites = 0;
    await page.route("**/accounts/supplier-credits/*", async (route) => {
      if (route.request().method() !== "PUT") {
        await route.continue();
        return;
      }
      creditWrites++;
      const response = await route.fetch();
      expect(response.status(), await response.text()).toBe(200);
      creditId = ((await response.json()) as { id: string }).id;
      await route.abort("failed");
    });
    await page.getByRole("button", { name: "Save supplier credit", exact: true }).click();
    await expect(page.getByText(/credit outcome is unconfirmed/)).toBeVisible();
    await page.unroute("**/accounts/supplier-credits/*");
    page.once("dialog", (dialog) => dialog.accept());
    await page.reload();
    await page.getByRole("button", { name: "Check original credit", exact: true }).click();
    await expect(
      page.getByText(
        `Supplier credit ${creditReference} verified. Return matching and bill applications are separate steps.`,
      ),
    ).toBeVisible();
    expect(creditWrites).toBe(1);
    const credit = await financial<{
      amount_minor: number;
      matched_minor: number;
      allocated_minor: number;
      available_minor: number;
    }>(page, "GET", `${outlet}/accounts/supplier-credits/${creditId}`);
    expect(credit).toMatchObject({
      amount_minor: 750,
      matched_minor: 0,
      allocated_minor: 0,
      available_minor: 750,
    });
    const unchangedBill = await owner<{ outstanding_minor: number }>(
      page,
      "GET",
      `${outlet}/accounts/payables/${payable}`,
    );
    expect(unchangedBill.outstanding_minor).toBe(11800);
    const creditDetails = page.getByRole("region", {
      name: "Supplier credit details",
      exact: true,
    });
    await expect(
      creditDetails.getByRole("heading", {
        name: `Credit ${creditReference} · TEST External Supplier`,
        exact: true,
      }),
    ).toBeVisible();
    await expect(creditDetails.getByText("INR 7.50", { exact: true })).toHaveCount(2);
    await attachSupplierEvidence(page, testInfo, "supplier-credit-recovered");
    await page.getByRole("button", { name: "Match credit to return", exact: true }).click();
    await choose(page, "Dispatched return to match", new RegExp(`Return 1.*TEST-BILL-${stamp}`));
    await expect(page.getByLabel("Amount to match (INR)", { exact: true })).toHaveValue("");
    await page.getByLabel("Amount to match (INR)", { exact: true }).fill("7.50");
    await page
      .getByLabel("Matching reason", { exact: true })
      .fill("TEST attribute actual partial supplier credit");
    let matchWrites = 0;
    const matchingPattern = `**/accounts/supplier-credits/${creditId}/matches/*`;
    await page.route(matchingPattern, async (route) => {
      if (route.request().method() !== "PUT") {
        await route.continue();
        return;
      }
      matchWrites++;
      const response = await route.fetch();
      expect(response.status(), await response.text()).toBe(200);
      await route.abort("failed");
    });
    await page.getByRole("button", { name: "Save return match", exact: true }).click();
    await expect(page.getByText(/matching outcome is unconfirmed/)).toBeVisible();
    await page.unroute(matchingPattern);
    page.once("dialog", (dialog) => dialog.accept());
    await page.reload();
    await page.getByRole("button", { name: `Open credit ${creditReference}`, exact: true }).click();
    await page.getByRole("button", { name: "Check original match", exact: true }).click();
    await expect(page.getByText(/Return matching verified/)).toBeVisible();
    expect(matchWrites).toBe(1);
    const attributed = await financial<{
      matched_minor: number;
      allocated_minor: number;
      available_minor: number;
    }>(page, "GET", `${outlet}/accounts/supplier-credits/${creditId}`);
    expect(attributed).toMatchObject({
      matched_minor: 750,
      allocated_minor: 0,
      available_minor: 750,
    });
    const stillUnpaid = await owner<{ outstanding_minor: number }>(
      page,
      "GET",
      `${outlet}/accounts/payables/${payable}`,
    );
    expect(stillUnpaid.outstanding_minor).toBe(11800);
    await expect(
      page
        .getByRole("table", { name: "Supplier credit return matching" })
        .getByRole("cell", { name: "INR 7.50", exact: true }),
    ).toBeVisible();
    await attachSupplierEvidence(page, testInfo, "supplier-credit-matched");
    await page.getByRole("button", { name: "Review match for return 1", exact: true }).click();
    await page
      .getByLabel("Reversal reason", { exact: true })
      .fill("TEST correct original matching evidence");
    let reversalWrites = 0,
      reversalId = "";
    const reversalPattern = `**/accounts/supplier-credits/${creditId}/matches/*/reversals/*`;
    await page.route(reversalPattern, async (route) => {
      if (route.request().method() !== "PUT") {
        await route.continue();
        return;
      }
      reversalWrites++;
      const response = await route.fetch();
      expect(response.status(), await response.text()).toBe(200);
      reversalId = ((await response.json()) as { id: string }).id;
      await route.abort("failed");
    });
    await page.getByRole("button", { name: "Reverse full match", exact: true }).click();
    await expect(page.getByText(/reversal outcome is unconfirmed/)).toBeVisible();
    await page.unroute(reversalPattern);
    page.once("dialog", (dialog) => dialog.accept());
    await page.reload();
    await page.getByRole("button", { name: `Open credit ${creditReference}`, exact: true }).click();
    await page.getByRole("button", { name: "Check original reversal", exact: true }).click();
    await expect(page.getByText(/Return matching reversal verified/)).toBeVisible();
    await expect(
      page.getByText("TEST correct original matching evidence", { exact: true }),
    ).toBeVisible();
    expect(reversalWrites).toBe(1);
    const reversedCredit = await financial<{
      matched_minor: number;
      allocated_minor: number;
      available_minor: number;
    }>(page, "GET", `${outlet}/accounts/supplier-credits/${creditId}`);
    expect(reversedCredit).toMatchObject({
      matched_minor: 0,
      allocated_minor: 0,
      available_minor: 750,
    });
    const reversedBill = await owner<{ outstanding_minor: number }>(
      page,
      "GET",
      `${outlet}/accounts/payables/${payable}`,
    );
    expect(reversedBill.outstanding_minor).toBe(11800);
    const unchangedStock = await owner<{ on_hand_units: number }>(
      page,
      "GET",
      `${outlet}/stock/balance?location_id=${quarantine}&batch_id=${batch}`,
    );
    expect(unchangedStock.on_hand_units).toBe(0);
    const matchHistory = await financial<{
      data: { reversal_id: string | null; amount_minor: number }[];
    }>(page, "GET", `${outlet}/accounts/supplier-credits/${creditId}/matches`);
    expect(matchHistory.data).toHaveLength(1);
    expect(matchHistory.data[0]?.amount_minor).toBe(750);
    expect(matchHistory.data[0]?.reversal_id).toBe(reversalId);
    await attachSupplierEvidence(page, testInfo, "supplier-credit-match-reversed");
    expect(await page.locator("body").innerText()).not.toContain(creditId);
    expect(await page.locator("body").innerText()).not.toContain(returnId);
  } finally {
    await fixture.context.close();
  }
});
