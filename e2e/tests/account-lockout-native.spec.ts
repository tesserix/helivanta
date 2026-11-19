import { randomUUID } from "node:crypto";
import { lstatSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { expect, test, type Page } from "@playwright/test";
import { login, specPharmacist, type Credentials } from "./support/login";
import { ZITADEL_ORIGIN } from "./support/hosts";
import nativeStaffTenants from "../../scripts/lib/native-staff-fixture-tenants.json" with { type: "json" };

test.use({ trace: "off", video: "off", screenshot: "off" });
const tenant = nativeStaffTenants.hospital;
const permissionPath = "/api/v1/iam/me/permissions";

async function refusedSignIn(page: Page, account: Credentials) {
  await page.goto(`/login?workspace=${tenant}`);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByLabel("Email or username", { exact: true }).fill(account.email);
  await page.getByLabel("Password", { exact: true }).fill(account.password);
  const refusal = page.waitForResponse(
    (response) =>
      response.request().method() === "POST" &&
      new URL(response.url()).pathname === "/api/v1/auth/login/password",
  );
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  const response = await refusal;
  expect(response.status()).toBe(401);
  await expect(page.getByRole("alert")).toContainText(
    /account is locked, contact your tenant administrator for recovery/i,
  );
  await expect(page.getByRole("alert")).toContainText(
    /Tenant owners should contact Helivanta support/i,
  );
  return (await response.json()) as unknown;
}

test("a genuinely locked native account gets actionable recovery guidance without an account-existence oracle", async ({
  page,
}) => {
  test.setTimeout(90_000);
  if (
    process.env.HELIVANTA_STAFF_MFA_RECOVERY_FIXTURE !== "true" ||
    new URL(test.info().project.use.baseURL!).origin !== "http://helivanta.localhost:4301" ||
    ZITADEL_ORIGIN !== "http://auth.tesserix.localhost:20080"
  )
    throw new Error("native lockout acceptance requires the isolated explicit fixture");
  const account = specPharmacist();
  if (account.email !== "e2e-account-lockout-native-pharmacist@helivanta.dev")
    throw new Error("native lockout account is not isolated");
  const file = resolve(__dirname, "../../dev/zitadel/secrets/native-staff-setup.pat");
  const info = lstatSync(file);
  if (
    !info.isFile() ||
    info.isSymbolicLink() ||
    (info.mode & 0o777) !== 0o600 ||
    info.size < 1 ||
    info.size > 8192
  )
    throw new Error("private local native setup credential unavailable");
  const pat = readFileSync(file, "utf8").trim();
  await login(page, account);
  const allowed = await page.request.get(permissionPath);
  expect(allowed.status()).toBe(200);
  const principal = (await allowed.json()) as { tenant_id: string; subject: string };
  expect(principal.tenant_id).toBe(tenant);
  if (!/^[A-Za-z0-9_-]{1,200}$/.test(principal.subject))
    throw new Error("normal native sign-in did not establish a bounded subject");
  const cookie = (await page.context().cookies()).find(
    (value) => value.name === "helivanta_session",
  );
  if (!cookie?.value) throw new Error("normal native sign-in did not establish a session");
  const oldCookie = `helivanta_session=${cookie.value}`;
  await page.context().clearCookies();
  const incorrect = await refusedSignIn(page, {
    email: account.email,
    password: `Wrong-local-${randomUUID()}!`,
  });

  async function nativeUser() {
    const response = await fetch(`${ZITADEL_ORIGIN}/v2/users/${principal.subject}`, {
      redirect: "error",
      signal: AbortSignal.timeout(10_000),
      headers: { Authorization: `Bearer ${pat}` },
    });
    expect(response.status).toBe(200);
    const { user } = await response.json();
    expect(user.userId).toBe(principal.subject);
    expect(user.details.resourceOwner).toBe(`helivanta-${tenant}`);
    expect(user.human.email.email).toBe(account.email);
    return user.state as string;
  }
  expect(await nativeUser()).toBe("USER_STATE_ACTIVE");
  expect(
    (await page.request.get(permissionPath, { headers: { Cookie: oldCookie } })).status(),
  ).toBe(200);
  const locked = await fetch(`${ZITADEL_ORIGIN}/v2/users/${principal.subject}/lock`, {
    method: "POST",
    redirect: "error",
    signal: AbortSignal.timeout(10_000),
    headers: { Authorization: `Bearer ${pat}`, "Content-Type": "application/json" },
    body: "{}",
  });
  expect(locked.status).toBe(200);
  await expect.poll(nativeUser, { timeout: 10_000 }).toBe("USER_STATE_LOCKED");
  expect(
    (await page.request.get(permissionPath, { headers: { Cookie: oldCookie } })).status(),
  ).toBe(401);
  const correctButLocked = await refusedSignIn(page, account);
  expect(correctButLocked).toEqual(incorrect);
  const absent = await refusedSignIn(page, {
    email: `absent-${randomUUID()}@helivanta.dev`,
    password: account.password,
  });
  expect(absent).toEqual(incorrect);
});
