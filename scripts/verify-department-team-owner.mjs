import { chromium, expect } from "@playwright/test";

let material = "";
for await (const chunk of process.stdin) material += chunk;
const fixture = JSON.parse(material);
const browser = await chromium.launch({
  headless: true,
  ...(process.env.CHROME_EXECUTABLE ? { executablePath: process.env.CHROME_EXECUTABLE } : {}),
});
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
  await context.addCookies([
    { name: "helivanta_session", value: fixture.token, url: fixture.app_url },
  ]);
  let lost = false;
  const transfers = [];
  await context.route("**/api/**", async (route) => {
    const request = route.request();
    const incoming = new URL(request.url());
    const path = incoming.pathname.replace(/^\/api/, "") + incoming.search;
    const transfer = path.includes("/transfers/") && request.method() === "PUT";
    if (transfer) {
      expect(path).toContain(`/posting-proposals/${fixture.replacement.id}/transfers/`);
      transfers.push({ path, body: request.postData() });
    }
    const response = await route.fetch({
      url: fixture.gateway_url + path,
      headers: { ...request.headers(), authorization: `Bearer ${fixture.token}` },
    });
    if (transfer && !lost && response.ok()) {
      lost = true;
      await route.abort("connectionreset");
    } else await route.fulfill({ response });
  });
  const page = await context.newPage();
  page.setDefaultTimeout(20_000);
  await page.goto(`${fixture.app_url}/workforce/department-teams`);
  await page.getByRole("button", { name: "View current department teams", exact: true }).click();
  await page
    .getByRole("button", { name: /^Open team for / })
    .first()
    .click();
  await expect(page.getByText("Current at last check", { exact: true }).first()).toBeVisible();
  await page.getByRole("button", { name: "Review heads and supervisors", exact: true }).click();
  await page
    .getByRole("button", { name: /^Open posting reviews for / })
    .first()
    .click();
  await page
    .getByRole("button", { name: /^Open proposal for / })
    .first()
    .click();
  await page.getByRole("button", { name: "Review supervisor chain approval", exact: true }).click();
  await page
    .getByLabel("Coordinate this replacement with ending an original responsibility")
    .check();
  await page
    .getByRole("button", { name: /^Choose original: / })
    .first()
    .click();
  await expect(
    page.getByText("The current employment source matches this submission.", { exact: true }),
  ).toBeVisible();
  await page
    .getByLabel("Decision rationale", { exact: true })
    .fill("Independent browser acceptance of a coordinated department transfer");
  await page
    .getByLabel("I examined the submitted proposal and current employment source.", { exact: true })
    .check();
  await page
    .getByLabel("I independently reviewed the current supervisor chain.", { exact: true })
    .check();
  await page.getByRole("button", { name: "Approve coordinated transfer", exact: true }).click();
  await page.getByRole("button", { name: "Recover or replay transfer", exact: true }).click();
  await expect(page.getByText(/^Coordinated transfer recorded\./)).toBeVisible();
  expect(lost).toBe(true);
  expect(transfers).toHaveLength(2);
  expect(transfers[1]).toEqual(transfers[0]);
  expect(JSON.parse(transfers[0].body).from_posting_id).toBe(fixture.original.id);
  const receipt = await context.request.put(fixture.gateway_url + transfers[0].path, {
    headers: {
      authorization: `Bearer ${fixture.token}`,
      "X-Helivanta-Expected-Tenant": fixture.tenant_id,
      "X-Helivanta-Expected-Subject": fixture.subject,
      "content-type": "application/json",
    },
    data: transfers[0].body,
  });
  expect(receipt.status()).toBe(200);
  const pair = await receipt.json();
  expect(pair.ending.cutoff_at).toBe(pair.replacement.starts_at);
  expect(pair.ending.id).toBe(pair.replacement.command_id);
  await page.screenshot({ path: "/tmp/helivanta-department-team-owner.png", fullPage: true });
  console.log(
    "PASS: browser → Global API → real IAM/OpenFGA, Organization and Workforce/Postgres; paired transfer commits and exact replay recovers a deliberately lost response.",
  );
} finally {
  await browser.close();
}
