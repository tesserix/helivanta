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
    "https://tesserix.com",
  );
  expect(screen.getByText(/All rights reserved/)).toBeInTheDocument();
});
