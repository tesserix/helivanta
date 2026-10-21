import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { chooseOption } from "../../test/select";
import AmbulancePage from "./page";

describe("public AmbulanceConnect page", () => {
  it("offers a direct dial link without login or collecting personal information", () => {
    render(<AmbulancePage />);
    expect(screen.getByRole("heading", { level: 1, name: "Need emergency help?" })).toBeVisible();
    expect(screen.getByRole("link", { name: "Open dialler for 112" })).toHaveAttribute(
      "href",
      "tel:112",
    );
    expect(screen.getByText(/does not book an ambulance/)).toBeVisible();
    expect(screen.getByText(/does not request your location/)).toBeVisible();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /sign in/i })).not.toBeInTheDocument();
  });

  it("does not show Indian call links when the visitor selects outside India", async () => {
    const user = userEvent.setup();
    render(<AmbulancePage />);
    // The design-system Select (#1255 spec D6): a combobox and a listbox.
    await chooseOption(
      user,
      screen.getByRole("combobox", { name: "Where are you?" }),
      "Outside India",
    );
    expect(screen.queryByRole("link", { name: "Open dialler for 112" })).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("contact the local emergency service");
    expect(screen.queryByText("108")).not.toBeInTheDocument();
    await chooseOption(user, screen.getByRole("combobox", { name: "Where are you?" }), "India");
    expect(screen.getByRole("link", { name: "Open dialler for 112" })).toHaveAttribute(
      "href",
      "tel:112",
    );
  });

  // A phone-first emergency page: the control keeps a large touch target.
  it("keeps the country picker large enough to tap", () => {
    render(<AmbulancePage />);
    expect(screen.getByRole("combobox", { name: "Where are you?" })).toHaveClass("min-h-12");
  });

  it("labels regional services as information and links its official source", () => {
    render(<AmbulancePage />);
    const regional = screen.getByRole("region", { name: "About other ambulance numbers" });
    expect(within(regional).getByText("108")).toBeVisible();
    expect(
      within(regional).getByText(/particularly for pregnant women and children/),
    ).toBeVisible();
    expect(within(regional).queryByRole("link", { name: /call|dialler/i })).not.toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: "National Ambulance Services guidelines (2026)" }),
    ).toHaveAttribute("href", expect.stringContaining("https://nhsrcindia.org/"));
  });
});
