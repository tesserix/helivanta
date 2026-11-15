import { expect, test } from "@playwright/test";
import { api, freshOutlet, uuidv7 } from "./support/pharmacy";

test("a hospital owner registers a pharmacy and outlet without acquiring operating access", async ({
  page,
}) => {
  test.setTimeout(90_000);
  await freshOutlet(page);
  const me = await api(page, "GET", "/iam/me/permissions");
  const hospitalId = uuidv7();
  const hospitalName = `Synthetic hospital ${hospitalId}`;
  await api(page, "PUT", `/workforce/hospitals/${hospitalId}`, { name: hospitalName });
  await api(
    page,
    "PUT",
    `/workforce/hospitals/${hospitalId}/access/${encodeURIComponent(String(me.subject))}`,
    { level: "manager" },
  );
  await page.goto("/pharmacy/setup");

  const pharmacyName = `Synthetic hospital pharmacy ${uuidv7()}`;
  await page
    .getByLabel("Pharmacy code", { exact: true })
    .fill(`SYN-${Date.now().toString(36).toUpperCase()}`);
  await page.getByLabel("Pharmacy name", { exact: true }).fill(pharmacyName);
  await page.getByRole("combobox", { name: "Ownership type", exact: true }).click();
  await page.getByRole("option", { name: "Hospital-owned pharmacy", exact: true }).click();
  await page.getByRole("button", { name: "Register pharmacy", exact: true }).click();
  await expect(page.getByRole("combobox", { name: "Pharmacy", exact: true })).toHaveText(
    pharmacyName,
  );
  const pharmacyId = new URL(page.url()).searchParams.get("pharmacy");
  expect(pharmacyId).toMatch(/^[0-9a-f-]{36}$/);
  expect(new URL(page.url()).searchParams.has("outlet")).toBe(false);
  const root = `/pharmacy/pharmacies/${pharmacyId}`;

  await page.getByRole("combobox", { name: "Owning hospital", exact: true }).click();
  await page.getByRole("option", { name: hospitalName, exact: true }).click();
  const affiliationResponse = page.waitForResponse(
    (response) =>
      response.request().method() === "PUT" &&
      response.url().includes(`${root}/hospital-affiliations/`),
  );
  await page.getByRole("button", { name: "Declare hospital affiliation", exact: true }).click();
  const affiliation = await affiliationResponse;
  expect(affiliation.status()).toBe(200);
  expect(await affiliation.json()).toMatchObject({
    hospital_id: hospitalId,
    relationship: "owner",
    verification_status: "pending_validation",
  });

  await page.getByRole("button", { name: "Register outlet", exact: true }).click();
  await page.getByLabel("Outlet code", { exact: true }).fill("SYN-HOSP-MAIN");
  await page.getByLabel("Outlet name", { exact: true }).fill("Synthetic hospital main outlet");
  await page.getByRole("button", { name: "Register outlet", exact: true }).last().click();
  const pane = page.getByRole("region", {
    name: "Outlet Synthetic hospital main outlet",
    exact: true,
  });
  await expect(pane).toBeVisible();
  await expect(pane.getByText("Nobody has access to this outlet yet.")).toBeVisible();
  await expect(pane.getByText("You don't have access to this outlet yet.")).toBeVisible();
  const profile = await api(page, "GET", "/iam/me/profile");
  await expect(pane.getByRole("combobox", { name: "Person", exact: true })).toBeEnabled();
  await pane.getByRole("combobox", { name: "Person", exact: true }).click();
  const ownerChoice = page.getByRole("option").filter({ hasText: String(profile.email) });
  await expect(ownerChoice).toContainText(String(profile.display_name));
  for (const role of profile.roles as { label: string }[])
    await expect(ownerChoice).toContainText(role.label);
  await expect(ownerChoice).not.toContainText(String(me.subject));
  await page.keyboard.press("Escape");

  const outletId = new URL(page.url()).searchParams.get("outlet");
  expect(outletId).toMatch(/^[0-9a-f-]{36}$/);
  const outlet = await api(page, "GET", `${root}/outlets/${outletId}`);
  expect(outlet).toMatchObject({
    pharmacy_id: pharmacyId,
    country_code: "IN",
    timezone: "Asia/Kolkata",
    regulatory_status: "pending_review",
  });
  expect((await page.request.get(`/api/v1${root}/outlets/${outletId}/access`)).status()).toBe(404);

  const refused = await page.request.put(`/api/v1${root}/hospital-affiliations/${uuidv7()}`, {
    headers: { "X-Helivanta-Expected-Tenant": uuidv7() },
    data: { hospital_id: hospitalId, relationship: "owner" },
  });
  expect(refused.status()).toBe(409);
  expect(await refused.json()).toMatchObject({ error: "session_changed" });
  const affiliations = await api(page, "GET", `${root}/hospital-affiliations`);
  expect(affiliations.data).toHaveLength(1);
  await page.reload();
  await expect(pane).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Pharmacy", exact: true })).toHaveText(
    pharmacyName,
  );
});
