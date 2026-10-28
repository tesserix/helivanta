import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { EmergencyFleet } from "./emergency-fleet";

const base = "0199abc0-0000-7000-8000-000000000001";
const vehicle = {
  id: "0199abc0-0000-7000-8000-000000000002",
  base_id: base,
  version: 1,
  code: "SYN-ALS-1",
  vehicle_class: "als",
  certified_until: "2026-10-20T00:00:00Z",
  policy_reference: "SYN-POLICY",
  oxygen_min_bar: 100,
  readiness_minutes: 480,
  retired: false,
  dispatchable: false,
  effective_capability: null,
  blockers: ["defibrillator_failed"],
  open_constraints: [{ id: "0199abc0-0000-7000-8000-000000000003", code: "defibrillator_failed" }],
  duty_id: "0199abc0-0000-7000-8000-000000000004",
  computed_at: "2026-10-18T00:00:00Z",
  created_at: "2026-10-18T00:00:00Z",
  updated_at: "2026-10-18T00:00:00Z",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
const json = (data: unknown, status = 200) =>
  new Response(JSON.stringify(data), { status, headers: { "Content-Type": "application/json" } });
afterEach(() => {
  vi.unstubAllGlobals();
});

it("shows blockers with no ready claim and hides supervisor actions for readers", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) =>
      Promise.resolve(
        json(url.includes("permissions") ? { data: ["emergency.fleet.read"] } : page([vehicle])),
      ),
    ),
  );
  renderWithProviders(<EmergencyFleet base={base} level="reader" />);
  expect(await screen.findByText("Defibrillator check failed")).toBeInTheDocument();
  expect(screen.getByText("Blocked")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Clear constraint" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Save vehicle" })).not.toBeInTheDocument();
});
it("requires reviewed vehicle policy and sends versioned configuration", async () => {
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes.push(String(init.body));
        return Promise.resolve(json({ ...vehicle, ...JSON.parse(String(init.body)), version: 1 }));
      }
      return Promise.resolve(
        json(
          url.includes("permissions")
            ? { data: ["emergency.fleet.read", "emergency.fleet.manage"] }
            : page([]),
        ),
      );
    }),
  );
  const { user } = renderWithProviders(<EmergencyFleet base={base} level="supervisor" />);
  await user.click(await screen.findByText("Configure vehicle"));
  await user.click(screen.getByRole("button", { name: "Save vehicle" }));
  expect(writes).toHaveLength(0);
  await user.type(screen.getByLabelText("Vehicle operational code"), "SYN-ALS-1");
  await user.type(screen.getByLabelText("Vehicle policy reference"), "SYN-POLICY");
  await user.type(screen.getByLabelText("Minimum oxygen pressure (bar)"), "100");
  await user.type(screen.getByLabelText("Readiness validity (minutes)"), "480");
  await user.type(screen.getByLabelText("Vehicle certification valid until"), "2026-10-20T12:00");
  await user.click(screen.getByRole("button", { name: "Save vehicle" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(JSON.parse(writes[0])).toMatchObject({
    version: 0,
    code: "SYN-ALS-1",
    oxygen_min_bar: 100,
    policy_reference: "SYN-POLICY",
  });
});

it("shows the existing certification when reviewing a vehicle", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) =>
      Promise.resolve(
        json(
          url.includes("permissions")
            ? { data: ["emergency.fleet.read", "emergency.fleet.manage"] }
            : page(url.endsWith("/vehicles") ? [vehicle] : []),
        ),
      ),
    ),
  );
  const { user } = renderWithProviders(<EmergencyFleet base={base} level="supervisor" />);
  await user.click(await screen.findByRole("button", { name: "Configure SYN-ALS-1" }));
  expect(screen.getByLabelText("Vehicle certification valid until")).not.toHaveValue("");
});

it("can verify a second person without overwriting the first credential", async () => {
  const paths: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        paths.push(url);
        return Promise.resolve(
          json({ ...JSON.parse(String(init.body)), id: paths.length.toString(), version: 1 }),
        );
      }
      return Promise.resolve(
        json(
          url.includes("permissions")
            ? { data: ["emergency.fleet.read", "emergency.fleet.manage"] }
            : page([]),
        ),
      );
    }),
  );
  const { user } = renderWithProviders(<EmergencyFleet base={base} level="supervisor" />);
  await user.click(await screen.findByText("Verify crew credentials"));
  await user.type(screen.getByLabelText("Crew identity subject"), "user-driver");
  await user.type(screen.getByLabelText("Credential valid until"), "2026-10-20T12:00");
  await user.type(screen.getByLabelText("Credential evidence reference"), "SYN-CRED-1");
  await user.click(screen.getByRole("button", { name: "Save credential" }));
  await waitFor(() => expect(paths).toHaveLength(1));
  await user.click(screen.getByRole("button", { name: "Verify another person" }));
  await user.type(screen.getByLabelText("Crew identity subject"), "user-medic");
  await user.type(screen.getByLabelText("Credential valid until"), "2026-10-20T12:00");
  await user.type(screen.getByLabelText("Credential evidence reference"), "SYN-CRED-2");
  await user.click(screen.getByRole("button", { name: "Save credential" }));
  await waitFor(() => expect(paths).toHaveLength(2));
  expect(paths[1]).not.toEqual(paths[0]);
});
