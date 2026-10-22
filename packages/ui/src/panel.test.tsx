import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { Panel, PanelLink } from "./panel";

const classes = (el: HTMLElement) => el.className.split(/\s+/);

// One card surface (#1275 spec D1/D2): a panel inside a panel is a muted
// inset, never a second bordered card.
it("renders a top-level panel as the padded card surface", () => {
  render(<Panel aria-label="Visits">x</Panel>);
  const panel = screen.getByRole("region", { name: "Visits" });
  expect(panel.tagName).toBe("SECTION");
  expect(classes(panel)).toEqual(expect.arrayContaining(["bg-card", "border", "shadow-xs", "p-5"]));
});

it("renders a flush panel without padding, clipping its content", () => {
  render(<Panel flush aria-label="Orders" />);
  const panel = screen.getByRole("region", { name: "Orders" });
  expect(classes(panel)).toContain("overflow-hidden");
  expect(classes(panel)).not.toContain("p-5");
});

it("renders a panel inside a panel as an inset, not a card", () => {
  render(
    <Panel>
      <Panel as="div" data-testid="inner" />
    </Panel>,
  );
  const inner = classes(screen.getByTestId("inner"));
  expect(inner).toContain("bg-muted");
  expect(inner).not.toContain("border");
  expect(inner).not.toContain("bg-card");
  expect(inner).not.toContain("shadow-xs");
});

it("renders the element it is asked for", () => {
  render(
    <ul>
      <Panel as="li">Row</Panel>
    </ul>,
  );
  expect(screen.getByRole("listitem")).toHaveTextContent("Row");
});

it("renders a link tile as a card on the page and an inset inside a panel", () => {
  render(
    <>
      <PanelLink href="/lab">Lab</PanelLink>
      <Panel>
        <PanelLink href="/pharmacy">Pharmacy</PanelLink>
      </Panel>
    </>,
  );
  expect(classes(screen.getByRole("link", { name: "Lab" }))).toContain("bg-card");
  const inset = classes(screen.getByRole("link", { name: "Pharmacy" }));
  expect(inset).toContain("bg-muted");
  expect(inset).not.toContain("border");
});
