import { screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import LandingPage from "./page";

it("offers public signup and sign-in while clearly labeling unpublished trust content", () => {
  renderWithProviders(<LandingPage />);
  expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Better connected teams.");
  expect(screen.getAllByRole("link", { name: /start your workspace/i })[0]).toHaveAttribute(
    "href",
    "/signup",
  );
  expect(screen.getByRole("link", { name: "Sign in" })).toHaveAttribute("href", "/login");
  expect(screen.getByText("SOC 2 Type II")).toBeInTheDocument();
  expect(screen.getAllByText("Not yet certified")).toHaveLength(2);
  expect(
    screen.getByText(/No customer endorsements or ratings are published yet/),
  ).toBeInTheDocument();
  expect(screen.getByText("Hospital networks")).toBeInTheDocument();
  expect(screen.queryByText("YES HOSPITAL")).not.toBeInTheDocument();
});
