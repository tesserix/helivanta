import { useState } from "react";
import { describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { ListDetail } from "./list-detail";

const props = {
  label: "Bill TEST-001",
  backHref: "/pharmacy/accounts?tab=all",
  backLabel: "Back to bills",
};

describe("ListDetail", () => {
  it("shows the list alone when nothing is open", () => {
    renderWithProviders(<ListDetail {...props} list={<p>TEST list</p>} detail={null} />);
    expect(screen.getByText("TEST list")).toBeVisible();
    expect(screen.queryByRole("region", { name: props.label })).toBeNull();
    expect(screen.queryByRole("link", { name: "Back to bills" })).toBeNull();
  });

  it("names the open record's region and links back to the list", () => {
    renderWithProviders(
      <ListDetail {...props} list={<p>TEST list</p>} detail={<p>TEST detail</p>} />,
    );
    expect(screen.getByRole("region", { name: "Bill TEST-001" })).toHaveTextContent("TEST detail");
    expect(screen.getByRole("link", { name: "Back to bills" })).toHaveAttribute(
      "href",
      props.backHref,
    );
  });

  // #1303 D1: the layout follows main's width through container queries, so
  // the list and the pane are always both rendered, and one grid holds both.
  it("lays out by main's width, never the viewport's", () => {
    renderWithProviders(
      <ListDetail {...props} list={<p>TEST list</p>} detail={<p>TEST detail</p>} />,
    );
    const grid = screen.getByTestId("list-detail");
    expect(grid.className).toContain("@[860px]/main:grid-cols-[minmax(0,1fr)_400px]");
    expect(grid.className).toContain("@[1400px]/main:grid-cols-[minmax(0,1fr)_600px]");
    expect(grid.className).not.toMatch(/(^|\s)(md|lg|xl):grid-cols/);
    // Below the split the list gives way to the record.
    expect(screen.getByTestId("list-detail-list").className.split(" ")).toEqual(
      expect.arrayContaining(["hidden", "@[860px]/main:block"]),
    );
  });

  // Review focus 3: the rail expanding flips the layout; the open record
  // and a half-typed form must survive it, so the pane is never remounted.
  it("keeps the same detail element when the list re-renders", async () => {
    function Harness() {
      const [rows, setRows] = useState(1);
      return (
        <>
          <button type="button" onClick={() => setRows((n) => n + 1)}>
            More rows
          </button>
          <ListDetail
            {...props}
            list={<p>TEST rows {rows}</p>}
            detail={<input aria-label="TEST amount" />}
          />
        </>
      );
    }
    const { user } = renderWithProviders(<Harness />);
    const before = screen.getByLabelText("TEST amount");
    await user.type(before, "125");
    await user.click(screen.getByRole("button", { name: "More rows" }));
    expect(screen.getByText("TEST rows 2")).toBeInTheDocument();
    expect(screen.getByLabelText("TEST amount")).toBe(before);
    expect(screen.getByLabelText("TEST amount")).toHaveValue("125");
  });
});
