import { test, expect } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.route("**/api/v1/auth/signup?userId=*", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ state: "email_action_required" }),
    }),
  );
});

test("reopened verified email shows password setup without reusing its code", async ({ page }) => {
  await page.route("**/api/v1/auth/signup?userId=*", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ state: "password_action_required" }),
    }),
  );
  const writes: string[] = [];
  page.on("request", (request) => {
    if (request.method() === "POST" && request.url().includes("/auth/signup"))
      writes.push(request.url());
  });
  await page.goto("/signup/verify#phase=email&userId=owner-synthetic&code=used-proof");
  await expect(page.getByRole("heading", { name: "Your email is already verified" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Verify email address" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Send password setup email" })).toBeVisible();
  expect(writes).toEqual([]);
  expect(new URL(page.url()).hash).toBe("");
});

test("password setup redirects only to its confirmed workspace after seven seconds", async ({
  page,
}) => {
  const tenantId = "91cbb027-0eef-5621-9542-bdfa2c816a78";
  let passwordSet = false;
  const frozen = new Date("2026-11-12T00:00:00Z");
  await page.clock.install({ time: new Date(frozen.getTime() - 60000) });
  await page.clock.pauseAt(frozen);
  await page.route("**/api/v1/auth/signup?userId=*", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        passwordSet
          ? {
              state: "ready",
              name: "QA",
              kind: "hospital",
              tenantId,
              resourceId: "1aee9771-66c3-445c-8b3e-b18f472ca409",
            }
          : { state: "password_action_required" },
      ),
    }),
  );
  await page.route("**/api/v1/auth/signup/password", (route) => {
    passwordSet = true;
    return route.fulfill({
      status: 202,
      contentType: "application/json",
      body: JSON.stringify({ state: "provisioning" }),
    });
  });
  await page.route(`**/login?workspace=${tenantId}`, (route) =>
    route.fulfill({ status: 200, contentType: "text/html", body: "<h1>Workspace sign-in</h1>" }),
  );
  await page.goto("/signup/verify#phase=password&userId=owner-synthetic&code=one-use-proof");
  await expect
    .poll(async () => {
      await page.clock.runFor(0);
      return page.getByLabel("New password", { exact: true }).count();
    })
    .toBe(1);
  await page.getByLabel("New password", { exact: true }).fill("Synthetic-password-only-42!");
  await page.getByLabel("Confirm password", { exact: true }).fill("Synthetic-password-only-42!");
  await page.getByRole("button", { name: "Set password", exact: true }).click();
  await expect
    .poll(async () => {
      await page.clock.runFor(0);
      return page.getByRole("link", { name: "Sign in now" }).count();
    })
    .toBe(1);
  await expect(page.getByRole("heading", { name: "Your password is set" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Sign in now" })).toHaveAttribute(
    "href",
    `/login?workspace=${tenantId}`,
  );
  await expect(page.getByRole("status")).toContainText("7 seconds");
  await page.clock.runFor(6999);
  expect(new URL(page.url()).pathname).toBe("/signup/verify");
  await page.clock.runFor(1);
  await expect(page).toHaveURL(new RegExp(`/login\\?workspace=${tenantId}$`));
  await expect(page.getByRole("heading", { name: "Workspace sign-in" })).toBeVisible();
});

test("new tenant signup requires its own account instead of adopting another organisation's user", async ({
  page,
}) => {
  await page.route("**/api/v1/auth/signup", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ state: "email_action_required" }),
    }),
  );
  await page.goto("/signup");
  await expect(page.getByRole("heading", { name: "Verify your account" })).toBeFocused();
  await expect(page.getByRole("button", { name: "Continue with an existing account" })).toHaveCount(
    0,
  );
  await expect(page.getByText(/separate login for this workspace/)).toBeVisible();
});

test("ready workspace sign-in pins the native organisation with PKCE", async ({ page }) => {
  const tenant = "11111111-1111-4111-8111-111111111111";
  await page.route("**/api/v1/auth/signup", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        state: "ready",
        name: "TEST Hospital",
        kind: "hospital",
        tenantId: tenant,
        resourceId: "0199e000-0000-7000-8000-000000000010",
      }),
    }),
  );
  await page.route("**/oauth/v2/authorize?**", (route) =>
    route.fulfill({
      status: 200,
      contentType: "text/html",
      body: "<p>Captured sign-in request</p>",
    }),
  );
  await page.goto("/signup");
  const origin = new URL(page.url()).origin;
  const authorization = page.waitForRequest(
    (request) => new URL(request.url()).pathname === "/oauth/v2/authorize",
  );
  await page.getByRole("button", { name: "Open workspace" }).click();
  const url = new URL((await authorization).url());
  expect(url.searchParams.get("scope")?.split(" ")).toContain(
    `urn:zitadel:iam:org:id:helivanta-${tenant}`,
  );
  expect(url.searchParams.get("prompt")).toBe("login");
  expect(url.searchParams.get("max_age")).toBe("0");
  expect(url.searchParams.get("code_challenge_method")).toBe("S256");
  expect(url.searchParams.get("code_challenge")).toBeTruthy();
  expect(url.searchParams.get("redirect_uri")).toBe(
    new URL("/api/auth/callback", origin).toString(),
  );
});

test("new owners can reach signup and choose their own independent pharmacy", async ({ page }) => {
  await page.goto("/signup");
  await expect(page.getByRole("heading", { name: "Create your workspace" })).toBeVisible();
  await page.getByRole("combobox", { name: "Workspace type" }).click();
  await page.getByRole("option", { name: "Independent pharmacy" }).click();
  await page.getByRole("combobox", { name: "State" }).click();
  await page.getByRole("option", { name: "Odisha" }).click();
  await expect(page.getByLabel("Owner email")).toBeVisible();
  await expect(page.getByLabel("Head office address line 1")).toBeVisible();
  await expect(page.getByLabel("Organisation phone")).toBeVisible();
  await expect(page.getByRole("button", { name: "Create workspace" })).toBeVisible();
  await expect(page.getByText("Self-service signup is coming soon")).toHaveCount(0);
});

test("signup never reports readiness when its provisioner is unavailable", async ({ page }) => {
  await page.route("**/api/v1/auth/signup", (route) =>
    route.fulfill({
      status: 503,
      contentType: "application/json",
      body: JSON.stringify({
        error: "onboarding_unavailable",
        message: "Workspace setup is temporarily unavailable.",
      }),
    }),
  );
  await page.goto("/signup");
  await expect(page.getByText("Workspace setup is unavailable. Try again.")).toBeVisible();
  await expect(page.getByText("Your workspace is ready")).toHaveCount(0);
});

for (const kind of ["hospital", "pharmacy"] as const) {
  test(`${kind} signup submits business contacts and headquarters with its main branch`, async ({
    page,
  }) => {
    let submitted: unknown;
    await page.route("**/api/v1/auth/signup", async (route) => {
      if (route.request().method() !== "POST") return route.continue();
      submitted = route.request().postDataJSON();
      await route.fulfill({
        status: 202,
        contentType: "application/json",
        body: JSON.stringify({ state: "provisioning" }),
      });
    });
    await page.goto("/signup");
    await page.getByLabel("Organisation name").fill("TEST Signup Organisation");
    if (kind === "pharmacy") {
      await page.getByRole("combobox", { name: "Workspace type" }).click();
      await page.getByRole("option", { name: "Independent pharmacy" }).click();
    }
    await page.getByLabel("Organisation phone").fill("+919876543210");
    await page.getByLabel("Organisation email").fill("office@example.test");
    await page.getByLabel("Head office address line 1").fill("10 Headquarters Road");
    await page.getByLabel("Head office city").fill("Pune");
    await page.getByLabel("Head office district").fill("Pune");
    await page.getByLabel("Head office PIN code").fill("411001");
    await page.getByLabel("Main branch name").fill("Test Main Branch");
    if (kind === "pharmacy") {
      await page
        .getByRole("checkbox", { name: "Main branch uses the head office address" })
        .uncheck();
      await page.getByLabel("Main branch address line 1").fill("20 Branch Road");
      await page.getByLabel("Main branch city").fill("Bhubaneswar");
      await page.getByLabel("Main branch district").fill("Khordha");
      await page.getByLabel("Main branch PIN code").fill("751001");
      await page.getByRole("combobox", { name: "Main branch state" }).click();
      await page.getByRole("option", { name: "Odisha" }).click();
    }
    await page.getByLabel("Given name").fill("Test");
    await page.getByLabel("Family name").fill("Owner");
    await page.getByLabel("Owner email").fill("owner@example.test");
    await page.getByRole("button", { name: "Create workspace" }).click();
    const headOffice = {
      line1: "10 Headquarters Road",
      line2: "",
      city: "Pune",
      district: "Pune",
      postalCode: "411001",
      stateCode: "MH",
      countryCode: "IN",
    };
    await expect
      .poll(() => submitted)
      .toEqual({
        name: "TEST Signup Organisation",
        kind,
        countryCode: "IN",
        stateCode: "MH",
        owner: { givenName: "Test", familyName: "Owner", email: "owner@example.test" },
        profile: {
          contact: { phone: "+919876543210", email: "office@example.test" },
          headOffice,
          mainBranch: {
            name: "Test Main Branch",
            address:
              kind === "hospital"
                ? headOffice
                : {
                    line1: "20 Branch Road",
                    line2: "",
                    city: "Bhubaneswar",
                    district: "Khordha",
                    postalCode: "751001",
                    stateCode: "OD",
                    countryCode: "IN",
                  },
          },
        },
      });
    const status = page.getByRole("heading", { name: "Setting up your workspace" });
    await expect(status).toBeFocused();
    await expect(status).toBeInViewport();
    await expect(page.getByText("Your workspace is ready")).toHaveCount(0);
  });
}

test("expired email proof offers a fresh verification link with expiry and shared delivery limits", async ({
  page,
}) => {
  const deliveries: string[] = [];
  await page.route("**/api/v1/auth/signup/verify-email", (route) =>
    route.fulfill({
      status: 400,
      contentType: "application/json",
      body: JSON.stringify({ error: "invalid_request", message: "Invalid proof" }),
    }),
  );
  await page.route("**/api/v1/auth/signup/*-link", (route) => {
    deliveries.push(new URL(route.request().url()).pathname);
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ state: "email_action_required" }),
    });
  });
  await page.goto("/signup/verify#phase=email&userId=owner-synthetic&code=expired-proof");
  await expect(page.getByText(/Links expire after 1 hour/)).toBeVisible();
  await expect(page.getByText(/3 emails per hour and 10 per 24 hours/)).toBeVisible();
  await expect.poll(() => new URL(page.url()).hash).toBe("");
  expect(deliveries).toEqual([]);
  await page.getByRole("button", { name: "Verify email address" }).click();
  await expect(
    page.getByRole("alert").filter({ hasText: "This link may have expired" }),
  ).toHaveText(
    "This link may have expired or already been used. Check your password or request a fresh email link.",
  );
  await page.getByRole("button", { name: "Request a fresh email link" }).click();
  await expect.poll(() => deliveries).toEqual(["/api/v1/auth/signup/email-link"]);
  await expect(page.getByRole("status")).toContainText("If this request can receive an email");
});

test("another workspace opens a blank signup without provisioning or deleting the existing one", async ({
  page,
}) => {
  let fresh = false;
  const commands: string[] = [];
  await page.route("**/api/v1/auth/signup**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (request.method() === "POST") {
      commands.push(path);
      expect(path).toBe("/api/v1/auth/signup/new");
      expect(request.postDataJSON()).toEqual({});
      fresh = true;
    }
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify(
        fresh
          ? { state: "not_started" }
          : {
              state: "ready",
              name: "Existing hospital",
              kind: "hospital",
              tenantId: "11111111-1111-4111-8111-111111111111",
              resourceId: "0199e000-0000-7000-8000-000000000010",
            },
      ),
    });
  });
  await page.goto("/signup");
  await page.getByRole("button", { name: "Create another workspace" }).click();
  await expect(page.getByRole("heading", { name: "Create your workspace" })).toBeFocused();
  await expect(page.getByLabel("Owner email")).toHaveValue("");
  await expect(page.getByLabel("Organisation name")).toHaveValue("");
  expect(commands).toEqual(["/api/v1/auth/signup/new"]);
});
