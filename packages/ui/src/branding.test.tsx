import { fireEvent, render, screen, within } from "@testing-library/react";
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

// #1835 D6: with no logo the fallback is the Helivanta mark, drawn on the
// surface: bars in the text colour, pulse in the (tenant) accent, no tile.
it("draws the Helivanta mark when there is no tenant logo", () => {
  const { container } = render(<BrandMark branding={DEFAULT_BRANDING} />);
  const pulse = container.querySelector("svg path")!;
  expect(pulse.getAttribute("stroke")).toBe("var(--hms-accent)");
  expect(container.querySelector("svg rect")!.getAttribute("fill")).toBe("currentColor");
  expect(container.querySelector(".bg-primary")).toBeNull();
  expect(screen.queryByRole("img")).toBeNull();
});

it("draws the mark's pulse in the on-ink accent inside the ink panel", () => {
  const { container } = render(
    <LoginSurface branding={{ ...DEFAULT_BRANDING, login_layout: "split" }} entry>
      <h1>Sign in</h1>
    </LoginSurface>,
  );
  const panel = container.querySelector("aside.login-story")!;
  expect(panel.querySelector("svg path")!.getAttribute("stroke")).toBe("var(--hms-accent-on-ink)");
  const header = container.querySelector("header")!;
  expect(header.querySelector("svg path")!.getAttribute("stroke")).toBe("var(--hms-accent)");
});
it("keeps Tesserix attribution on an entry page", () => {
  render(
    <LoginSurface
      entry
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
      entry
    >
      <h1>Sign in</h1>
    </LoginSurface>,
  );
  expect(screen.getByText("Welcome to our care team.")).toBeInTheDocument();
  // The name is in the header below lg and in the ink panel from lg; CSS shows
  // one of them (#1836 D5), so the DOM holds both.
  expect(screen.getAllByText("YES HOSPITAL")).toHaveLength(2);
  expect(screen.getByRole("heading", { name: "Sign in" })).toBeInTheDocument();
});

// #1767 D1/D2: every screen after the entry page is one card. A split
// tenant's story, the attribution footer and the access line all stay off.
it("renders a task screen as the card alone", () => {
  render(
    <LoginSurface
      branding={{
        ...DEFAULT_BRANDING,
        display_name: "YES HOSPITAL",
        welcome_text: "Welcome to our care team.",
        login_layout: "split",
      }}
    >
      <h1>Set up two-step verification</h1>
    </LoginSurface>,
  );
  expect(screen.getByRole("heading", { name: "Set up two-step verification" })).toBeInTheDocument();
  expect(screen.queryByText("Welcome to our care team.")).not.toBeInTheDocument();
  expect(screen.queryByText(/A little less complexity/)).not.toBeInTheDocument();
  expect(screen.queryByText(/All rights reserved/)).not.toBeInTheDocument();
  expect(screen.queryByText(/Need access/)).not.toBeInTheDocument();
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

// #1836 D5: a split entry page is two full-height columns, the brand in an ink
// panel on the left and the form on the canvas. The brand appears once.
it("puts the brand in the ink panel on a split entry page, once", () => {
  const { container } = render(
    <LoginSurface
      branding={{
        ...DEFAULT_BRANDING,
        display_name: "YES HOSPITAL",
        welcome_text: "Welcome to our care team.",
        login_layout: "split",
      }}
      entry
    >
      <h1>Sign in</h1>
    </LoginSurface>,
  );
  const panel = container.querySelector("aside.login-story") as HTMLElement;
  expect(within(panel).getAllByText("YES HOSPITAL").length).toBeGreaterThan(0);
  expect(within(panel).getByText("Welcome to our care team.")).toBeInTheDocument();
  expect(container.querySelector("header")!.className.split(/\s+/)).toContain("lg:hidden");
  // The illustration card is gone: the panel carries the brand, not a diagram.
  expect(panel.textContent).not.toMatch(/Care teams|Laboratory|Your organisation/);
});

// #1767: the platform keeps the centred entry page; only a tenant that chose
// split gets the panel (#1836 D5 as amended).
it("keeps the platform's centred entry page without a panel", () => {
  const { container } = render(
    <LoginSurface branding={DEFAULT_BRANDING} entry homeHref="/">
      <h1>Sign in</h1>
    </LoginSurface>,
  );
  expect(container.querySelector("aside.login-story")).toBeNull();
  expect(container.querySelector("header")!.className.split(/\s+/)).not.toContain("lg:hidden");
});

// A tenant logo is drawn for a light surface; on the ink panel it sits on a
// card chip so a dark logo stays visible (#1836 review).
it("sets a tenant logo on a light chip inside the ink panel", () => {
  const { container } = render(
    <LoginSurface
      branding={{
        ...DEFAULT_BRANDING,
        display_name: "YES HOSPITAL",
        logo_url: "https://example.test/logo.png",
        login_layout: "split",
      }}
      entry
    >
      <h1>Sign in</h1>
    </LoginSurface>,
  );
  const panel = container.querySelector("aside.login-story") as HTMLElement;
  const logo = within(panel).getByRole("img", { name: "YES HOSPITAL logo" });
  expect(logo.parentElement!.className.split(/\s+/)).toContain("bg-card");
  const header = container.querySelector("header") as HTMLElement;
  expect(within(header).getByRole("img").parentElement!.className).not.toContain("bg-card");
});
