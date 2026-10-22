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

// The shell header's h1 already names the page (#1274), so a page that would
// only repeat it passes no title and gets no second heading.
it("renders no heading without a title", () => {
  render(
    <PageHeading
      description="File leave for staff."
      actions={<button type="button">New request</button>}
    />,
  );
  expect(screen.queryByRole("heading")).toBeNull();
  expect(screen.getByText("File leave for staff.")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "New request" })).toBeInTheDocument();
});
