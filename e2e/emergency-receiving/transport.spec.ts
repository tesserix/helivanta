import { expect, test, type BrowserContext } from "@playwright/test";
const api = process.env.EMERGENCY_BROWSER_API;
if (!api || new URL(api).hostname !== "127.0.0.1") throw new Error("Disposable fixture required");
async function connect(context: BrowserContext, origin: string, actor: string) {
  await context.addCookies([
    { name: "helivanta_session", value: `synthetic-${actor}`, url: origin },
  ]);
  await context.route("**/api/v1/**", async (route) => {
    const url = new URL(route.request().url());
    const response = await route.fetch({
      url: `${api}${url.pathname.slice(4)}${url.search}`,
      headers: { ...route.request().headers(), authorization: `Bearer ${actor}` },
    });
    await route.fulfill({ response });
  });
}
test("independent hospital acceptance and physical receipt gate transport and clearance", async ({
  page,
  context,
  browser,
  request,
  baseURL,
}) => {
  if (!baseURL) throw new Error("Local shell required");
  const seed = await (await request.get(`${api}/__test/receiving-seed`)).json();
  const hospitalContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
  const crewContext = await browser.newContext({ viewport: { width: 390, height: 844 } });
  try {
    await connect(context, baseURL, "supervisor");
    await connect(hospitalContext, baseURL, "breceiver");
    await connect(crewContext, baseURL, "driver");
    const hospital = await hospitalContext.newPage(),
      crew = await crewContext.newPage();
    await page.goto(`/emergency/bases/${seed.base_id}/cases/${seed.case_id}`);
    await page.getByRole("combobox", { name: "Approved receiving partner" }).click();
    await page.getByRole("option", { name: "Synthetic Receiving Campus", exact: true }).click();
    await page.getByLabel("Pre-alert communication reference").fill("SYN-BROWSER-PREALERT");
    await page.getByRole("button", { name: "Send hospital pre-alert", exact: true }).click();
    await hospital.goto(`${baseURL}/emergency/receiving`);
    await hospital
      .getByRole("button", { name: "Open Synthetic Receiving Campus", exact: true })
      .click();
    await hospital.getByRole("button", { name: "Open pre-alert", exact: true }).click();
    await hospital.getByLabel("Clinical communication reference").fill("SYN-CAPACITY-CONFIRMED");
    await hospital
      .getByRole("button", { name: "Confirm hospital acceptance", exact: true })
      .click();
    await expect(
      hospital.getByRole("heading", { name: "Hospital acceptance recorded", exact: true }),
    ).toBeVisible();
    await expect(
      hospital.getByRole("button", { name: "Confirm physical receipt", exact: true }),
    ).toBeDisabled();
    await crew.goto(`${baseURL}/emergency/crew`);
    await crew.getByRole("button", { name: "Open assignment", exact: true }).click();
    for (const action of ["Start hospital transport", "Record hospital arrival"]) {
      await crew.getByLabel("Operational evidence reference").fill("SYN-BROWSER-TRANSPORT");
      await crew.getByRole("button", { name: action, exact: true }).click();
    }
    await expect(
      crew.getByRole("button", { name: "Confirm crew handover", exact: true }),
    ).toHaveCount(0);
    await hospital.getByRole("button", { name: "Refresh current pre-alert", exact: true }).click();
    await hospital.getByLabel("Clinical communication reference").fill("SYN-PHYSICAL-RECEIPT");
    await hospital.getByRole("button", { name: "Confirm physical receipt", exact: true }).click();
    await expect(
      hospital.getByRole("heading", { name: "Physical receipt confirmed", exact: true }),
    ).toBeVisible();
    await crew.getByRole("button", { name: "Refresh current assignment", exact: true }).click();
    await crew.getByLabel("Operational evidence reference").fill("SYN-CREW-HANDOVER");
    await crew.getByRole("button", { name: "Confirm crew handover", exact: true }).click();
    await expect(
      crew.getByRole("heading", { name: "Handover confirmed", exact: true }),
    ).toBeVisible();
    await page.goto("/emergency");
    await page.getByRole("button", { name: "Open Synthetic Odisha Base", exact: true }).click();
    await page.getByRole("button", { name: "Record readiness for SYN-ALS-1", exact: true }).click();
    await page.getByLabel("Measured oxygen pressure (bar)", { exact: true }).fill("150");
    await page.getByLabel("Defibrillator self-test passed", { exact: true }).check();
    await page.getByLabel("Reviewed equipment checklist complete", { exact: true }).check();
    await page.getByLabel("Roadworthiness check passed", { exact: true }).check();
    await page.getByRole("button", { name: "Record readiness", exact: true }).click();
    await page.goto(`/emergency/bases/${seed.base_id}/cases/${seed.case_id}`);
    await page.getByLabel("Operational confirmation reference").fill("SYN-SUPERVISOR-CLEAR");
    await page
      .getByRole("button", { name: "Clear trip after post-handover check", exact: true })
      .click();
    await expect(page.getByText("Trip cleared", { exact: true })).toBeVisible();
    for (const [name, screen] of [
      ["receiving", hospital],
      ["transport", crew],
      ["cleared", page],
    ] as const) {
      expect(
        await screen.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
      ).toBe(true);
      await screen.screenshot({
        path: `../docs/architecture/evidence/ambulanceconnect-2026-10-18/${name}-mobile.png`,
        fullPage: true,
      });
    }
  } finally {
    await context.unrouteAll({ behavior: "wait" });
    await hospitalContext.unrouteAll({ behavior: "wait" });
    await crewContext.unrouteAll({ behavior: "wait" });
    await hospitalContext.close();
    await crewContext.close();
  }
});
