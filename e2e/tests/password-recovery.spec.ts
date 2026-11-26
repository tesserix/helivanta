import { expect, test } from "@playwright/test";

// Browser consumer contracts with a stubbed owner. Native reset/new-password
// sign-in and retained owner commands require separate deployed acceptance.
test("an uncertain password is never resubmitted after refresh or reload", async ({ page }) => {
  let state = "ready";
  const writes: { action: string; body: Record<string, string> }[] = [];
  await page.route("**/api/v1/auth/password-recovery**", async (route) => {
    const request = route.request();
    const action = new URL(request.url()).pathname.split("/").at(-1)!;
    if (request.method() === "GET") {
      await route.fulfill({ status: 200, json: { state } });
      return;
    }
    writes.push({ action, body: request.postDataJSON() });
    if (action === "prepare") state = "prepared";
    if (action === "cancel") state = "cancelled";
    if (action === "password") {
      await route.fulfill({ status: 503, json: { error: "password_recovery_unavailable" } });
      return;
    }
    await route.fulfill({ status: 200, json: { state } });
  });
  await page.goto("/recover-password?authRequest=V2_recovery");
  await expect(page.getByRole("heading", { name: "Recover your password" })).toBeVisible();
  await expect(page.getByLabel("New password", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Choose a new password" }).click();
  await page.getByLabel("New password", { exact: true }).fill("Synthetic-password-only-42!");
  await page
    .getByLabel("Confirm new password", { exact: true })
    .fill("Synthetic-password-only-42!");
  await page.getByRole("button", { name: "Set new password" }).click();
  await expect(page.getByText(/couldn't confirm whether that worked/i)).toBeVisible();
  await expect(page.getByLabel("New password", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Check again" }).click();
  await expect(page.getByRole("button", { name: "Cancel and start again" })).toBeVisible();
  await page.reload();
  await expect(page.getByLabel("New password", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Cancel and start again" }).click();
  await expect(page.getByRole("button", { name: "Choose a new password" })).toBeVisible();
  expect(writes.map(({ action }) => action)).toEqual(["prepare", "password", "cancel"]);
  expect(writes[0]?.body).not.toHaveProperty("password");
  expect(writes[1]?.body.command_id).toBe(writes[0]?.body.command_id);
  expect(writes[2]?.body).toEqual({});
  expect(
    await page.evaluate(() => JSON.stringify([window.localStorage, window.sessionStorage])),
  ).not.toContain("Synthetic-password-only-42!");
});

test("verified mailbox recovery stops resends and completes through a prepared password attempt", async ({
  page,
}) => {
  let verified = false;
  let state = "ready";
  const writes: string[] = [];
  await page.route("**/api/v1/auth/password-recovery**", async (route) => {
    const request = route.request();
    const action = new URL(request.url()).pathname.split("/").at(-1)!;
    if (request.method() === "GET") {
      await route.fulfill(
        verified
          ? { status: 200, json: { state } }
          : { status: 404, json: { error: "password_recovery_expired" } },
      );
      return;
    }
    writes.push(action);
    if (action === "password-recovery") {
      await route.fulfill({ status: 202, json: { state: "check_email" } });
      return;
    }
    if (action === "verify") {
      expect(request.postDataJSON()).toEqual({ code: "01234567" });
      verified = true;
      await route.fulfill({ status: 200, json: { state: "verified" } });
      return;
    }
    state = action === "prepare" ? "prepared" : "completed";
    await route.fulfill({ status: 200, json: { state } });
  });
  await page.goto("/recover-password?authRequest=V2_recovery");
  await page.getByLabel("Email", { exact: true }).fill("person@example.test");
  await page.getByRole("button", { name: "Send recovery code" }).click();
  const code = page.getByRole("group", { name: "Recovery email code" });
  await expect(code.getByRole("textbox")).toHaveCount(8);
  await code.getByRole("textbox", { name: "Digit 1", exact: true }).pressSequentially("01234567");
  await expect(page.getByRole("button", { name: "Send a new code" })).toHaveCount(0);
  await page.getByRole("button", { name: "Choose a new password" }).click();
  await page.getByLabel("New password", { exact: true }).fill("Synthetic-password-only-42!");
  await page
    .getByLabel("Confirm new password", { exact: true })
    .fill("Synthetic-password-only-42!");
  await page.getByRole("button", { name: "Set new password" }).click();
  await expect(page.getByRole("link", { name: "Sign in with your new password" })).toHaveAttribute(
    "href",
    "/login?authRequest=V2_recovery",
  );
  expect(writes).toEqual(["password-recovery", "verify", "prepare", "password"]);
});
