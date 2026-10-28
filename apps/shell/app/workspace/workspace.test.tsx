import { screen, within } from "@testing-library/react";
import { expect, it } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import WorkspacePage from "./page";

it("offers public signup and sign-in while clearly labeling unpublished trust content", () => {
  renderWithProviders(<WorkspacePage />);
  expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Better connected teams.");
  expect(screen.getAllByRole("link", { name: /start your workspace/i })[0]).toHaveAttribute(
    "href",
    "/signup",
  );
  expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/login?start=1");
  expect(screen.getByText("SOC 2 Type II")).toBeInTheDocument();
  expect(screen.getAllByText("Not yet certified")).toHaveLength(2);
  expect(
    screen.getByText(/No customer endorsements or ratings are published yet/),
  ).toBeInTheDocument();
  expect(screen.getByText("Hospital networks")).toBeInTheDocument();
  expect(screen.queryByText("YES HOSPITAL")).not.toBeInTheDocument();
});

it("covers the platform catalogue without presenting planned clinical workflows as available", () => {
  renderWithProviders(<WorkspacePage />);
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
  renderWithProviders(<WorkspacePage />);
  expect(screen.getByRole("link", { name: "Emergency help" })).toHaveAttribute(
    "href",
    "/ambulance",
  );
});
