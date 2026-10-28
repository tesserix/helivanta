import { test, expect } from "@playwright/test";

const tenant = "11111111-1111-4111-8111-111111111111";
const palettes = [
  { theme: "teal", button: "rgb(51, 65, 85)" }, // the default brand, slate #334155
  { theme: "blue", button: "rgb(29, 93, 168)" },
  { theme: "indigo", button: "rgb(83, 82, 166)" },
] as const;

for (const { theme, button: background } of palettes) {
  for (const layout of ["split", "centered"]) {
    test(`${theme} ${layout} login has a distinct primary action during submission`, async ({
      page,
    }) => {
      await page.route("**/api/v1/auth/branding/*", (route) =>
        route.fulfill({
          json: {
            display_name: "Synthetic Hospital",
            logo_url: "",
            theme,
            login_layout: layout,
            welcome_text: "Welcome to our care team.",
          },
        }),
      );
      await page.route("**/api/v1/auth/login/request/*", (route) =>
        route.fulfill({
          json: {
            id: "V2_appearance",
            client_id: "synthetic-appearance-client",
            redirect_uri: "http://localhost/api/auth/callback",
            scope: ["openid"],
            policies: { allow_password: true, second_factors: [], ignore_unknown_usernames: false },
          },
        }),
      );
      let finish!: () => void;
      const pending = new Promise<void>((resolve) => {
        finish = resolve;
      });
      let submissions = 0;
      await page.route("**/api/v1/auth/login/password", async (route) => {
        submissions++;
        await pending;
        await route.fulfill({
          status: 401,
          json: { error: "invalid_credentials", message: "Sign-in failed. Try again." },
        });
      });

      await page.goto(`/login?authRequest=V2_appearance&tenant=${tenant}`);
      const action = page.getByRole("button", { name: "Sign in", exact: true });
      const loginName = page.getByLabel("Email or username");
      await expect(action).toHaveCSS("background-color", background);
      await expect(action).toHaveCSS("color", "rgb(255, 255, 255)");
      await expect(action).toHaveCSS(
        "width",
        await loginName.evaluate((el) => getComputedStyle(el).width),
      );
      await expect(action).toHaveCSS("min-height", "48px");
      await expect(page.getByLabel("Email or username")).toHaveCSS(
        "background-color",
        "rgb(255, 255, 255)",
      );
      await action.focus();
      await expect(action).toHaveCSS("outline-style", "solid");

      await page.setViewportSize({ width: 390, height: 844 });
      await expect(action).toHaveCSS(
        "width",
        await loginName.evaluate((el) => getComputedStyle(el).width),
      );
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
      ).toBe(true);

      await page.getByLabel("Email or username").fill("appearance@example.invalid");
      await page.getByLabel("Password", { exact: true }).fill("synthetic-password");
      await page.getByLabel("Password", { exact: true }).press("Enter");
      const busy = page.getByRole("button", { name: "Please wait…", exact: true });
      await expect(busy).toBeDisabled();
      await expect(busy).toHaveAttribute("aria-busy", "true");
      await expect(busy).toHaveCSS("background-color", background);
      await expect(busy).toHaveCSS("opacity", "1");
      expect(await busy.evaluate((el) => getComputedStyle(el, "::before").content)).toBe('""');
      await expect.poll(() => submissions).toBe(1);

      finish();
      await expect(
        page.getByRole("alert").filter({ hasText: "Sign-in failed. Try again." }),
      ).toBeVisible();
      await expect(action).toBeEnabled();
    });
  }
}
