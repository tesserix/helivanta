import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { PageHeading } from "./page-heading";

// One plain heading for every page (#1255 spec D3): no eyebrow, no serif
// accent, no slogan styling — those read as a landing page, not a tool.
it("renders the title as the page's section heading", () => {
  render(<PageHeading title="Hospital overview" />);
  expect(screen.getByRole("heading", { level: 2, name: "Hospital overview" })).toBeInTheDocument();
});

it("renders the description and actions when given", () => {
  render(
    <PageHeading
      title="Hospital overview"
      description="Activity across the hospital."
      actions={<button type="button">Export</button>}
    />,
  );
  expect(screen.getByText("Activity across the hospital.")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Export" })).toBeInTheDocument();
});

it("carries no marketing type", () => {
  const { container } = render(
    <PageHeading title="Hospital overview" description="Activity." actions={<span />} />,
  );
  expect(container.querySelector("em, i")).toBeNull();
  expect(container.innerHTML).not.toMatch(/font-serif|italic|uppercase/);
});
