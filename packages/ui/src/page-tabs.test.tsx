import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { PageTabs } from "./page-tabs";

describe("PageTabs", () => {
  it("links each state and marks the current one", () => {
    render(
      <PageTabs
        label="Dispense queue"
        current="dispensed"
        tabs={[
          { key: "waiting", label: "Waiting", href: "/pharmacy?tab=waiting" },
          { key: "dispensed", label: "Dispensed", href: "/pharmacy?tab=dispensed" },
        ]}
      />,
    );
    expect(screen.getByRole("navigation", { name: "Dispense queue" })).toBeVisible();
    const waiting = screen.getByRole("link", { name: "Waiting" });
    const dispensed = screen.getByRole("link", { name: "Dispensed" });
    expect(waiting).toHaveAttribute("href", "/pharmacy?tab=waiting");
    expect(waiting).not.toHaveAttribute("aria-current");
    expect(dispensed).toHaveAttribute("aria-current", "page");
  });
});
