import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { DetailHeader } from "./detail-header";

const base = {
  title: "TEST-001",
  noun: "bill",
  closeLabel: "Close document",
};

describe("DetailHeader", () => {
  it("names Previous and Next for the record and honours disabled", async () => {
    const previous = vi.fn();
    const next = vi.fn();
    const onClose = vi.fn();
    const { user } = renderWithProviders(
      <DetailHeader
        {...base}
        previous={{ disabled: true, onClick: previous }}
        next={{ disabled: false, onClick: next }}
        onClose={onClose}
      />,
    );
    expect(screen.getByRole("heading", { name: "TEST-001" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Previous bill" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Next bill" }));
    expect(next).toHaveBeenCalledOnce();
    await user.click(screen.getByRole("button", { name: "Close document" }));
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("leaves Previous and Next out where there are no rows to step through", () => {
    renderWithProviders(
      <DetailHeader
        {...base}
        stepping={false}
        previous={{ disabled: true, onClick: vi.fn() }}
        next={{ disabled: true, onClick: vi.fn() }}
        onClose={vi.fn()}
      />,
    );
    expect(screen.queryByRole("button", { name: "Previous bill" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Next bill" })).toBeNull();
  });

  it("gives the heading to the record, for focus on view", () => {
    const heading: { current: HTMLHeadingElement | null } = { current: null };
    renderWithProviders(
      <DetailHeader
        {...base}
        subtitle="TEST payee"
        headingRef={heading}
        previous={{ disabled: true, onClick: vi.fn() }}
        next={{ disabled: true, onClick: vi.fn() }}
        onClose={vi.fn()}
      />,
    );
    expect(heading.current).toBe(screen.getByRole("heading", { name: "TEST-001" }));
    expect(heading.current!.tabIndex).toBe(-1);
    expect(screen.getByText("TEST payee")).toBeInTheDocument();
  });
});
