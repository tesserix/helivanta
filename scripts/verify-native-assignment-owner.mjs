import { chromium, expect } from "@playwright/test";

let material = "";
for await (const chunk of process.stdin) material += chunk;
const fixture = JSON.parse(material);
const browser = await chromium.launch({
  headless: true,
  ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}),
});
const contexts = [];
const proposals = [];
const decisions = [];
const root = `/v1/iam/hospitals/${fixture.hospital}`;
const reason = "Synthetic qualified hospital browser assignment";
let lostProposal = false;
let lostDecision = false;
let proposalPath;
try {
  async function open(actor) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
    contexts.push(context);
    await context.addCookies([{ name: "helivanta_session", value: actor, url: fixture.app_url }]);
    await context.route("**/api/**", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      const path = url.pathname.replace(/^\/api/, "") + url.search;
      expect(path).not.toMatch(/^\/v1\/iam\/assignment-proposals/);
      expect(path).not.toContain("/staff-access-templates");
      const proposal =
        path.startsWith(root + "/assignment-proposals/") && request.method() === "PUT";
      const decision =
        path.startsWith(root + "/assignment-proposals/") &&
        path.endsWith("/decision") &&
        request.method() === "POST";
      if (proposal || decision) {
        expect(request.headers()["x-helivanta-expected-tenant"]).toBe(fixture.tenant_id);
        expect(request.headers()["x-helivanta-expected-subject"]).toBe(`user-${actor}`);
        const command = { actor, path, body: request.postDataJSON() };
        (proposal ? proposals : decisions).push(command);
      }
      const response = await route.fetch({
        url: fixture.gateway_url + path,
        headers: { ...request.headers(), authorization: `Bearer ${actor}` },
      });
      if (proposal && response.ok() && !lostProposal) {
        proposalPath = path;
        lostProposal = true;
        await route.abort("connectionreset");
      } else if (decision && response.ok() && !lostDecision) {
        lostDecision = true;
        await route.abort("connectionreset");
      } else await route.fulfill({ response });
    });
    const page = await context.newPage();
    page.setDefaultTimeout(20_000);
    const discovery = page.waitForResponse(
      (response) =>
        new URL(response.url()).pathname === "/api/v1/workforce/assignment-authority-scopes",
    );
    await page.goto(`${fixture.app_url}/workforce/access`);
    const response = await discovery;
    expect(response.status()).toBe(200);
    const choices = (await response.json()).data;
    const index = choices.findIndex((choice) => choice.hospital_id === fixture.hospital);
    expect(index).toBeGreaterThanOrEqual(0);
    const workspace = page
      .getByRole("heading", { name: "Hospital assignment reviews", exact: true })
      .locator("..");
    await workspace
      .getByRole("button", { name: /^Open / })
      .nth(index)
      .click();
    return { page, context };
  }
  const { page: preparer } = await open("admin");
  const targetRead = preparer.waitForResponse(
    (response) => new URL(response.url()).pathname === "/api" + root + "/assignment-target-choices",
  );
  await preparer.getByRole("button", { name: "Prepare assignment", exact: true }).click();
  const targetResponse = await targetRead;
  expect(targetResponse.status()).toBe(200);
  expect((await targetResponse.json()).data).toEqual([
    {
      hospital_id: fixture.hospital,
      staff_id: fixture.staff,
      subject: fixture.subject,
      name: fixture.name,
      email: fixture.email,
    },
  ]);
  await preparer.getByRole("button", { name: `Choose ${fixture.name}`, exact: true }).click();
  const form = preparer.getByRole("form", { name: "Prepare hospital assignment" });
  await expect(form.getByText(fixture.email, { exact: true })).toBeVisible();
  await expect(
    form.getByLabel(/account ID|subject ID|Staff ID|department ID|membership epoch/i),
  ).toHaveCount(0);
  await form.getByLabel("Assignment reason", { exact: true }).fill(reason);
  await form
    .getByLabel("Access starts", { exact: true })
    .fill(new Date(Date.now() - 60_000).toISOString().slice(0, 16));
  await form
    .getByLabel("Access expires", { exact: true })
    .fill(new Date(Date.now() + 30 * 60_000).toISOString().slice(0, 16));
  await form.getByRole("button", { name: "Submit proposal", exact: true }).click();
  await expect(form.getByRole("alert")).toContainText("may have been recorded");
  await expect(form.getByLabel("Assignment reason", { exact: true })).toBeDisabled();
  await expect(form.getByRole("button", { name: "Close preparation", exact: true })).toBeDisabled();
  await form.getByRole("button", { name: "Retry same request", exact: true }).click();
  await expect(preparer.getByRole("region", { name: "Current assignment review" })).toBeVisible();
  await expect(preparer.getByRole("button", { name: "Approve", exact: true })).toHaveCount(0);
  expect(lostProposal).toBe(true);
  expect(proposals).toHaveLength(2);
  expect(proposals[0]).toEqual(proposals[1]);
  expect(proposals[0].body).toMatchObject({
    hospital_id: fixture.hospital,
    subject: fixture.subject,
    template_key: "workforce.leave_reviewer.v1",
    reason,
  });
  const { page: reviewer, context } = await open("owner2");
  await reviewer.getByRole("button", { name: "Review assignment", exact: true }).click();
  const headers = {
    authorization: "Bearer owner2",
    "x-helivanta-expected-tenant": fixture.tenant_id,
    "x-helivanta-expected-subject": "user-owner2",
  };
  async function currentStatus() {
    const response = await context.request.get(fixture.gateway_url + proposalPath, { headers });
    expect(response.status()).toBe(200);
    return (await response.json()).status;
  }
  await expect.poll(currentStatus, { timeout: 20_000 }).toBe("pending_review");
  await reviewer.getByRole("button", { name: "Refresh selected assignment", exact: true }).click();
  await reviewer.getByRole("button", { name: "Approve", exact: true }).click();
  await reviewer
    .getByLabel("Decision reason", { exact: true })
    .fill("Independent owner confirms current qualified Staff responsibility");
  await reviewer.getByRole("button", { name: "Confirm approve", exact: true }).click();
  await expect(reviewer.getByRole("alert").first()).toContainText("may have been recorded");
  await expect(reviewer.getByRole("button", { name: "Close form", exact: true })).toBeDisabled();
  await reviewer.getByRole("button", { name: "Retry same request", exact: true }).click();
  await expect.poll(currentStatus, { timeout: 20_000 }).toBe("active");
  await reviewer.getByRole("button", { name: "Refresh selected assignment", exact: true }).click();
  await expect(reviewer.getByText(/^active · Version /)).toBeVisible();
  expect(lostDecision).toBe(true);
  expect(decisions).toHaveLength(2);
  expect(decisions[0]).toEqual(decisions[1]);
  expect(decisions[0].body).toMatchObject({ expected_version: 2, decision: "approve" });
  const history = reviewer.waitForResponse(
    (response) => new URL(response.url()).pathname === "/api" + proposalPath + "/history",
  );
  await reviewer.getByRole("button", { name: "History", exact: true }).click();
  expect((await history).status()).toBe(200);
  console.log(
    "PASS: qualified hospital Staff preparation, exact lost-response proposal/decision replay, distinct owner approval, canonical activation and audited native history through Global API.",
  );
} finally {
  await Promise.all(
    contexts.map(async (context) => {
      await context.unrouteAll({ behavior: "wait" });
      await context.close();
    }),
  );
  await browser.close();
}
