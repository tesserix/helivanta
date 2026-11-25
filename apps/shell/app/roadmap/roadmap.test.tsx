import { screen, within } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import RoadmapPage, { metadata } from "./page";

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

// #1837 D1, D3, D4: what is not available yet lives here, with its status.
it("says what it is", () => {
  expect(String(metadata.title)).toMatch(/Roadmap/);
  renderWithProviders(<RoadmapPage />);
  expect(screen.getByRole("heading", { level: 1 })).toBeInTheDocument();
  expect(screen.getByText("What comes next")).toBeVisible();
});

it("shows maintenance and compliance checked automatically before a named person decides", () => {
  renderWithProviders(<RoadmapPage />);
  const maintenance = screen.getByRole("region", {
    name: "Maintenance and compliance check themselves",
  });
  expect(
    within(maintenance).getByRole("list", { name: "Steps in the maintenance check" }),
  ).toBeVisible();
  expect(within(maintenance).getByText(/does not certify compliance/)).toBeVisible();
});

it("shows AI agents drafting work that a named person approves", () => {
  renderWithProviders(<RoadmapPage />);
  const ai = screen.getByRole("region", { name: "AI drafts. People decide." });
  expect(within(ai).getByRole("group", { name: "AI scenarios" })).toBeVisible();
  expect(within(ai).getByText(/acts only when a named person approves/)).toBeVisible();
});

it("covers the whole platform catalogue with every status", () => {
  renderWithProviders(<RoadmapPage />);
  const platform = screen.getByRole("region", { name: /Different responsibilities/ });
  const inventory = within(platform).getByRole("article", { name: "Inventory & procurement" });
  expect(within(inventory).getByText("Planned")).toBeInTheDocument();
  const patients = within(platform).getByRole("article", { name: "Patient management" });
  expect(within(patients).getByText("Foundation available")).toBeInTheDocument();
  expect(within(platform).getByText(/Availability varies by module/)).toBeInTheDocument();
});

it("keeps the trust placeholders, labelled as placeholders", () => {
  renderWithProviders(<RoadmapPage />);
  const trust = screen.getByRole("region", { name: "Care deserves a clear standard." });
  expect(within(trust).getByText("SOC 2 Type II")).toBeInTheDocument();
  expect(within(trust).getAllByText("Not yet certified")).toHaveLength(2);
  expect(within(trust).getByText(/not claims of certification/)).toBeInTheDocument();
});

it("sends the nav's chapter links back to the home page", () => {
  renderWithProviders(<RoadmapPage />);
  const nav = screen.getByRole("navigation", { name: "Main navigation" });
  for (const [name, href] of [
    ["How it works", "/#how"],
    ["Why Helivanta", "/#why"],
    ["Platform", "/#platform"],
  ])
    expect(within(nav).getByRole("link", { name })).toHaveAttribute("href", href);
});
