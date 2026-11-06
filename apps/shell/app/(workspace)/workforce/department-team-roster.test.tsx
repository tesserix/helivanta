import { screen } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { DepartmentTeamRoster } from "./department-team-roster";
const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-reviewer" };
const hospital = "22222222-2222-4222-8222-222222222222";
const id = (n: number) => `019a0000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const row = {
  posting_id: id(1),
  revision_id: id(2),
  hospital_id: hospital,
  staff: { id: id(3), employee_code: "SYN-TEAM", display_name: "Synthetic Team Supervisor" },
  version: 2,
  department_id: id(4),
  department_revision_id: id(5),
  starts_at: "2026-01-01T00:00:00Z",
  ends_at: null,
  responsibility: "department_supervisor",
  supervisor_staff_id: id(6),
  supervisor_posting_id: id(7),
  state: "current",
  checked_at: "2026-11-06T00:00:00Z",
  source_until: "2099-01-01T00:00:00Z",
  created_at: "2026-01-01T00:00:00Z",
};
function fixture(failure = false, expired = false) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("posting-review-scopes"))
        return Response.json({
          data: [
            {
              hospital_id: hospital,
              name: "Synthetic Hospital",
              can_submit: false,
              can_review: true,
              departments: [],
            },
          ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.includes("department-choices"))
        return Response.json({ data: [], page: { has_more: false, next_cursor: null } });
      if (url.includes("department-team"))
        return failure
          ? Response.json(
              { error: "posting_source_unavailable", message: "Unavailable" },
              { status: 503 },
            )
          : Response.json({
              data: [
                {
                  ...row,
                  source_until: expired
                    ? new Date(Date.now() - 1000).toISOString()
                    : row.source_until,
                },
                {
                  ...row,
                  posting_id: id(8),
                  created_at: "2025-01-01T00:00:00Z",
                  state: "not_current",
                  source_until: null,
                  staff: { ...row.staff, id: id(9), display_name: "Synthetic Ended Placement" },
                },
              ],
              page: { has_more: true, next_cursor: "synthetic-next" },
            });
      throw new Error(`Unexpected team request ${url}`);
    }),
  );
}
afterEach(() => vi.unstubAllGlobals());
it("shows current observations separately from retained non-current placements and preserves continuation", async () => {
  fixture();
  const { user } = renderWithProviders(
    <DepartmentTeamRoster scope={scope} onContextChanged={vi.fn()} />,
  );
  await user.click(await screen.findByRole("button", { name: "Open team for Synthetic Hospital" }));
  expect(await screen.findByText("Synthetic Team Supervisor")).toBeInTheDocument();
  expect(screen.getByText("Current at last check")).toBeInTheDocument();
  expect(screen.getByText("Not currently usable")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Next team placements" })).toBeEnabled();
  expect(screen.queryByText("No approved placements on this page.")).not.toBeInTheDocument();
});
it("shows an owner failure without turning it into an empty roster", async () => {
  fixture(true);
  const { user } = renderWithProviders(
    <DepartmentTeamRoster scope={scope} onContextChanged={vi.fn()} />,
  );
  await user.click(await screen.findByRole("button", { name: "Open team for Synthetic Hospital" }));
  expect(await screen.findByText("The department team could not be verified.")).toBeInTheDocument();
  expect(screen.queryByText("No approved placements on this page.")).not.toBeInTheDocument();
});

it("labels an expired observation separately from a confirmed non-current placement", async () => {
  fixture(false, true);
  const { user } = renderWithProviders(
    <DepartmentTeamRoster scope={scope} onContextChanged={vi.fn()} />,
  );
  await user.click(await screen.findByRole("button", { name: "Open team for Synthetic Hospital" }));
  expect(await screen.findByText("Observation expired; refresh current team")).toBeInTheDocument();
  expect(screen.getByText("Not currently usable")).toBeInTheDocument();
  expect(screen.queryByText("Current at last check")).not.toBeInTheDocument();
});
