import { randomUUID } from "node:crypto";
import { lstatSync, readFileSync } from "node:fs";
import { resolve } from "node:path";
import { expect, test, type Page, type APIRequestContext, type Browser } from "@playwright/test";
import { specAdmin, specPharmacist, type Credentials } from "./login";
import { uuidv7 } from "./pharmacy";
import { generateTOTP } from "./totp";
import { ZITADEL_ORIGIN } from "./hosts";

const permissions = "/api/v1/iam/me/permissions";

async function identity(request: APIRequestContext, tenant: string) {
  const response = await request.get(permissions);
  expect(response.status()).toBe(200);
  const result = (await response.json()) as { tenant_id: string; subject: string };
  expect(result.tenant_id).toBe(tenant);
  return {
    subject: result.subject,
    headers: {
      "X-Helivanta-Expected-Tenant": result.tenant_id,
      "X-Helivanta-Expected-Subject": result.subject,
    },
  };
}
async function password(page: Page, account: Credentials, tenant: string) {
  await page.goto(`/login?workspace=${tenant}`);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await page.getByLabel("Email or username", { exact: true }).fill(account.email);
  await page.getByLabel("Password", { exact: true }).fill(account.password);
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
}
async function code(page: Page, label: string, secret: string) {
  const group = page.getByRole("group", { name: label, exact: true });
  await expect(group).toBeVisible();
  await group
    .getByRole("textbox", { name: "Digit 1", exact: true })
    .pressSequentially(generateTOTP(secret));
}

export async function exerciseNativeStaffMFARecovery(
  { page, browser }: { page: Page; browser: Browser },
  kind: "hospital" | "pharmacy",
) {
  const tenant =
    kind === "hospital"
      ? "11111111-1111-1111-1111-111111111111"
      : "22222222-2222-2222-2222-222222222222";
  const slug = kind === "hospital" ? "staff-mfa-recovery-native" : "staff-mfa-pharmacy-native";
  if (
    process.env.HELIVANTA_STAFF_MFA_RECOVERY_FIXTURE !== "true" ||
    new URL(test.info().project.use.baseURL!).origin !== "http://helivanta.localhost:4301" ||
    ZITADEL_ORIGIN !== "http://auth.tesserix.localhost:20080"
  )
    throw new Error("native Staff MFA recovery requires the isolated explicit fixture");
  const target = specPharmacist();
  if (target.email !== `e2e-${slug}-pharmacist@helivanta.dev`)
    throw new Error("native Staff MFA account is not isolated");
  const file = resolve(__dirname, "../../../dev/zitadel/secrets/helivanta-seed.pat");
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
  async function native(path: string, data?: unknown) {
    const response = await fetch(ZITADEL_ORIGIN + path, {
      method: data === undefined ? "GET" : "POST",
      redirect: "error",
      signal: AbortSignal.timeout(10_000),
      headers: { Authorization: `Bearer ${pat}`, "Content-Type": "application/json" },
      ...(data === undefined ? {} : { body: JSON.stringify(data) }),
    });
    expect(response.status).toBe(200);
    return await response.json();
  }
  const targetContext = await browser.newContext({ baseURL: test.info().project.use.baseURL });
  const reviewerContext = await browser.newContext({ baseURL: test.info().project.use.baseURL });
  const foreignContext =
    kind === "pharmacy"
      ? await browser.newContext({ baseURL: test.info().project.use.baseURL })
      : null;
  try {
    const targetPage = await targetContext.newPage();
    const reviewerPage = await reviewerContext.newPage();
    await password(targetPage, target, tenant);
    await targetPage.waitForURL((url) => url.pathname === "/dashboard");
    const colleague = await identity(targetPage.request, tenant);
    const nativeUser = await native(`/v2/users/${colleague.subject}`);
    expect(nativeUser.user.details.resourceOwner).toBe(`helivanta-${tenant}`);
    expect(nativeUser.user.human.email.email).toBe(target.email);
    const initial = await native(`/v2/users/${colleague.subject}/totp`, {});
    if (typeof initial.secret !== "string" || !/^[A-Z2-7]+$/.test(initial.secret))
      throw new Error("native initial factor unavailable");
    const originalSecret = initial.secret as string;
    await native(`/v2/users/${colleague.subject}/totp/verify`, {
      code: generateTOTP(originalSecret),
    });
    await targetContext.clearCookies();
    await password(targetPage, target, tenant);
    await code(targetPage, "Verification code", originalSecret);
    await targetPage.waitForURL((url) => url.pathname === "/dashboard");
    await identity(targetPage.request, tenant);
    const old = (await targetContext.cookies()).find(
      (cookie) => cookie.name === "helivanta_session",
    );
    if (!old?.value) throw new Error("normal native sign-in did not establish a session");
    const oldCookie = `helivanta_session=${old.value}`;
    await password(page, specAdmin(), tenant);
    await page.waitForURL((url) => url.pathname === "/dashboard");
    const actor = await identity(page.request, tenant);
    await password(
      reviewerPage,
      { email: `e2e-${slug}-reviewer@helivanta.dev`, password: target.password },
      tenant,
    );
    await reviewerPage.waitForURL((url) => url.pathname === "/dashboard");
    const reviewer = await identity(reviewerPage.request, tenant);
    const hospital = kind === "hospital" ? randomUUID() : uuidv7(),
      staff = randomUUID(),
      link = uuidv7();
    const root = `/api/v1/workforce/${kind === "hospital" ? "hospitals" : "pharmacies"}/${hospital}`;
    const name = `Synthetic native recovery ${hospital.slice(0, 8)}`;
    const staffName = "Synthetic Native Recovery Colleague";
    if (kind === "pharmacy") {
      const pharma = await page.request.put(`/api/v1/pharmacy/pharmacies/${hospital}`, {
        headers: actor.headers,
        data: { code: `NATIVE-MFA-${hospital.slice(-8)}`, name, ownership_type: "independent" },
      });
      expect(pharma.status()).toBe(200);
    }
    expect(
      (
        await page.request.put(root, {
          headers: actor.headers,
          data: kind === "hospital" ? { name } : {},
        })
      ).status(),
    ).toBe(200);
    expect(
      (
        await page.request.put(`${root}/access/${reviewer.subject}`, {
          headers: actor.headers,
          data: { level: "manager" },
        })
      ).status(),
    ).toBe(200);
    expect(
      (
        await page.request.put(
          `${root}/staff/${staff}${kind === "pharmacy" ? "?directory_context=v1" : ""}`,
          {
            headers: actor.headers,
            data: {
              ...(kind === "pharmacy"
                ? { expected_version: 0, directory_context_version: "v1" }
                : {}),
              employee_code: "NATIVE-MFA-01",
              display_name: staffName,
              job_category: "pharmacist",
              employment_status: "active",
            },
          },
        )
      ).status(),
    ).toBe(200);
    const links = `${root}/staff/${staff}/${kind === "hospital" ? "login-links" : "account-links"}`;
    expect(
      (
        await page.request.put(`${links}/${link}`, {
          headers: actor.headers,
          data: {
            ...(kind === "pharmacy" ? { expected_staff_version: 1 } : {}),
            subject: colleague.subject,
            reason: "Verified isolated native Staff account",
          },
        })
      ).status(),
    ).toBe(200);
    expect(
      (
        await reviewerPage.request.fetch(
          `${links}/${link}/${kind === "hospital" ? "decision" : `commands/${uuidv7()}`}`,
          {
            method: kind === "hospital" ? "POST" : "PUT",
            headers: reviewer.headers,
            data: {
              expected_version: 1,
              decision: "approve",
              reason: "Independent synthetic identity review",
            },
          },
        )
      ).status(),
    ).toBe(200);
    const stale = uuidv7();
    expect(
      (
        await page.request.put(`${root}/staff/${staff}/mfa-recoveries/${stale}`, {
          headers: actor.headers,
          data: {
            commandId: stale,
            expectedLinkId: link,
            expectedLinkVersion: 3,
            expectedStaffVersion: 1,
            reason: "Stale source must not remove a factor",
          },
        })
      ).status(),
    ).toBe(409);
    if (foreignContext) {
      const foreignPage = await foreignContext.newPage();
      const foreignTenant = "11111111-1111-1111-1111-111111111111";
      await password(
        foreignPage,
        { email: "e2e-staff-mfa-pharmacy-native-foreign@helivanta.dev", password: target.password },
        foreignTenant,
      );
      await foreignPage.waitForURL((url) => url.pathname === "/dashboard");
      const foreign = await identity(foreignPage.request, foreignTenant);
      const command = uuidv7();
      expect(
        (
          await foreignPage.request.put(`${root}/staff/${staff}/mfa-recoveries/${command}`, {
            headers: foreign.headers,
            data: {
              commandId: command,
              expectedLinkId: link,
              expectedLinkVersion: 2,
              expectedStaffVersion: 1,
              reason: "Foreign tenant cannot reset this Staff account",
            },
          })
        ).status(),
      ).toBe(404);
      expect(
        (
          await page.request.get(`/api/v1/workforce/hospitals/${hospital}/staff/${staff}`, {
            headers: actor.headers,
          })
        ).status(),
      ).toBe(404);
    }
    const before = await native(`/v2/users/${colleague.subject}/authentication_methods`);
    expect(before.authMethodTypes.includes("AUTHENTICATION_METHOD_TYPE_TOTP")).toBe(true);
    expect(
      (await targetPage.request.get(permissions, { headers: { Cookie: oldCookie } })).status(),
    ).toBe(200);

    await page.goto(kind === "hospital" ? "/workforce" : "/pharmacy/staff");
    await page
      .getByRole("combobox", {
        name: kind === "hospital" ? "Hospital" : "Pharmacy employer",
        exact: true,
      })
      .click();
    await page.getByRole("option", { name, exact: true }).click();
    await page
      .getByRole("button", {
        name: kind === "hospital" ? `Account link for ${staffName}` : `Edit ${staffName}`,
        exact: true,
      })
      .click();
    await page.getByRole("button", { name: "Reset authenticator", exact: true }).click();
    await page
      .getByLabel("Reason for authenticator reset", { exact: true })
      .fill("Verified synthetic Staff lost their authenticator");
    await page
      .getByRole("checkbox", { name: "I verified the Staff member and authorize this reset." })
      .check();
    const dispatch = page.waitForResponse(
      (response) =>
        response.request().method() === "PUT" &&
        new URL(response.url()).pathname.startsWith(`${root}/staff/${staff}/mfa-recoveries/`),
    );
    await page.getByRole("button", { name: "Confirm authenticator reset", exact: true }).click();
    const acknowledged = await dispatch;
    expect(acknowledged.status()).toBe(202);
    expect((await acknowledged.json()).state).toBe("awaiting_enrollment");
    await expect(
      page.getByRole("status").filter({ hasText: "The authenticator reset was acknowledged." }),
    ).toBeVisible();
    const removed = await native(`/v2/users/${colleague.subject}/authentication_methods`);
    expect((removed.authMethodTypes ?? []).includes("AUTHENTICATION_METHOD_TYPE_TOTP")).toBe(false);
    expect(
      (await targetPage.request.get(permissions, { headers: { Cookie: oldCookie } })).status(),
    ).toBe(401);
    expect(
      (
        await targetPage.request.post("/api/v1/auth/renew", { headers: { Cookie: oldCookie } })
      ).status(),
    ).toBe(401);

    // Native auth_time has whole-second precision; cross the committed revocation cutoff.
    await targetPage.waitForTimeout(1100);
    await targetContext.clearCookies();
    await password(targetPage, target, tenant);
    await expect(
      targetPage.getByRole("heading", { name: "Set up two-step verification", exact: true }),
    ).toBeVisible();
    const replacement = (
      await targetPage.getByLabel("Authenticator setup key", { exact: true }).textContent()
    )?.trim();
    if (!replacement || !/^[A-Z2-7]+$/.test(replacement) || replacement === originalSecret)
      throw new Error("native recovery did not issue a replacement factor");
    expect((await targetPage.request.get(permissions)).status()).toBe(401);
    await code(targetPage, "Confirmation code", replacement);
    await targetPage.waitForURL((url) => url.pathname === "/dashboard", { timeout: 35000 });
    await identity(targetPage.request, tenant);
    expect((await targetPage.request.post("/api/v1/auth/renew")).status()).toBe(200);
    await identity(targetPage.request, tenant);
    await page.getByRole("button", { name: "Check recovery status", exact: true }).click();
    await expect(
      page.getByRole("status").filter({ hasText: "This recovery request is complete." }),
    ).toBeVisible();
    expect(
      (await targetPage.request.get(permissions, { headers: { Cookie: oldCookie } })).status(),
    ).toBe(401);
  } finally {
    await foreignContext?.close();
    await reviewerContext.close();
    await targetContext.close();
  }
}
