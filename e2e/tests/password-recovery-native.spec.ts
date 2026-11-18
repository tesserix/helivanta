import { readFile, stat, unlink } from "node:fs/promises";
import path from "node:path";
import { randomUUID } from "node:crypto";
import { expect, test } from "@playwright/test";
import { login, specAdmin } from "./support/login";

test.use({ trace: "off", video: "off", screenshot: "off" });

// Real owner/PostgreSQL/Zitadel/Global API and browser credential flow.
// Only the external mail provider is replaced by the isolated private outbox.
// This case cannot target a deployed environment or use a real mailbox.
test("native reset revokes old access, refuses the old password and signs in with the new one", async ({
  page,
}) => {
  if (
    process.env.HELIVANTA_PASSWORD_RECOVERY_FIXTURE !== "true" ||
    new URL(test.info().project.use.baseURL!).origin !== "http://helivanta.localhost:4301"
  ) {
    throw new Error("native recovery requires the explicitly enabled isolated development fixture");
  }
  const account = specAdmin();
  if (account.email !== "e2e-password-recovery-native-admin@helivanta.dev")
    throw new Error("native recovery test account is not isolated");
  await login(page, account);
  const allowed = await page.request.get("/api/v1/iam/me/permissions");
  expect(allowed.status()).toBe(200);
  await allowed.dispose();

  await page.goto("/login?workspace=11111111-1111-1111-1111-111111111111");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByLabel("Email or username", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Forgot password?" }).click();
  await expect(page.getByRole("heading", { name: "Recover your password" })).toBeVisible();
  await page.getByLabel("Email", { exact: true }).fill(account.email);
  const deliveryRequest = page.waitForRequest(
    (request) =>
      request.method() === "POST" &&
      new URL(request.url()).pathname === "/api/v1/auth/password-recovery",
  );
  await page.getByRole("button", { name: "Send recovery code", exact: true }).click();
  const delivery = (await deliveryRequest).postDataJSON() as { request_id: string };
  if (
    !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      delivery.request_id,
    )
  )
    throw new Error("mailbox delivery selector is invalid");
  const mailbox = path.resolve(
    path.dirname(test.info().file),
    "../../dev/zitadel/secrets/password-recovery-mailbox",
  );
  const file = path.join(mailbox, delivery.request_id + ".json");
  let code = "";
  await expect
    .poll(
      async () => {
        try {
          const info = await stat(file);
          if (!info.isFile() || (info.mode & 0o777) !== 0o600 || info.size > 512) return false;
          const message = JSON.parse(await readFile(file, "utf8")) as {
            email?: unknown;
            code?: unknown;
          };
          if (
            message.email !== account.email ||
            typeof message.code !== "string" ||
            !/^[0-9]{8}$/.test(message.code)
          )
            return false;
          code = message.code;
          return true;
        } catch {
          return false;
        }
      },
      { message: "isolated mailbox proof must be delivered", timeout: 10000 },
    )
    .toBe(true);
  await unlink(file);
  const group = page.getByRole("group", { name: "Recovery email code" });
  await expect(group.getByRole("textbox")).toHaveCount(8);
  await group.getByRole("textbox", { name: "Digit 1", exact: true }).pressSequentially(code);
  await page.getByRole("button", { name: "Choose a new password", exact: true }).click();
  const newPassword = `Native-recovery-${randomUUID()}!`;
  await page.getByLabel("New password", { exact: true }).fill(newPassword);
  await page.getByLabel("Confirm new password", { exact: true }).fill(newPassword);
  await page.getByRole("button", { name: "Set new password", exact: true }).click();
  const signIn = page.getByRole("link", { name: "Sign in with your new password", exact: true });
  await expect(signIn).toBeVisible({ timeout: 35000 });
  await expect(page.getByRole("button", { name: "Send a new code" })).toHaveCount(0);

  const oldAccess = await page.request.get("/api/v1/iam/me/permissions");
  expect(oldAccess.status()).toBe(401);
  await oldAccess.dispose();

  await signIn.click();
  await page.getByLabel("Email or username", { exact: true }).fill(account.email);
  await page.getByLabel("Password", { exact: true }).fill(account.password);
  const refused = page.waitForResponse(
    (response) =>
      new URL(response.url()).pathname === "/api/v1/auth/login/password" &&
      response.request().method() === "POST",
  );
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  expect((await refused).status()).toBe(401);
  await expect(page.getByRole("alert")).toContainText(/password is incorrect/i);
  await login(page, { email: account.email, password: newPassword });
  const fresh = await page.request.get("/api/v1/iam/me/permissions");
  expect(fresh.status()).toBe(200);
  await fresh.dispose();
});
