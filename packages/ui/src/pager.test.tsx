import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Pager } from "./pager";

describe("Pager", () => {
  it("shows the range and links both ways in the middle of a list", () => {
    render(
      <Pager
        page={2}
        pageSize={25}
        shown={25}
        total={312}
        previousHref="/q?page=1"
        nextHref="/q?page=3"
        firstHref="/q"
      />,
    );
    expect(screen.getByText("26–50 of 312")).toBeVisible();
    expect(screen.getByRole("link", { name: "Previous" })).toHaveAttribute("href", "/q?page=1");
    expect(screen.getByRole("link", { name: "Next" })).toHaveAttribute("href", "/q?page=3");
  });

  it("disables Next on the last page", () => {
    render(
      <Pager
        page={13}
        pageSize={25}
        shown={12}
        total={312}
        previousHref="/p"
        nextHref={null}
        firstHref="/q"
      />,
    );
    expect(screen.getByText("301–312 of 312")).toBeVisible();
    expect(screen.queryByRole("link", { name: "Next" })).toBeNull();
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
  });

  // #1295 D5: after a reload the earlier cursors are unknown; never guess.
  it("offers First page instead of Previous when the way back is unknown", () => {
    render(
      <Pager
        page={3}
        pageSize={25}
        shown={25}
        total={100}
        previousHref={null}
        nextHref="/n"
        firstHref="/q"
      />,
    );
    expect(screen.queryByRole("link", { name: "Previous" })).toBeNull();
    expect(screen.getByRole("link", { name: "First page" })).toHaveAttribute("href", "/q");
  });

  it("disables Previous on the first page", () => {
    render(
      <Pager
        page={1}
        pageSize={25}
        shown={25}
        total={30}
        previousHref={null}
        nextHref="/n"
        firstHref="/q"
      />,
    );
    expect(screen.getByText("1–25 of 30")).toBeVisible();
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();
    expect(screen.queryByRole("link", { name: "First page" })).toBeNull();
  });

  it("renders nothing for an empty list", () => {
    const { container } = render(
      <Pager
        page={1}
        pageSize={25}
        shown={0}
        total={0}
        previousHref={null}
        nextHref={null}
        firstHref="/q"
      />,
    );
    expect(container).toBeEmptyDOMElement();
  });
});
