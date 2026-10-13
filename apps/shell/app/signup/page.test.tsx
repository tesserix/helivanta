import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import SignupPage from "./page";

it("states signup availability without collecting data or implying a tenant was created", () => {
  render(<SignupPage />);
  expect(
    screen.getByRole("heading", { name: "Self-service signup is coming soon" }),
  ).toBeInTheDocument();
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  expect(screen.getByRole("link", { name: "Sign in to an existing workspace" })).toHaveAttribute(
    "href",
    "/login",
  );
  expect(screen.getByRole("link", { name: "Visit Tesserix" })).toHaveAttribute(
    "href",
    "https://tesserix.app",
  );
});
