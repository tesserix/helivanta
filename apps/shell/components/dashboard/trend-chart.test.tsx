import { screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { TrendChart } from "./trend-chart";

afterEach(() => vi.unstubAllGlobals());

it("keeps chart text legible by sizing the drawing to its mobile container", () => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      constructor(private callback: (entries: unknown[]) => void) {}
      observe() {
        this.callback([{ contentRect: { width: 300 } }]);
      }
      disconnect() {}
    },
  );
  renderWithProviders(
    <TrendChart
      title="Mobile"
      data={data}
      categories={[{ key: "IPD", label: "Inpatients", color: "var(--hms-accent)" }]}
      mode="line"
    />,
  );
  expect(screen.getByRole("img", { name: "Mobile trend" })).toHaveAttribute(
    "viewBox",
    "0 0 300 240",
  );
});

const data = {
  from: "2026-01-01",
  to: "2026-01-02",
  timezone: "UTC",
  interval: "day" as const,
  as_at: "2026-01-02T12:00:00Z",
  totals: { IPD: 12 },
  series: [
    { date: "2026-01-01", counts: { IPD: 12 } },
    { date: "2026-01-02", counts: { IPD: 0 } },
  ],
};

it("switches line and bar rendering while retaining exact accessible values", async () => {
  const categories = [{ key: "IPD", label: "Inpatients", color: "var(--hms-accent)" }];
  const { user, rerender } = renderWithProviders(
    <TrendChart title="Admissions" data={data} categories={categories} mode="line" />,
  );
  expect(
    screen.getByRole("img", { name: "Admissions trend" }).querySelectorAll("polyline"),
  ).toHaveLength(1);
  await user.click(screen.getByText("View data table"));
  expect(screen.getByRole("table", { name: "Admissions data" })).toHaveTextContent("2026-01-0112");
  rerender(<TrendChart title="Admissions" data={data} categories={categories} mode="bar" />);
  expect(
    screen.getByRole("img", { name: "Admissions trend" }).querySelectorAll("rect"),
  ).toHaveLength(2);
  expect(
    screen.getByRole("img", { name: "Admissions trend" }).querySelectorAll("polyline"),
  ).toHaveLength(0);
});

it("renders zero-only series without invalid coordinates", () => {
  renderWithProviders(
    <TrendChart
      title="Empty"
      data={{ ...data, totals: { IPD: 0 }, series: [{ date: "2026-01-01", counts: { IPD: 0 } }] }}
      categories={[{ key: "IPD", label: "Inpatients", color: "var(--hms-accent)" }]}
      mode="line"
    />,
  );
  expect(screen.getByRole("img", { name: "Empty trend" }).innerHTML).not.toMatch(/NaN|Infinity/);
});
