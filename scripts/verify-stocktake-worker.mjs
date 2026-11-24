import { chromium, expect } from "@playwright/test";

let material = "";
for await (const chunk of process.stdin) material += chunk;
const fixture = JSON.parse(material);
const browser = await chromium.launch({ headless: true });
const contexts = [];
const commands = [];
let lostCount = false;
const root = fixture.hospital
  ? `/v1/pharmacy/hospitals/${fixture.hospital}/departments/${fixture.department}/pharmacies/${fixture.pharmacy}/outlets/${fixture.outlet}/stocktakes`
  : `/v1/pharmacy/pharmacies/${fixture.pharmacy}/operating-locations/${fixture.outlet}/stocktakes`;
const takeNumber = fixture.take_number ?? 1;
const bookUnits = fixture.book_units ?? 30;
const countedUnits = fixture.counted_units ?? 27;
try {
  async function open(actor) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
    contexts.push(context);
    const token = fixture.accounts?.[actor]?.token ?? actor;
    const subject = fixture.accounts?.[actor]?.subject ?? `user-${actor}`;
    await context.addCookies([{ name: "helivanta_session", value: token, url: fixture.app_url }]);
    await context.route("**/api/**", async (route) => {
      const request = route.request();
      const incoming = new URL(request.url());
      const path = incoming.pathname.replace(/^\/api/, "") + incoming.search;
      if (actor === "receiver" || actor === "stock-reviewer") {
        expect(path).not.toMatch(/\/outlets\/[^/]+\/(catalogue|stock)(\/|$)/);
        expect(path).not.toContain("/operating-management");
        if (fixture.hospital) expect(path).not.toContain("/operating-locations/");
      }
      if (path.startsWith(root) && request.method() === "PUT") {
        expect(request.headers()["x-helivanta-expected-tenant"]).toBe(fixture.tenant_id);
        expect(request.headers()["x-helivanta-expected-subject"]).toBe(subject);
        commands.push({ actor, path, body: request.postDataJSON() });
      }
      const response = await route.fetch({
        url: fixture.gateway_url + path,
        headers: { ...request.headers(), authorization: `Bearer ${token}` },
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
  async function chooseOutlet(page, role) {
    if (fixture.hospital) {
      const actor = role === "counter" ? "receiver" : "stock-reviewer";
      const response = await page
        .context()
        .request.get(
          `${fixture.gateway_url}/v1/iam/me/hospital-pharmacy-stocktake-scopes?limit=20`,
          {
            headers: {
              authorization: `Bearer ${fixture.accounts[actor].token}`,
              "X-Helivanta-Expected-Tenant": fixture.tenant_id,
              "X-Helivanta-Expected-Subject": fixture.accounts[actor].subject,
            },
          },
        );
      expect(response.ok()).toBe(true);
      const scopes = await response.json();
      const assignment = scopes.data.find(
        (item) =>
          item.hospital_id === fixture.hospital &&
          item.department_id === fixture.department &&
          item.template_key === `pharmacy.department_stocktake_${role}.v1`,
      );
      expect(assignment).toBeTruthy();
      await page
        .getByRole("button", {
          name: `${assignment.hospital_name} · ${assignment.department_name} · ${assignment.template_label}`,
          exact: true,
        })
        .click();
      const departmentsResponse = await page
        .context()
        .request.get(
          `${fixture.gateway_url}/v1/pharmacy/hospitals/${fixture.hospital}/departments/${fixture.department}/stocktake-department-choices?limit=20`,
          {
            headers: {
              authorization: `Bearer ${fixture.accounts[actor].token}`,
              "X-Helivanta-Expected-Tenant": fixture.tenant_id,
              "X-Helivanta-Expected-Subject": fixture.accounts[actor].subject,
            },
          },
        );
      expect(departmentsResponse.ok()).toBe(true);
      const departments = await departmentsResponse.json();
      const department = departments.data.find((item) => item.department_id === fixture.department);
      expect(department).toBeTruthy();
      await page
        .getByRole("button", {
          name: `${department.department_name} · ${department.department_code}`,
          exact: true,
        })
        .click();
      await page.getByRole("button", { name: fixture.outlet_name, exact: true }).click();
    } else {
      await page.getByRole("button", { name: /TEST Independent Pharmacy.*TEST-RX-MAIN/ }).click();
    }
    for (const value of [fixture.tenant_id, fixture.pharmacy, fixture.outlet])
      await expect(page.locator("body")).not.toContainText(value);
  }
  for (const actor of fixture.unassigned_actors ?? ["owner", "reviewer"]) {
    const page = await open(actor);
    await expect(
      page.getByText("No current reviewed stocktake assignment is available to this account."),
    ).toBeVisible();
    await expect(page.getByRole("button", { name: "Start stocktake", exact: true })).toHaveCount(0);
  }
  const counter = await open("receiver");
  await chooseOutlet(counter, "counter");
  await counter.getByRole("button", { name: "Start stocktake", exact: true }).click();
  await counter.getByRole("combobox", { name: "Quarantine destination", exact: true }).click();
  await counter
    .getByRole("option", {
      name: fixture.quarantine_name ?? "TEST Quarantine · TEST-Q",
      exact: true,
    })
    .click();
  await counter
    .getByRole("textbox", { name: "Start reason", exact: true })
    .fill("TEST reviewed browser physical count");
  await counter.getByRole("button", { name: "Freeze book and start count", exact: true }).click();
  await expect(
    counter.getByRole("heading", { name: `Stocktake ${takeNumber}`, exact: true }),
  ).toBeVisible();
  await counter.getByRole("button", { name: /^Record count/ }).click();
  const physical = counter.getByRole("textbox", { name: "Physical count (capsule)", exact: true });
  await expect(physical).toHaveValue("");
  await physical.fill(String(countedUnits));
  await counter
    .getByRole("textbox", { name: "Count reason", exact: true })
    .fill("TEST counted three missing units");
  await counter.getByRole("button", { name: "Save physical count", exact: true }).click();
  await expect(counter.getByText(/The save could not be confirmed/)).toBeVisible();
  counter.once("dialog", (dialog) => dialog.accept());
  await counter.reload();
  await chooseOutlet(counter, "counter");
  await counter.getByRole("button", { name: "Check original count result", exact: true }).click();
  await expect(counter.getByText(/Original physical count was recorded/)).toBeVisible();
  await counter.getByRole("button", { name: "Acknowledge saved result", exact: true }).click();
  await counter.getByRole("button", { name: `Open stocktake ${takeNumber}`, exact: true }).click();
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
  await chooseOutlet(reviewer, "reviewer");
  await expect(reviewer.getByRole("button", { name: "Start stocktake", exact: true })).toHaveCount(
    0,
  );
  await reviewer.getByRole("button", { name: `Open stocktake ${takeNumber}`, exact: true }).click();
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
  expect(download.suggestedFilename()).toBe(`stocktake-${takeNumber}-register.csv`);
  const stream = await download.createReadStream();
  let csv = "";
  for await (const chunk of stream) csv += chunk;
  expect(csv).toContain("book_units,counted_units,difference_units");
  expect(csv).toContain(`,${bookUnits},${countedUnits},${countedUnits - bookUnits},`);
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
  await reviewer.screenshot({
    path: fixture.hospital
      ? "/tmp/helivanta-hospital-stocktake-worker.png"
      : "/tmp/helivanta-stocktake-worker.png",
    fullPage: true,
  });
  console.log(
    "PASS: reviewed worker browser → Global API → IAM/Workforce/Pharmacy; separate counter and reviewer post one attributable shortage and download the verified register.",
  );
} finally {
  for (const context of contexts) await context.unrouteAll({ behavior: "wait" });
  await browser.close();
}
