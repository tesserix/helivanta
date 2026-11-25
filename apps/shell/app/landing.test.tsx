import { screen, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import LandingPage from "./page";

// next/font is a build-time transform that throws when called outside Next.
vi.mock("next/font/local", () => ({
  default: () => ({ className: "font-site", variable: "font-site-var" }),
}));

beforeEach(() => {
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});

it("sends every workspace call to action to the workspace page and keeps sign-in", () => {
  renderWithProviders(<LandingPage />);
  expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(
    "From boardroom to bedside, one connected hospital.",
  );
  const starts = screen.getAllByRole("link", { name: /start your workspace/i });
  expect(starts).toHaveLength(3);
  for (const start of starts) expect(start).toHaveAttribute("href", "/workspace");
  expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/login?start=1");
  // The trust placeholders and the reviews placeholder left the home page (#1837 D4, D5).
  expect(screen.queryByText("SOC 2 Type II")).toBeNull();
  expect(screen.queryByText(/No customer endorsements/)).toBeNull();
  expect(screen.queryByText("YES HOSPITAL")).not.toBeInTheDocument();
});

it("shows how work moves between management, administration and departments", () => {
  renderWithProviders(<LandingPage />);
  const how = screen.getByRole("region", { name: "How work moves through Helivanta" });
  expect(within(how).getByRole("figure")).toBeInTheDocument();
  expect(within(how).getByRole("group", { name: "Hospital stories" })).toBeInTheDocument();
});

it("follows one patient through the hospital on one shared record", () => {
  renderWithProviders(<LandingPage />);
  const patients = screen.getByRole("region", {
    name: "One patient, one record, every department",
  });
  expect(within(patients).getByRole("list", { name: "Stations on the journey" })).toBeVisible();
});

it("shows pharmacy stock and payments joining one audit trail", () => {
  renderWithProviders(<LandingPage />);
  const audit = screen.getByRole("region", {
    name: "Every tablet and every payment, on one trail",
  });
  expect(within(audit).getByRole("list", { name: "Entries in the trail" })).toBeVisible();
});

it("shows pharmacy stock checked automatically before a named person approves", () => {
  renderWithProviders(<LandingPage />);
  const stock = screen.getByRole("region", { name: "Pharmacy stock checks itself" });
  expect(within(stock).getByRole("list", { name: "Steps in the stock check" })).toBeVisible();
});

it("gives reasons to try Helivanta with what is available today", () => {
  renderWithProviders(<LandingPage />);
  const why = screen.getByRole("region", { name: "Why hospitals try Helivanta" });
  const reasons = within(why).getAllByRole("article");
  expect(reasons).toHaveLength(4);
  for (const reason of reasons) expect(within(reason).getByText(/^Today:/)).toBeInTheDocument();
});

it("describes government and private hospital structures without claiming them as deployed", () => {
  renderWithProviders(<LandingPage />);
  const sectors = screen.getByRole("region", {
    name: "Built for government and private hospitals",
  });
  const government = within(sectors).getByRole("article", { name: "Government" });
  expect(within(government).getByText("District health society")).toBeInTheDocument();
  expect(within(government).getByText(/Rogi Kalyan Samiti/)).toBeInTheDocument();
  expect(within(sectors).getByRole("article", { name: "Private" })).toBeInTheDocument();
  expect(within(sectors).getByText(/sector setup is planned/i)).toBeInTheDocument();
});

it("offers public emergency call access from the homepage", () => {
  renderWithProviders(<LandingPage />);
  // In the nav and in the public links row above the footer (#1837 D4).
  const links = screen.getAllByRole("link", { name: "Emergency help" });
  expect(links).toHaveLength(2);
  for (const link of links) expect(link).toHaveAttribute("href", "/ambulance");
});

// Anek is the public page's headline face only; body text is the shared UI
// face from the root layout (#1834 spec D4). Scoped by variable, so the
// wrapper must not carry the class that sets font-family on everything.
it("scopes Anek to the home page by variable, not by class", () => {
  const { container } = renderWithProviders(<LandingPage />);
  const classes = (container.querySelector(".home-slate") as HTMLElement).className.split(/\s+/);
  expect(classes).toContain("font-site-var");
  expect(classes).not.toContain("font-site");
});

// #1835 D3/D6: the nav carries the mark and the word, nothing else: no tile
// behind the mark and no trailing dot.
it("shows the Helivanta mark and word in the nav", () => {
  renderWithProviders(<LandingPage />);
  const home = screen.getByRole("link", { name: "Helivanta home" });
  expect(home.querySelector("svg path")!.getAttribute("stroke")).toBe("var(--hms-accent)");
  expect(home.textContent).toBe("helivanta");
  // The word is set in Anek through the stylesheet, like the headlines.
  expect(within(home).getByText("helivanta").className).toContain("home-wordmark");
  expect(home.querySelector(".rounded-xl")).toBeNull();
});

// #1837: the home page leads with what exists; the plan is one click away.
it("renders no Planned or Future badge on the home page", () => {
  renderWithProviders(<LandingPage />);
  expect(screen.queryAllByText(/^(Planned|Future)$/)).toHaveLength(0);
  expect(document.querySelector("#trust, #reviews, #next")).toBeNull();
  // D6: no dashed placeholder card (a dashed box holding a heading). Dashed
  // strokes inside the diagrams are drawing, not placeholders.
  for (const dashed of document.querySelectorAll(".border-dashed"))
    expect(dashed.querySelector("h2, h3, h4")).toBeNull();
});

it("links the roadmap from the nav and the platform section", () => {
  renderWithProviders(<LandingPage />);
  const nav = screen.getByRole("navigation", { name: "Main navigation" });
  expect(within(nav).getByRole("link", { name: "Roadmap" })).toHaveAttribute("href", "/roadmap");
  expect(within(nav).getByRole("link", { name: "How it works" })).toHaveAttribute("href", "#how");
  const platform = screen.getByRole("region", { name: /Different responsibilities/ });
  expect(
    within(platform).getByRole("link", {
      name: "The full platform and its status, module by module.",
    }),
  ).toHaveAttribute("href", "/roadmap");
  expect(
    within(platform).getByRole("article", { name: "Staff & hospital management" }),
  ).toBeInTheDocument();
  // Not eligible in this release (#1839), so it is on the roadmap only.
  expect(within(platform).queryByRole("article", { name: "Patient management" })).toBeNull();
  expect(within(platform).queryByRole("article", { name: "Inventory & procurement" })).toBeNull();
});

it("lists only the chapters available today in the chapter bar", () => {
  renderWithProviders(<LandingPage />);
  expect(screen.queryByText("Maintenance and AI")).toBeNull();
  expect(screen.queryByText("What comes next")).toBeNull();
});

it("closes on one plain call to action", () => {
  const { container } = renderWithProviders(<LandingPage />);
  const close = screen.getByRole("region", {
    name: "Bring your hospital onto one connected record.",
  });
  const links = within(close).getAllByRole("link");
  expect(links).toHaveLength(1);
  expect(links[0]).toHaveAccessibleName("Start your workspace");
  expect(close.className).not.toMatch(/bg-muted|rounded/);
  expect(container.querySelector("main")!.lastElementChild).toBe(close);
  expect(screen.getByRole("navigation", { name: "Public pages" })).toBeInTheDocument();
});

it("sets the Today lines as plain paragraphs, not callouts", () => {
  renderWithProviders(<LandingPage />);
  const why = screen.getByRole("region", { name: "Why hospitals try Helivanta" });
  for (const today of within(why).getAllByText(/^Today:/))
    expect(today.className).not.toMatch(/border-l/);
  const sectors = screen.getByText(/sector setup is planned/i);
  expect(sectors.className).not.toMatch(/border-l/);
});
