import { expect, test } from "@playwright/test";

test.use({ storageState: { cookies: [], origins: [] }, viewport: { width: 390, height: 844 } });

test("citizen call access works signed out when the hospital API is unavailable", async ({
  page,
}) => {
  await page.route("**/api/v1/**", (route) => route.abort());
  await page.goto("/ambulance");
  await expect(page.getByRole("heading", { name: "Need emergency help?" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Open dialler for 112" })).toHaveAttribute(
    "href",
    "tel:112",
  );
  await expect(page.getByText(/does not book an ambulance/)).toBeVisible();
  await page.addStyleTag({ content: "html { font-size: 24px; }" });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  await page.getByRole("combobox", { name: "Where are you?" }).selectOption("OTHER");
  await expect(page.getByRole("link", { name: "Open dialler for 112" })).toHaveCount(0);
  await expect(page.getByRole("status")).toContainText("contact the local emergency service");
  await page.getByRole("combobox", { name: "Where are you?" }).selectOption("IN");
  await expect(page.getByRole("link", { name: "Open dialler for 112" })).toHaveAttribute(
    "href",
    "tel:112",
  );
  expect(await page.context().cookies()).toEqual([]);
});

test("public configuration is narrow and neighbouring emergency routes remain protected", async ({
  request,
}) => {
  const response = await request.get("/api/emergency/access?country=IN");
  expect(response.status()).toBe(200);
  expect(response.headers()["cache-control"]).toBe("no-store");
  expect(response.headers()["referrer-policy"]).toBe("no-referrer");
  expect(response.headers()["set-cookie"]).toBeUndefined();
  const body = await response.json();
  expect(body.services[0].dialUrl).toBe("tel:112");
  expect(body.bookingEnabled).toBe(false);
  expect((await request.get("/api/emergency/access?country=IN&tenant_id=private")).status()).toBe(
    400,
  );
  expect((await request.get("/api/emergency/access/admin")).status()).toBe(401);
  expect((await request.get("/api/v1/emergency/cases")).status()).toBe(401);
  expect((await request.get("/ambulance/admin", { maxRedirects: 0 })).status()).toBe(307);
  expect(
    (await request.post("/api/emergency/access", { data: { phone: "not-collected" } })).status(),
  ).toBe(405);
});
