import { chromium, expect } from "@playwright/test";

let material = "";
for await (const chunk of process.stdin) material += chunk;
const fixture = JSON.parse(material);
const browser = await chromium.launch({ headless: true });
try {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
  await context.addCookies([
    { name: "helivanta_session", value: fixture.token, url: fixture.app_url },
  ]);
  const headers = {
    authorization: `Bearer ${fixture.token}`,
    "X-Helivanta-Expected-Tenant": fixture.tenant_id,
    "X-Helivanta-Expected-Subject": fixture.subject,
  };
  const choices = await context.request.get(
    `${fixture.gateway_url}/v1/workforce/staff-actor-scopes?limit=20`,
    { headers },
  );
  expect(choices.status()).toBe(200);
  const hospital = (await choices.json()).data.find(
    (row) => row.hospital_id === fixture.hospital_id,
  );
  expect(hospital).toBeTruthy();
  const commands = [];
  let lost = false;
  await context.route("**/api/**", async (route) => {
    const request = route.request();
    const incoming = new URL(request.url());
    const path = incoming.pathname.replace(/^\/api/, "") + incoming.search;
    const command =
      path.includes(`/member-proposals/${fixture.member.id}/commands/`) &&
      request.method() === "PUT";
    if (command) commands.push({ path, body: request.postData() });
    const response = await route.fetch({
      url: fixture.gateway_url + path,
      headers: { ...request.headers(), authorization: headers.authorization },
    });
    if (command && !lost && response.ok()) {
      lost = true;
      await route.abort("connectionreset");
    } else await route.fulfill({ response });
  });
  const page = await context.newPage();
  page.setDefaultTimeout(30_000);
  await page.goto(`${fixture.app_url}/workforce/partner-teams`);
  const manage = page.getByRole("button", { name: "Manage team memberships", exact: true });
  if (await manage.isVisible()) await manage.click();
  await page.getByRole("button", { name: `Choose ${hospital.name}`, exact: true }).click();
  await page.getByRole("button", { name: `Members of ${fixture.group_name}`, exact: true }).click();
  await expect(page.getByText(fixture.member.member_display_name, { exact: true })).toBeVisible();
  await page
    .getByRole("button", { name: `Review ${fixture.member.member_display_name}`, exact: true })
    .click();
  await page
    .getByLabel("Review reason", { exact: true })
    .fill("Independent browser verification of unilateral hospital membership suspension");
  await page.getByRole("button", { name: "Record membership decision", exact: true }).click();
  await expect(page.getByText(/Decision outcome unconfirmed/)).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Change membership team", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "Recover membership decision", exact: true }).click();
  await expect(
    page.getByText("Recorded membership decision recovered.", { exact: true }),
  ).toBeVisible();
  expect(lost).toBe(true);
  expect(commands).toHaveLength(1);
  expect(JSON.parse(commands[0].body).operation).toBe("suspend");
  const result = await context.request.get(fixture.gateway_url + commands[0].path, { headers });
  expect(result.status()).toBe(200);
  const receipt = await result.json();
  expect(receipt.member.id).toBe(fixture.member.id);
  expect(receipt.member.status).toBe("suspended");
  expect(receipt.result_version).toBe(fixture.member.version + 1);
  await page.screenshot({ path: "/tmp/helivanta-partner-member-owner.png", fullPage: true });
  console.log(
    "PASS: browser → Global API → actual IAM/OpenFGA and Workforce/Postgres; scoped membership suspension commits once and GET recovers a deliberately lost response.",
  );
} finally {
  await browser.close();
}
