import { expect, test, type Page } from "@playwright/test";
import { login, specAdmin } from "./support/login";

function observeChallenges(page: Page): string[] {
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
  return challenges;
}

test("marketing sign-in resumes the same authenticated tenant without credential or email challenges", async ({
  page,
}) => {
  await login(page);
  const beforeResponse = await page.request.get("/api/v1/iam/me/permissions");
  expect(beforeResponse.status()).toBe(200);
  const before = await beforeResponse.json();
  expect(before.subject).toMatch(/\S+/);
  expect(before.tenant_id).toMatch(/\S+/);

  const challenges = observeChallenges(page);
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

// #1751 spec D1: with no session and no sign-out, sign-in starts on its own.
// It is unscoped even though this browser just used a workspace (#1820 spec
// D1), and offers the remembered email. prompt=login keeps it deliberate: it
// reaches the card and nothing is granted, no email code is sent and no
// credential is checked, until a password is typed.
test("a missing app session reopens the email card with the remembered name and no challenge", async ({
  page,
}) => {
  const user = specAdmin();
  await login(page, user);
  await page.context().clearCookies({ name: "helivanta_session" });
  const challenges = observeChallenges(page);
  const refused = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/v1/auth/renew" &&
      response.request().method() === "POST",
  );
  await page.goto("/login?start=1");
  expect((await refused).status()).toBe(401);
  await expect(page.getByLabel("Email or username", { exact: true })).toHaveValue(user.email, {
    timeout: 30_000,
  });
  await expect(page.getByText("Sign in with your email and password.")).toBeVisible();
  await expect(page).toHaveURL(/\/login\?authRequest=/);
  expect(challenges.filter((path) => path !== "/oauth/v2/authorize")).toEqual([]);
  const probe = await page.request.get("/api/v1/iam/me/permissions");
  expect(probe.status()).toBe(401);
});

// #1820 slice 2 D2: "Sign in again" after a failed sign-in is a new sign-in.
// A live session in this browser is never asked, so it cannot answer for the
// account the person was trying to reach, and it survives until a password is
// typed.
test("sign in again opens the email card without consulting the live session", async ({ page }) => {
  await login(page);
  const renewals: string[] = [];
  page.on("request", (request) => {
    if (new URL(request.url()).pathname === "/api/v1/auth/renew") renewals.push(request.url());
  });
  await page.goto("/login?again=1");
  await expect(page.getByText("Sign in with your email and password.")).toBeVisible({
    timeout: 30_000,
  });
  await expect(page).toHaveURL(/\/login\?authRequest=/);
  expect(renewals).toEqual([]);
  const probe = await page.request.get("/api/v1/iam/me/permissions");
  expect(probe.status()).toBe(200);
});
