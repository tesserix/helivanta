import { screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { CohortBreakdown } from "./cohort-breakdown";

const categories = [
  { key: "OPD", label: "OPD registrations", color: "var(--hms-accent)" },
  { key: "IPD", label: "IPD registrations", color: "var(--hms-violet)" },
];

it("shows exact cohort counts and proportional shares alongside a ring chart", () => {
  renderWithProviders(
    <CohortBreakdown
      title="Visit mix"
      totals={{ OPD: 20, IPD: 8 }}
      categories={categories}
      presentation="ring"
    />,
  );
  expect(screen.getByRole("img", { name: "Visit mix distribution" })).toBeInTheDocument();
  const table = screen.getByRole("table", { name: "Visit mix breakdown" });
  expect(table).toHaveTextContent("OPD registrations2071.4%");
  expect(table).toHaveTextContent("IPD registrations828.6%");
});

it("does not invent percentages for a zero-record cohort", () => {
  renderWithProviders(
    <CohortBreakdown
      title="Empty mix"
      totals={{ OPD: 0, IPD: 0 }}
      categories={categories}
      presentation="ring"
    />,
  );
  expect(screen.getByText("No records to distribute in this period.")).toBeInTheDocument();
  expect(screen.queryByRole("img")).not.toBeInTheDocument();
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
});

it("bases a filtered bar distribution only on the displayed categories", () => {
  renderWithProviders(
    <CohortBreakdown
      title="Selected visits"
      totals={{ OPD: 20, IPD: 8 }}
      categories={[categories[1]!]}
      presentation="bar"
    />,
  );
  expect(screen.getByRole("img", { name: "Selected visits distribution" })).toBeInTheDocument();
  const table = screen.getByRole("table", { name: "Selected visits breakdown" });
  expect(table).toHaveTextContent("IPD registrations8100.0%");
  expect(table).not.toHaveTextContent("OPD");
});
