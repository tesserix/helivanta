import { expect, test } from "@playwright/test";
import { login } from "./support/login";

test("marketing sign-in resumes the same authenticated tenant without credential or email challenges", async ({
  page,
}) => {
  await login(page);
  const beforeResponse = await page.request.get("/api/v1/iam/me/permissions");
  expect(beforeResponse.status()).toBe(200);
  const before = await beforeResponse.json();
  expect(before.subject).toMatch(/\S+/);
  expect(before.tenant_id).toMatch(/\S+/);

  const challenges: string[] = [];
  page.on("request", (request) => {
    const path = new URL(request.url()).pathname;
    if (
      path.startsWith("/api/v1/auth/accounts") ||
      path === "/api/v1/auth/login/password" ||
      path === "/api/v1/auth/login/factor" ||
      path === "/oauth/v2/authorize"
    ) {
      challenges.push(path);
    }
  });
  const renewed = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/v1/auth/renew" &&
      response.request().method() === "POST",
  );
  await page.goto("/login?start=1");
  expect((await renewed).status()).toBe(200);
  await expect(page).toHaveURL(/\/dashboard(?:[?#]|$)/);
  await expect(page.getByRole("heading", { level: 1, name: "Dashboard" })).toBeVisible();

  const afterResponse = await page.request.get("/api/v1/iam/me/permissions");
  expect(afterResponse.status()).toBe(200);
  const after = await afterResponse.json();
  expect(after.subject).toBe(before.subject);
  expect(after.tenant_id).toBe(before.tenant_id);
  expect(challenges).toEqual([]);
});
