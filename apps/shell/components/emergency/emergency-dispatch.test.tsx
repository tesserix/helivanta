import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { EmergencyDispatchPanel } from "./emergency-dispatch";

const base = "019a1234-5678-7000-8000-000000000001";
const caseID = "019a1234-5678-7000-8000-000000000002";
const vehicle = "019a1234-5678-7000-8000-000000000003";
const assessment = "019a1234-5678-7000-8000-000000000004";
const duty = "019a1234-5678-7000-8000-000000000005";
function fixture(permissions: string[], enabled = true) {
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: "11111111-1111-1111-1111-111111111111",
          subject: "TEST-dispatcher",
          data: permissions,
        }),
      );
    if (url.endsWith("dispatch-eligibility"))
      return new Response(
        JSON.stringify({
          case_id: caseID,
          case_version: 1,
          assessment_id: assessment,
          required_class: "als",
          eligible: true,
          dispatch_enabled: enabled,
          blockers: [],
          valid_until: "2030-01-01T00:00:00Z",
          computed_at: "2026-10-18T00:00:00Z",
        }),
      );
    if (url.includes("/fleet"))
      return new Response(
        JSON.stringify({
          data: [
            {
              id: vehicle,
              code: "TEST-ALS-1",
              dispatchable: true,
              effective_capability: "als",
              duty_id: duty,
              blockers: [],
            },
          ],
          page: { has_more: false, next_cursor: null },
        }),
      );
    if (init?.method === "PUT")
      return new Response(
        JSON.stringify({ error: "unavailable", message: "TEST uncertain request" }),
        { status: 503 },
      );
    return new Response(JSON.stringify({ data: [], page: { has_more: false, next_cursor: null } }));
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});
it("makes no dispatch query without an explicit permission", async () => {
  const fetcher = fixture([]);
  renderWithProviders(<EmergencyDispatchPanel baseID={base} caseID={caseID} />);
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([url]) => url.endsWith("permissions"))).toBe(true),
  );
  expect(fetcher.mock.calls.some(([url]) => url.includes("/emergency/"))).toBe(false);
  expect(
    screen.queryByRole("button", { name: "Reserve vehicle and crew" }),
  ).not.toBeInTheDocument();
});
it("does not offer a reservation when dispatch activation is disabled", async () => {
  fixture(["emergency.case.read", "emergency.dispatch.manage"], false);
  renderWithProviders(<EmergencyDispatchPanel baseID={base} caseID={caseID} />);
  expect(await screen.findByText(/dispatch activation is not enabled/i)).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Reserve vehicle and crew" }),
  ).not.toBeInTheDocument();
});
it("retains exactly the same command and body after an uncertain reservation", async () => {
  const user = userEvent.setup();
  const fetcher = fixture(["emergency.case.read", "emergency.dispatch.manage"]);
  renderWithProviders(<EmergencyDispatchPanel baseID={base} caseID={caseID} />);
  await screen.findByRole("option", { name: "TEST-ALS-1" });
  await user.selectOptions(await screen.findByLabelText("Ready vehicle"), vehicle);
  await user.type(screen.getByLabelText("Crew acknowledgement window (seconds)"), "120");
  await user.click(screen.getByRole("button", { name: "Reserve vehicle and crew" }));
  expect(await screen.findByText(/TEST uncertain request/)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Retry unchanged reservation" }));
  await waitFor(() =>
    expect(fetcher.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(2),
  );
  const puts = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
  expect(puts[0]).toEqual(puts[1]);
  const sent = JSON.parse(String(puts[0][1]?.body));
  expect(sent).toEqual({
    case_id: caseID,
    case_version: 1,
    assessment_id: assessment,
    vehicle_id: vehicle,
    duty_id: duty,
    acknowledgement_seconds: 120,
  });
});
