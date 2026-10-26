import { screen, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import LandingPage from "./page";

// next/font is a build-time transform that throws when called outside Next.
vi.mock("next/font/local", () => ({ default: () => ({ className: "font-site" }) }));

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
  expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/login");
  expect(screen.getByText("SOC 2 Type II")).toBeInTheDocument();
  expect(screen.getAllByText("Not yet certified")).toHaveLength(2);
  expect(
    screen.getByText(/No customer endorsements or ratings are published yet/),
  ).toBeInTheDocument();
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

it("shows maintenance and compliance checked automatically before a named person decides", () => {
  renderWithProviders(<LandingPage />);
  const maintenance = screen.getByRole("region", {
    name: "Maintenance and compliance check themselves",
  });
  expect(
    within(maintenance).getByRole("list", { name: "Steps in the maintenance check" }),
  ).toBeVisible();
  expect(within(maintenance).getByText(/does not certify compliance/)).toBeVisible();
});

it("shows AI agents drafting work that a named person approves", () => {
  renderWithProviders(<LandingPage />);
  const ai = screen.getByRole("region", { name: "AI drafts. People decide." });
  expect(within(ai).getByRole("group", { name: "AI scenarios" })).toBeVisible();
  expect(within(ai).getByText(/acts only when a named person approves/)).toBeVisible();
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

it("covers the platform catalogue without presenting planned clinical workflows as available", () => {
  renderWithProviders(<LandingPage />);
  const platform = screen.getByRole("region", { name: /Different responsibilities/ });
  for (const name of [
    "Patient management",
    "Appointments & reception",
    "Inventory & procurement",
    "Billing & insurance",
    "Staff & hospital management",
    "CRM & referrals",
    "Patient portal",
    "Emergency & ambulance",
    "Home care",
    "Assets & facilities",
    "Safety & quality",
    "Reporting & administration",
  ]) {
    expect(within(platform).getByRole("heading", { name })).toBeInTheDocument();
  }
  const inventory = within(platform).getByRole("article", { name: "Inventory & procurement" });
  expect(within(inventory).getByText("Planned")).toBeInTheDocument();
  const patients = within(platform).getByRole("article", { name: "Patient management" });
  expect(within(patients).getByText("Foundation available")).toBeInTheDocument();
  expect(within(platform).getByText(/Availability varies by module/)).toBeInTheDocument();
});

it("offers public emergency call access from the homepage", () => {
  renderWithProviders(<LandingPage />);
  expect(screen.getByRole("link", { name: "Emergency help" })).toHaveAttribute(
    "href",
    "/ambulance",
  );
});
