import { expect, test, type BrowserContext } from "@playwright/test";

const api = process.env.EMERGENCY_BROWSER_API;
if (!api || new URL(api).hostname !== "127.0.0.1")
  throw new Error("Use the disposable Go browser fixture");

async function connect(context: BrowserContext, origin: string, actor: string) {
  await context.addCookies([
    { name: "helivanta_session", value: `synthetic-${actor}-session`, url: origin },
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

test("assigned driver and clinician acknowledge separately, progress and lose private access after stand-down", async ({
  page,
  context,
  browser,
  request,
  baseURL,
}) => {
  if (!baseURL) throw new Error("Local shell required");
  const seed = await (await request.get(`${api}/__test/crew-seed`)).json();
  await connect(context, baseURL, "driver");
  const clinical = await browser.newContext({ viewport: { width: 390, height: 844 } });
  try {
    await connect(clinical, baseURL, "medic");
    const medic = await clinical.newPage();
    await page.goto("/emergency/crew");
    await page.getByRole("button", { name: "Open assignment", exact: true }).click();
    await expect(page.getByText("Synthetic Gate C", { exact: true })).toBeVisible();
    await expect(page.getByText("Synthetic Caller", { exact: true })).toHaveCount(0);
    await page.getByLabel("Operational evidence reference").fill("SYN-DRIVER-ACK");
    await page.getByRole("button", { name: "Acknowledge assignment", exact: true }).click();
    await expect(page.getByText("Your acknowledgement is recorded.")).toBeVisible();
    await expect(page.getByRole("button", { name: "Record departure", exact: true })).toHaveCount(
      0,
    );
    await medic.goto(`${baseURL}/emergency/crew`);
    await medic.getByRole("button", { name: "Open assignment", exact: true }).click();
    await medic.getByLabel("Operational evidence reference").fill("SYN-MEDIC-ACK");
    await medic.getByRole("button", { name: "Acknowledge assignment", exact: true }).click();
    await expect(
      medic.getByRole("button", { name: "Record departure", exact: true }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Refresh current assignment", exact: true }).click();
    await page.getByLabel("Operational evidence reference").fill("SYN-DEPARTURE");
    await page.getByRole("button", { name: "Record departure", exact: true }).click();
    await expect(
      page.getByRole("button", { name: "Record arrival on scene", exact: true }),
    ).toBeVisible();
    await page.getByLabel("Operational evidence reference").fill("SYN-ON-SCENE");
    await page.getByRole("button", { name: "Record arrival on scene", exact: true }).click();
    await expect(page.getByRole("heading", { name: "At the scene", exact: true })).toBeVisible();
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true);
    await page.screenshot({
      path: "../docs/architecture/evidence/ambulanceconnect-2026-10-18/crew-mobile.png",
      fullPage: true,
    });
    const path = `${api}/v1/emergency/bases/${seed.base_id}/dispatches/${seed.dispatch_id}`;
    const current = await (
      await request.get(path, { headers: { authorization: "Bearer dispatcher" } })
    ).json();
    for (const [offset, actor, operation] of [
      [0, "dispatcher", "request_cancel"],
      [1, "supervisor", "reconcile_cancelled"],
    ] as const) {
      const response = await request.put(
        `${path}/commands/${crypto.randomUUID().replace(/^(.{14})./, (_match, prefix: string) => `${prefix}7`)}`,
        {
          headers: { authorization: `Bearer ${actor}` },
          data: {
            version: current.version + offset,
            operation,
            evidence_reference: "SYN-VOICE-STAND-DOWN",
          },
        },
      );
      expect(response.status(), await response.text()).toBe(200);
    }
    await page.getByRole("button", { name: "Refresh current assignment", exact: true }).click();
    await expect(
      page.getByRole("alert").filter({ hasText: "Assignment or incident access is unavailable" }),
    ).toBeVisible();
    await expect(page.getByText("Synthetic Gate C", { exact: true })).toHaveCount(0);
    await expect(page.getByLabel("Operational evidence reference")).toHaveCount(0);
  } finally {
    await context.unrouteAll({ behavior: "wait" });
    await clinical.unrouteAll({ behavior: "wait" });
    await clinical.close();
  }
});
