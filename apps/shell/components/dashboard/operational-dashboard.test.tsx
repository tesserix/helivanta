import "../../test/select";
import { fireEvent, screen, within, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../test/select";
import { OperationalDashboard } from "./operational-dashboard";

const permissions = ["medicore.visit.read", "pharmacy.dispense.read", "lab.order.read"];
const emptyPage = { data: [], page: { has_more: false, next_cursor: null } };

function mockApi({
  perms = permissions,
  failLab = false,
  from = "2026-10-01",
  to = "2026-10-18",
  timezone = "UTC",
} = {}) {
  const reads: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      reads.push(url);
      let body: unknown = emptyPage;
      let status = 200;
      if (url.includes("/iam/me/permissions")) body = { data: perms };
      if (url.includes("/insights?")) {
        const clinical = url.includes("/medicore/");
        const counts = clinical
          ? { OPD: 20, IPD: 8 }
          : { pending: 3, [url.includes("/lab/") ? "completed" : "dispensed"]: 7 };
        body = {
          from,
          to,
          interval: "day",
          timezone,
          as_at: "2026-10-18T10:00:00Z",
          totals: counts,
          series: [{ date: "2026-10-01", counts }],
        };
        if (failLab && url.includes("/lab/")) {
          status = 503;
          body = { error: "unavailable", message: "Try later" };
        }
      }
      return new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
  return reads;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

it("identifies missing emergency and birth reporting without inventing counts or endpoints", async () => {
  const reads = mockApi();
  renderWithProviders(<OperationalDashboard />);
  const coverage = await screen.findByRole("region", { name: "Clinical reporting coverage" });
  expect(
    within(coverage).getByRole("heading", { name: "Emergency department" }),
  ).toBeInTheDocument();
  expect(within(coverage).getByRole("heading", { name: "Maternity & births" })).toBeInTheDocument();
  expect(within(coverage).getByText(/Live births/)).toHaveTextContent("stillbirths");
  expect(within(coverage).getAllByText("Not yet reported")).toHaveLength(6);
  expect(within(coverage).queryByText(/^0(?:\.0|%)?$/)).not.toBeInTheDocument();
  expect(within(coverage).queryByRole("img")).not.toBeInTheDocument();
  expect(reads.some((url) => /emergency|birth|maternity|occupancy/.test(url))).toBe(false);
});

it("keeps clinical reporting coverage in the clinical view and out of service and team views", async () => {
  mockApi();
  const { user } = renderWithProviders(<OperationalDashboard />);
  await user.click(await screen.findByRole("tab", { name: "Patient activity" }));
  const coverage = screen.getByRole("region", { name: "Clinical reporting coverage" });
  for (const title of [
    "Admissions & discharges",
    "Beds & occupancy",
    "Procedures & theatre",
    "Deaths",
  ])
    expect(within(coverage).getByRole("heading", { name: title })).toBeInTheDocument();
  await user.click(screen.getByRole("tab", { name: "Diagnostics & pharmacy" }));
  expect(
    screen.queryByRole("region", { name: "Clinical reporting coverage" }),
  ).not.toBeInTheDocument();
  await user.click(screen.getByRole("tab", { name: "Care teams" }));
  expect(
    screen.queryByRole("region", { name: "Clinical reporting coverage" }),
  ).not.toBeInTheDocument();
});

type User = ReturnType<typeof renderWithProviders>["user"];

// The custom range lives in a popover behind the period control (#1255 spec D5).
async function openFilters(user: User) {
  await user.click(await screen.findByRole("button", { name: /Custom range/ }));
  return screen.findByRole("form", { name: "Dashboard filters" });
}

it("focuses the clinical view and keeps the applied period when returning to overview", async () => {
  mockApi();
  const { user } = renderWithProviders(
    <OperationalDashboard now={new Date("2026-10-18T10:00:00Z")} />,
  );
  await user.click(await screen.findByRole("tab", { name: "Patient activity" }));
  expect(screen.getByRole("tab", { name: "Patient activity" })).toHaveAttribute(
    "aria-selected",
    "true",
  );
  expect(screen.getByRole("region", { name: "Patient activity" })).toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Laboratory" })).not.toBeInTheDocument();
  expect(screen.queryByRole("region", { name: "Doctors & care teams" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("tab", { name: "Care teams" }));
  expect(screen.queryByRole("button", { name: /Custom range/ })).not.toBeInTheDocument();
  expect(screen.getByRole("region", { name: "Doctors & care teams" })).toBeInTheDocument();
  await user.click(screen.getByRole("tab", { name: "Overview" }));
  expect(await screen.findByRole("region", { name: "Laboratory" })).toBeInTheDocument();
  expect(within(await openFilters(user)).getByLabelText("From")).toHaveValue("2026-10-01");
});

it("uses the current date when a preset is clicked after midnight", async () => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-10-18T23:59:00Z"));
  mockApi();
  const { user } = renderWithProviders(<OperationalDashboard />);
  await screen.findByRole("button", { name: /Custom range/ });
  vi.setSystemTime(new Date("2026-10-19T00:01:00Z"));
  await user.click(screen.getByRole("button", { name: /^Today$/ }));
  expect(within(await openFilters(user)).getByLabelText("From")).toHaveValue("2026-10-19");
});

it("rejects invalid ranges inline without fetching a misleading result", async () => {
  const reads = mockApi();
  const { user } = renderWithProviders(
    <OperationalDashboard now={new Date("2026-10-18T10:00:00Z")} />,
  );
  const form = await openFilters(user);
  fireEvent.change(within(form).getByLabelText("From"), { target: { value: "2027-01-01" } });
  await user.click(within(form).getByRole("button", { name: "Apply" }));
  expect(await within(form).findByText(/ending on or after From/)).toBeInTheDocument();
  expect(reads.some((url) => url.includes("from=2027"))).toBe(false);
});

it("presents real clinical totals, accessible chart data and honest metric definitions", async () => {
  mockApi();
  const { user } = renderWithProviders(
    <OperationalDashboard now={new Date("2026-10-18T10:00:00Z")} />,
  );
  const clinical = await screen.findByRole("region", { name: "Patient activity" });
  expect(await within(clinical).findByText("28")).toBeInTheDocument();
  expect(within(clinical).getAllByText("IPD registrations").length).toBeGreaterThan(0);
  expect(
    screen.getByText(/Appointment scheduling and doctor assignments are not recorded/),
  ).toBeInTheDocument();
  await user.click(within(clinical).getByText("View data table"));
  expect(within(clinical).getByRole("table", { name: "Patient activity data" })).toHaveTextContent(
    "20",
  );
});

it("applies inclusive dates, timezone and yearly grouping to each permitted panel", async () => {
  const reads = mockApi();
  const { user } = renderWithProviders(
    <OperationalDashboard now={new Date("2026-10-18T10:00:00Z")} />,
  );
  const form = await openFilters(user);
  await user.clear(within(form).getByLabelText("From"));
  await user.type(within(form).getByLabelText("From"), "2024-01-01");
  await chooseOption(user, within(form).getByLabelText("Group by"), "Year");
  await user.clear(within(form).getByLabelText("Timezone"));
  await user.type(within(form).getByLabelText("Timezone"), "Asia/Kolkata");
  await user.click(within(form).getByRole("button", { name: "Apply" }));
  await waitFor(() =>
    expect(
      reads.filter(
        (url) => url.includes("interval=year") && url.includes("timezone=Asia%2FKolkata"),
      ),
    ).toHaveLength(3),
  );
});

it("isolates a failed panel and never substitutes zero for an upstream error", async () => {
  mockApi({ failLab: true });
  renderWithProviders(<OperationalDashboard />);
  const lab = await screen.findByRole("region", { name: "Laboratory" });
  expect(await within(lab).findByRole("alert")).toHaveTextContent(
    "Could not load laboratory insights",
  );
  expect(within(lab).getByRole("button", { name: "Retry Laboratory" })).toBeInTheDocument();
  expect(within(lab).queryByText("0")).not.toBeInTheDocument();
  expect(await screen.findByRole("img", { name: "Patient activity trend" })).toBeInTheDocument();
});

it("does not resolve clinical or lab data for a pharmacy-only user", async () => {
  const reads = mockApi({ perms: ["pharmacy.dispense.read"] });
  renderWithProviders(<OperationalDashboard />);
  await screen.findByRole("region", { name: "Pharmacy" });
  expect(screen.queryByRole("region", { name: "Patient activity" })).not.toBeInTheDocument();
  expect(
    screen.queryByRole("region", { name: "Clinical reporting coverage" }),
  ).not.toBeInTheDocument();
  expect(reads.some((url) => /\/(medicore|lab)\/insights/.test(url))).toBe(false);
});

it("shows visit mix and an inclusive daily average independent of chart grouping", async () => {
  mockApi();
  const { user } = renderWithProviders(
    <OperationalDashboard now={new Date("2026-10-18T10:00:00Z")} />,
  );
  const clinical = await screen.findByRole("region", { name: "Patient activity" });
  expect(await within(clinical).findByText("1.6")).toBeInTheDocument();
  expect(within(clinical).getByText("Registrations / day")).toBeInTheDocument();
  expect(within(clinical).getByRole("table", { name: "Visit mix breakdown" })).toHaveTextContent(
    "28.6%",
  );
  await chooseOption(user, within(clinical).getByLabelText("Visit type"), "Inpatients (IPD)");
  expect(within(clinical).getByText("0.4")).toBeInTheDocument();
  expect(within(clinical).getByRole("table", { name: "Visit mix breakdown" })).toHaveTextContent(
    "100.0%",
  );
  expect(
    within(clinical).getByRole("table", { name: "Visit mix breakdown" }),
  ).not.toHaveTextContent("OPD");
  const lab = screen.getByRole("region", { name: "Laboratory" });
  expect(
    within(lab).getByRole("table", { name: "Laboratory cohort status breakdown" }),
  ).toHaveTextContent("70.0%");
});

it("counts calendar days equally across a DST transition", async () => {
  mockApi({ from: "2026-10-03", to: "2026-10-05", timezone: "Australia/Melbourne" });
  renderWithProviders(<OperationalDashboard />);
  const clinical = await screen.findByRole("region", { name: "Patient activity" });
  expect(await within(clinical).findByText("9.3")).toBeInTheDocument();
});

// #1255 spec D3/D5: a plain title, no slogan, eyebrow or tenant pill.
it("titles the page plainly", async () => {
  mockApi();
  renderWithProviders(<OperationalDashboard now={new Date("2026-10-18T10:00:00Z")} />);
  expect(
    await screen.findByRole("heading", { level: 2, name: "Hospital overview" }),
  ).toBeInTheDocument();
  expect(screen.queryByText(/Your care|Hospital intelligence|authorised data/)).toBeNull();
});

it("keeps quick ranges visible and the custom range in a popover", async () => {
  const reads = mockApi();
  const { user } = renderWithProviders(
    <OperationalDashboard now={new Date("2026-10-18T10:00:00Z")} />,
  );
  expect(await screen.findByRole("button", { name: "This month" })).toBeVisible();
  expect(screen.queryByLabelText("From")).toBeNull();
  const form = await openFilters(user);
  for (const label of ["From", "To", "Group by", "Timezone"]) {
    expect(within(form).getByLabelText(label)).toBeInTheDocument();
  }
  fireEvent.change(within(form).getByLabelText("From"), { target: { value: "2026-09-01" } });
  await user.click(within(form).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(screen.queryByRole("form", { name: "Dashboard filters" })).toBeNull());
  await waitFor(() => expect(reads.some((url) => url.includes("from=2026-09-01"))).toBe(true));
  expect(screen.getByRole("button", { name: /Custom range/ })).toHaveTextContent("2026-09-01");
});

it("shows each caveat on demand, word for word", async () => {
  mockApi();
  const { user } = renderWithProviders(
    <OperationalDashboard now={new Date("2026-10-18T10:00:00Z")} />,
  );
  const caveat =
    "Current status of orders created in this period. Completed does not mean resulted during this period.";
  await screen.findByRole("region", { name: "Laboratory" });
  expect(screen.queryByText(caveat)).toBeNull();
  await user.click(screen.getByRole("button", { name: "About Laboratory" }));
  expect(screen.getByText(caveat)).toBeVisible();
});

// #1255 final review: floating panels are announced and keep the user's place.
it("announces each caveat as a dialog its info button controls", async () => {
  mockApi();
  const { user } = renderWithProviders(
    <OperationalDashboard now={new Date("2026-10-18T10:00:00Z")} />,
  );
  await screen.findByRole("region", { name: "Laboratory" });
  const info = screen.getByRole("button", { name: "About Laboratory" });
  await user.click(info);
  const dialog = screen.getByRole("dialog", { name: "About Laboratory" });
  expect(info).toHaveAttribute("aria-controls", dialog.id);
});

it("returns focus to the range control after Apply", async () => {
  mockApi();
  const { user } = renderWithProviders(
    <OperationalDashboard now={new Date("2026-10-18T10:00:00Z")} />,
  );
  const form = await openFilters(user);
  await user.click(within(form).getByRole("button", { name: "Apply" }));
  await waitFor(() => expect(screen.queryByRole("form", { name: "Dashboard filters" })).toBeNull());
  expect(screen.getByRole("button", { name: /Custom range/ })).toHaveFocus();
});

// Five range buttons do not fit a 390px phone; below sm they are one menu.
it("offers the quick ranges as one menu on a phone", async () => {
  const reads = mockApi();
  const { user } = renderWithProviders(
    <OperationalDashboard now={new Date("2026-10-18T10:00:00Z")} />,
  );
  const menu = await screen.findByRole("combobox", { name: "Quick range" });
  expect(menu.closest(".sm\\:hidden")).not.toBeNull();
  await chooseOption(user, menu, "Today");
  await waitFor(() => expect(reads.some((url) => url.includes("from=2026-10-18"))).toBe(true));
});
