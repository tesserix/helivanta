import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { LicenceRenewalNotice } from "./licence-renewal-notice";
import type { SupplyLicense } from "@helivanta/api";

const base = {
  date_status: "current",
  days_remaining: 181,
  local_date: "2026-10-18",
  time_zone: "Asia/Karachi",
  observed_at: "2026-10-18T18:45:00Z",
  premise_status: "active",
} as const;

it.each<[NonNullable<SupplyLicense["renewal"]>["date_status"], number, string]>([
  ["current", 181, "Expiry in 181 days"],
  ["renewal_due_180", 180, "Upcoming renewal · 180 days remaining"],
  ["renewal_due_90", 90, "Plan renewal · 90 days remaining"],
  ["renewal_due_30", 1, "Renew within 1 day"],
  ["not_yet_valid", 1, "Not yet valid · check the issue date"],
])("renders %s without using the browser timezone", (date_status, days_remaining, label) => {
  render(<LicenceRenewalNotice renewal={{ ...base, date_status, days_remaining }} />);
  expect(screen.getByText(label)).toBeInTheDocument();
  expect(screen.getByText("As of 2026-10-18 · Asia/Karachi")).toBeInTheDocument();
  expect(
    screen.getByText(/Licence verification and trading approval are separate/),
  ).toBeInTheDocument();
});

it("does not infer a current state from absent or malformed server metadata", () => {
  const { rerender } = render(<LicenceRenewalNotice renewal={undefined} />);
  expect(screen.getByText(/Renewal date notice unavailable/)).toBeInTheDocument();
  rerender(<LicenceRenewalNotice renewal={{ ...base, days_remaining: NaN }} />);
  expect(screen.getByText(/Renewal date notice unavailable/)).toBeInTheDocument();
  expect(screen.queryByText(/Expiry in/)).not.toBeInTheDocument();
});
