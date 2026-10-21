import { test, expect, type Page } from "@playwright/test";
import { login, specAdmin } from "./support/login";

// The shell's enforced Content-Security-Policy (#1113,
// docs/superpowers/specs/2026-10-20-nonce-csp-design.md D7). The policy is only
// worth having if nothing the shell itself renders trips it, so the first test
// fails on any violation anywhere in the app.

type Violation = { directive: string; blocked: string; document: string; source: string };

// exposeBinding outlives navigations, so a violation on a page we have already
// left is still counted. Violations on the IdP's own pages are not ours, and
// neither are those of Next's development overlay: CI's specs project runs
// `next dev`, and next-devtools never ships in a production build.
async function recordViolations(page: Page): Promise<Violation[]> {
  const violations: Violation[] = [];
  const origin = new URL(test.info().project.use.baseURL!).origin;
  await page.exposeBinding("__cspViolation", (_source, violation: Violation) => {
    if (new URL(violation.document).origin !== origin) return;
    if (/_next-devtools_/.test(violation.source)) return;
    violations.push(violation);
  });
  await page.addInitScript(() => {
    document.addEventListener("securitypolicyviolation", (event) => {
      (window as unknown as { __cspViolation: (v: Violation) => void }).__cspViolation({
        directive: event.effectiveDirective,
        blocked: event.blockedURI,
        document: event.documentURI,
        source: event.sourceFile,
      });
    });
  });
  return violations;
}

// A nonce attribute the browser blanks after parsing must not desync hydration.
function recordHydrationErrors(page: Page): string[] {
  const errors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error" && /hydrat/i.test(message.text())) errors.push(message.text());
  });
  return errors;
}

async function pageLinks(page: Page, navigation: string): Promise<string[]> {
  return page
    .getByRole("navigation", { name: navigation })
    .getByRole("link")
    .evaluateAll((links) =>
      links.map((link) => new URL((link as HTMLAnchorElement).href).pathname),
    );
}

test("every shell page renders without a policy violation", async ({ page }) => {
  // Around 45 pages, each compiled on first request under CI's `next dev`.
  test.setTimeout(300_000);
  const violations = await recordViolations(page);
  const hydration = recordHydrationErrors(page);
  const clean = async (where: string) => {
    // Let late scripts and styles (toasts, dialogs) run before judging.
    await page.waitForLoadState("networkidle");
    expect(violations, `violations on ${where}`).toEqual([]);
    expect(hydration, `hydration errors on ${where}`).toEqual([]);
  };

  for (const path of ["/", "/ambulance", "/login"]) {
    await page.goto(path);
    await clean(path);
  }

  // Through /login, the IdP and /api/auth/callback.
  await login(page, specAdmin());
  await clean("sign-in");

  const zones = await pageLinks(page, "Zones");
  expect(zones.length).toBeGreaterThan(3);
  const visited = new Set<string>();
  for (const zone of zones) {
    await page.goto(zone);
    await clean(zone);
    visited.add(new URL(page.url()).pathname);
    const zoneLabel = await page
      .getByRole("navigation", { name: "Zones" })
      .locator("[aria-current]")
      .getAttribute("aria-label");
    for (const path of zoneLabel ? await pageLinks(page, zoneLabel) : []) {
      if (visited.has(path)) continue;
      visited.add(path);
      await page.goto(path);
      await clean(path);
    }
  }

  // Client-side moves render through the RSC payload, not a fresh document.
  await page.goto("/dashboard");
  for (const zone of ["MediCore", "PharmaConnect", "Lab"]) {
    await page.getByRole("navigation", { name: "Zones" }).getByRole("link", { name: zone }).click();
    await expect(page.getByRole("navigation", { name: zone })).toBeVisible();
    await clean(`moving to ${zone}`);
  }

  await page.goto("/no-such-page");
  await clean("/no-such-page");

  // Radix's scroll lock injects a <style> only when a popup opens; its nonce
  // comes from get-nonce (spec D5).
  await page.goto("/dashboard");
  await page.getByLabel("Tenant").click();
  await expect(page.getByRole("listbox")).toBeVisible();
  await page.keyboard.press("Escape");
  await clean("the tenant picker");
});

test("each response carries its own nonce", async ({ page }) => {
  await login(page, specAdmin());
  const nonces: string[] = [];
  for (let i = 0; i < 2; i++) {
    const response = await page.goto("/dashboard");
    const policy = response!.headers()["content-security-policy"] ?? "";
    const nonce = /script-src 'self' 'nonce-([^']+)' 'strict-dynamic'/.exec(policy)?.[1];
    expect(nonce, `policy was: ${policy}`).toBeTruthy();
    nonces.push(nonce!);
  }
  expect(nonces[0]).not.toBe(nonces[1]);
});

// An HTML-injection bug delivers markup inside the server's own response, so
// the payload is spliced into the real /dashboard HTML, with the real policy
// header left in place. Parser-inserted, it gets no trust from 'strict-dynamic'.
const PAYLOAD =
  '<script>window.__pwnedScript = 1</script><img src="/no-such-image" onerror="window.__pwnedHandler = 1">';

test("an injected script does not run and is reported", async ({ page }) => {
  await login(page, specAdmin());
  const violations = await recordViolations(page);
  await page.route("**/dashboard", async (route) => {
    const response = await route.fetch();
    const html = (await response.text()).replace("</body>", `${PAYLOAD}</body>`);
    expect(html).toContain(PAYLOAD);
    await route.fulfill({ response, body: html });
  });
  const reported = page.waitForResponse(
    (response) => new URL(response.url()).pathname === "/api/csp-report",
  );

  await page.goto("/dashboard");

  expect((await reported).status()).toBe(204);
  await expect
    .poll(() => [...new Set(violations.map((violation) => violation.directive))].sort())
    .toEqual(["script-src-attr", "script-src-elem"]);
  expect(
    await page.evaluate(() => {
      const w = window as unknown as { __pwnedScript?: number; __pwnedHandler?: number };
      return [w.__pwnedScript ?? null, w.__pwnedHandler ?? null];
    }),
  ).toEqual([null, null]);
});
