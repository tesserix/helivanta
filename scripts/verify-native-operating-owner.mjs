import { chromium, expect } from "@playwright/test";

let material = "";
for await (const chunk of process.stdin) material += chunk;
const fixture = JSON.parse(material);
const browser = await chromium.launch({
  headless: true,
  ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}),
});
const contexts = [];
const mutations = [];
const recoveries = [];
let proposal;
let lostProposal = false;
let lostDecision = false;
const root = `/v1/iam/operating-governance-employers/${fixture.kind}/${fixture.employer}`;
const reason = `Synthetic browser ${fixture.kind} receiving duty`;
try {
  async function open(actor) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
    contexts.push(context);
    await context.addCookies([{ name: "helivanta_session", value: actor, url: fixture.app_url }]);
    await context.route("**/api/**", async (route) => {
      const request = route.request();
      const url = new URL(request.url());
      const path = url.pathname.replace(/^\/api/, "") + url.search;
      expect(path).not.toMatch(/^\/v1\/(supply\/parties|pharmacy\/pharmacies)/);
      expect(path).not.toContain("/account-links");
      expect(path).not.toMatch(/^\/v1\/iam\/operating-duties/);
      const mutation = path.startsWith(root + "/duties/") && request.method() === "PUT";
      if (mutation) {
        expect(request.headers()["x-helivanta-expected-tenant"]).toBe(fixture.tenant_id);
        expect(request.headers()["x-helivanta-expected-subject"]).toBe(`user-${actor}`);
        mutations.push({ actor, path, body: request.postDataJSON() });
      }
      if (
        path.endsWith("/proposal-receipt") ||
        (path.includes("/commands/") && request.method() === "GET")
      )
        recoveries.push({ actor, path });
      const response = await route.fetch({
        url: fixture.gateway_url + path,
        headers: { ...request.headers(), authorization: `Bearer ${actor}` },
      });
      if (mutation && response.ok() && !path.includes("/commands/") && !lostProposal) {
        proposal = path;
        lostProposal = true;
        await route.abort("connectionreset");
      } else if (mutation && response.ok() && path.includes("/commands/") && !lostDecision) {
        lostDecision = true;
        await route.abort("connectionreset");
      } else await route.fulfill({ response });
    });
    const page = await context.newPage();
    page.setDefaultTimeout(20_000);
    const discovery = page.waitForResponse(
      (r) => new URL(r.url()).pathname === "/api/v1/workforce/operating-authority-scopes",
    );
    await page.goto(`${fixture.app_url}/workforce/operating-access`);
    const response = await discovery;
    expect(response.status()).toBe(200);
    const choices = (await response.json()).data;
    const index = choices.findIndex(
      (row) => row.employer_id === fixture.employer && row.employer_kind === fixture.kind,
    );
    expect(index).toBeGreaterThanOrEqual(0);
    await page
      .getByRole("button", { name: /^Manage / })
      .nth(index)
      .click();
    await page
      .getByRole("button", { name: "Open Synthetic account-link target", exact: true })
      .click();
    await expect(
      page.getByRole("heading", { name: "Operating duties", exact: true }),
    ).toBeVisible();
    await expect(page.getByLabel(/employer ID|Staff ID|subject ID/i)).toHaveCount(0);
    return page;
  }
  const preparer = await open("auditor");
  await preparer.getByRole("button", { name: "Assign operating duty", exact: true }).click();
  await preparer.getByLabel("Operating role", { exact: true }).click();
  await preparer
    .getByRole("option", {
      name:
        fixture.kind === "pharmacy"
          ? "Outlet receiving and stock history"
          : "Warehouse receiving and stock history",
      exact: true,
    })
    .click();
  await preparer
    .getByLabel(fixture.kind === "pharmacy" ? "Pharmacy outlet" : "Warehouse or depot", {
      exact: true,
    })
    .click();
  await preparer.getByRole("option", { name: /^SYN-NATIVE-PRIMARY/ }).click();
  const now = Date.now();
  await preparer
    .getByLabel("Valid from (UTC)", { exact: true })
    .fill(new Date(now - 60_000).toISOString().slice(0, 16));
  await preparer
    .getByLabel("Expires at (UTC)", { exact: true })
    .fill(new Date(now + 30 * 60_000).toISOString().slice(0, 16));
  await preparer.getByLabel("Duty reason", { exact: true }).fill(reason);
  await preparer.getByRole("button", { name: "Propose operating duty", exact: true }).click();
  await expect(
    preparer.getByRole("button", { name: "Recover duty proposal", exact: true }),
  ).toBeEnabled();
  await expect(
    preparer.getByRole("button", { name: "Change employer", exact: true }),
  ).toBeDisabled();
  await expect(
    preparer.getByRole("button", { name: "Change Staff member", exact: true }),
  ).toBeDisabled();
  await preparer.getByRole("button", { name: "Recover duty proposal", exact: true }).click();
  await expect(
    preparer.getByText(
      "Operating duty proposed. A separate reviewer must approve it before activation.",
      { exact: true },
    ),
  ).toBeVisible();
  const proposedDuty = preparer
    .getByRole("article")
    .filter({ has: preparer.getByText(reason, { exact: true }) });
  await expect(proposedDuty.getByText("Status: pending review", { exact: true })).toBeVisible();
  await expect(proposedDuty.getByRole("button", { name: "Approve duty", exact: true })).toHaveCount(
    0,
  );
  expect(lostProposal).toBe(true);
  expect(mutations).toHaveLength(1);
  expect(mutations[0].body).toMatchObject({
    subject: "user-reviewer",
    identity: { staff_id: fixture.staff, staff_version: 1, link_version: 2 },
    scope: {
      employer_kind: fixture.kind,
      employer_id: fixture.employer,
      location_id: fixture.location,
      expected_employer_version: 1,
      expected_location_version: 1,
    },
    template_key: `${fixture.kind === "pharmacy" ? "pharmacy" : "supply"}.inventory_receiver.v1`,
    reason,
  });
  expect(recoveries).toContainEqual({ actor: "auditor", path: proposal + "/proposal-receipt" });
  const reviewer = await open("owner2");
  const duty = reviewer
    .getByRole("article")
    .filter({ has: reviewer.getByText(reason, { exact: true }) });
  await duty
    .getByLabel("Decision reason", { exact: true })
    .fill("Independent browser review of current native Staff and outlet");
  await duty.getByRole("button", { name: "Approve duty", exact: true }).click();
  await expect(
    duty.getByRole("button", { name: "Recover duty decision", exact: true }),
  ).toBeEnabled();
  await duty.getByRole("button", { name: "Recover duty decision", exact: true }).click();
  await expect(
    duty.getByText("Status: approved pending activation", { exact: true }),
  ).toBeVisible();
  expect(lostDecision).toBe(true);
  expect(mutations).toHaveLength(2);
  expect(recoveries).toContainEqual({ actor: "owner2", path: mutations[1].path });
  await duty
    .getByLabel("Decision reason", { exact: true })
    .fill("End the synthetic browser duty after review acceptance");
  await duty.getByRole("button", { name: "Revoke duty", exact: true }).click();
  await expect(duty.getByText("Status: revoked", { exact: true })).toBeVisible();
  expect(mutations).toHaveLength(3);
  expect(mutations[2].body).toMatchObject({ decision: "revoke", expected_version: 2 });
  console.log(
    `PASS: ${fixture.kind} native employer → reviewed Staff → owner location → proposal → independent review → revocation; lost responses recovered by GET through Global API and real IAM/Workforce/product owners.`,
  );
} finally {
  await Promise.all(contexts.map((context) => context.close()));
  await browser.close();
}
