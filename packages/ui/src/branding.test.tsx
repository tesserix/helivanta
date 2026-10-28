import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { DEFAULT_BRANDING } from "@helivanta/api";
import { BrandMark, LoginSurface } from "./branding";
it("falls back to the medical mark when the tenant logo is unavailable", () => {
  render(
    <BrandMark
      branding={{
        ...DEFAULT_BRANDING,
        display_name: "YES HOSPITAL",
        logo_url: "https://example.com/logo.png",
      }}
    />,
  );
  fireEvent.error(screen.getByRole("img", { name: "YES HOSPITAL logo" }));
  expect(screen.queryByRole("img")).not.toBeInTheDocument();
  expect(screen.getByText("YES HOSPITAL")).toBeInTheDocument();
});
it("keeps Tesserix attribution in a centered tenant login", () => {
  render(
    <LoginSurface
      branding={{ ...DEFAULT_BRANDING, display_name: "YES HOSPITAL", login_layout: "centered" }}
    >
      Sign in
    </LoginSurface>,
  );
  expect(screen.getByRole("link", { name: "Tesserix" })).toHaveAttribute(
    "href",
    "https://tesserix.app",
  );
  expect(screen.getByText(/All rights reserved/)).toBeInTheDocument();
});
it("preserves tenant welcome content in the split layout", () => {
  render(
    <LoginSurface
      branding={{
        ...DEFAULT_BRANDING,
        display_name: "YES HOSPITAL",
        welcome_text: "Welcome to our care team.",
        login_layout: "split",
      }}
    >
      <h1>Sign in</h1>
    </LoginSurface>,
  );
  expect(screen.getByText("Welcome to our care team.")).toBeInTheDocument();
  expect(screen.getByText("YES HOSPITAL")).toBeInTheDocument();
  expect(screen.getByRole("heading", { name: "Sign in" })).toBeInTheDocument();
});

// #1431 D2: the brand mark is the way home on the platform's own login; a
// hospital's logo never links to Helivanta's marketing page.
it("links the brand mark home only when given a home", () => {
  const { unmount } = render(
    <LoginSurface branding={DEFAULT_BRANDING} homeHref="/">
      <h1>Sign in</h1>
    </LoginSurface>,
  );
  expect(screen.getByRole("link", { name: "Helivanta home" })).toHaveAttribute("href", "/");
  expect(screen.queryByRole("link", { name: /back to/i })).not.toBeInTheDocument();
  unmount();

  render(
    <LoginSurface branding={{ ...DEFAULT_BRANDING, display_name: "YES HOSPITAL" }}>
      <h1>Sign in</h1>
    </LoginSurface>,
  );
  expect(screen.queryByRole("link", { name: /home|back to/i })).not.toBeInTheDocument();
});

// #1431 D3: the first screen of the app follows the app's restraint (#1255 D3).
it("draws no eyebrow, accent headline or decorative icon tile", () => {
  const { container } = render(
    <LoginSurface branding={{ ...DEFAULT_BRANDING, login_layout: "split" }}>
      <h1>Sign in</h1>
    </LoginSurface>,
  );
  expect(container.querySelector(".uppercase")).toBeNull();
  expect(container.querySelector("h2 [class*='hms-accent']")).toBeNull();
  expect(container.querySelector(".login-content > span")).toBeNull();
});
