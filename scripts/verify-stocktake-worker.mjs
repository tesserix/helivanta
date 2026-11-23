import { chromium, expect } from "@playwright/test";

let material = "";
for await (const chunk of process.stdin) material += chunk;
const fixture = JSON.parse(material);
const browser = await chromium.launch({ headless: true });
const contexts = [];
const commands = [];
let lostCount = false;
const root = `/v1/pharmacy/pharmacies/${fixture.pharmacy}/operating-locations/${fixture.outlet}/stocktakes`;
try {
  async function open(actor) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
    contexts.push(context);
    await context.addCookies([{ name: "helivanta_session", value: actor, url: fixture.app_url }]);
    await context.route("**/api/**", async (route) => {
      const request = route.request();
      const incoming = new URL(request.url());
      const path = incoming.pathname.replace(/^\/api/, "") + incoming.search;
      if (actor === "receiver" || actor === "stock-reviewer") {
        expect(path).not.toMatch(/\/outlets\/.*\/(catalogue|stock)/);
        expect(path).not.toContain("/operating-management");
      }
      if (path.startsWith(root) && request.method() === "PUT") {
        expect(request.headers()["x-helivanta-expected-tenant"]).toBe(fixture.tenant_id);
        expect(request.headers()["x-helivanta-expected-subject"]).toBe(`user-${actor}`);
        commands.push({ actor, path, body: request.postDataJSON() });
      }
      const response = await route.fetch({
        url: fixture.gateway_url + path,
        headers: { ...request.headers(), authorization: `Bearer ${actor}` },
      });
      if (
        actor === "receiver" &&
        request.method() === "PUT" &&
        path.includes("/counts/") &&
        response.ok() &&
        !lostCount
      ) {
        lostCount = true;
        await route.abort("connectionreset");
      } else await route.fulfill({ response });
    });
    const page = await context.newPage();
    page.setDefaultTimeout(20_000);
    await page.goto(`${fixture.app_url}/pharmacy/working-stocktakes`);
    await expect(page.getByRole("heading", { name: "My stocktakes", exact: true })).toBeVisible();
    return page;
  }
  for (const actor of ["owner", "reviewer"]) {
    const page = await open(actor);
    await expect(
      page.getByText("No current reviewed stocktake duty is available to this account."),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "Start stocktake", exact: true })).toHaveCount(0);
  }
  const counter = await open("receiver");
  await counter.getByRole("button", { name: /TEST Independent Pharmacy.*TEST-RX-MAIN/ }).click();
  await counter.getByRole("button", { name: "Start stocktake", exact: true }).click();
  await counter.getByRole("combobox", { name: "Quarantine destination", exact: true }).click();
  await counter.getByRole("option", { name: "TEST Quarantine · TEST-Q", exact: true }).click();
  await counter
    .getByRole("textbox", { name: "Start reason", exact: true })
    .fill("TEST reviewed browser physical count");
  await counter.getByRole("button", { name: "Freeze book and start count", exact: true }).click();
  await expect(counter.getByRole("heading", { name: "Stocktake 1", exact: true })).toBeVisible();
  await counter.getByRole("button", { name: /^Record count/ }).click();
  const physical = counter.getByRole("textbox", { name: "Physical count (capsule)", exact: true });
  await expect(physical).toHaveValue("");
  await physical.fill("27");
  await counter
    .getByRole("textbox", { name: "Count reason", exact: true })
    .fill("TEST counted three missing units");
  await counter.getByRole("button", { name: "Save physical count", exact: true }).click();
  await expect(counter.getByText(/The save could not be confirmed/)).toBeVisible();
  counter.once("dialog", (dialog) => dialog.accept());
  await counter.reload();
  await counter.getByRole("button", { name: /TEST Independent Pharmacy.*TEST-RX-MAIN/ }).click();
  await counter.getByRole("button", { name: "Check original count result", exact: true }).click();
  await expect(counter.getByText(/Original physical count was recorded/)).toBeVisible();
  await counter.getByRole("button", { name: "Acknowledge saved result", exact: true }).click();
  await counter.getByRole("button", { name: "Open stocktake 1", exact: true }).click();
  await expect(counter.getByText(/1 of 1 buckets counted/)).toBeVisible();
  await counter.getByRole("button", { name: "Review or close stocktake", exact: true }).click();
  await counter
    .getByRole("textbox", { name: "Decision reason", exact: true })
    .fill("TEST independent review requested");
  await counter.getByRole("button", { name: "Record decision", exact: true }).click();
  await expect(counter.getByText("Awaiting independent review", { exact: true })).toBeVisible();
  await counter.getByRole("button", { name: "Review or close stocktake", exact: true }).click();
  await counter.getByRole("combobox", { name: "Decision", exact: true }).click();
  await expect(
    counter.getByRole("option", { name: "Approve and post differences", exact: true }),
  ).toHaveCount(0);
  await counter.keyboard.press("Escape");

  const reviewer = await open("stock-reviewer");
  await reviewer.getByRole("button", { name: /TEST Independent Pharmacy.*TEST-RX-MAIN/ }).click();
  await expect(reviewer.getByRole("button", { name: "Start stocktake", exact: true })).toHaveCount(
    0,
  );
  await reviewer.getByRole("button", { name: "Open stocktake 1", exact: true }).click();
  await expect(reviewer.getByRole("button", { name: /^Record count/ })).toHaveCount(0);
  await reviewer.getByRole("button", { name: "Review or close stocktake", exact: true }).click();
  await expect(reviewer.getByRole("combobox", { name: "Decision", exact: true })).toContainText(
    "Approve and post differences",
  );
  await reviewer
    .getByRole("textbox", { name: "Decision reason", exact: true })
    .fill("TEST independent shortage verified");
  await reviewer.getByRole("button", { name: "Record decision", exact: true }).click();
  await expect(reviewer.getByText("Posted", { exact: true })).toBeVisible();
  const downloading = reviewer.waitForEvent("download");
  await reviewer.getByRole("button", { name: "Download stocktake register", exact: true }).click();
  const download = await downloading;
  expect(download.suggestedFilename()).toBe("stocktake-1-register.csv");
  const stream = await download.createReadStream();
  let csv = "";
  for await (const chunk of stream) csv += chunk;
  expect(csv).toContain("book_units,counted_units,difference_units");
  expect(csv).toContain(",30,27,-3,");
  expect(commands.map(({ actor }) => actor)).toEqual([
    "receiver",
    "receiver",
    "receiver",
    "stock-reviewer",
  ]);
  expect(lostCount).toBe(true);
  expect(commands.at(-1).body).toMatchObject({
    decision: "approve",
    expected_version: 2,
    extra_units_disposition: "no_extra_units",
  });
  await reviewer.screenshot({ path: "/tmp/helivanta-stocktake-worker.png", fullPage: true });
  console.log(
    "PASS: reviewed worker browser → Global API → IAM/Workforce/Pharmacy; separate counter and reviewer post one attributable shortage and download the verified register.",
  );
} finally {
  for (const context of contexts) await context.unrouteAll({ behavior: "wait" });
  await browser.close();
}
