import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithNamedAccounts } from "../../test/account-names";
import { chooseOption } from "../../test/select";
const renderWithProviders = (ui: Parameters<typeof renderWithNamedAccounts>[0]) =>
  renderWithNamedAccounts(
    ui,
    {
      "user-driver": "QA Driver",
      "user-medic": "QA Medic",
      "SYN-CREW-SUBJECT": "QA Crew",
      "SYN-DUTY-DRIVER": "Assigned QA Driver",
      "SYN-DUTY-CLINICIAN": "Assigned QA Clinician",
    },
    ["user-driver", "user-medic"],
    true,
  );
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
// #1262: cards are Panel, so no hand-painted card class survives.
// Buttons and design-system inputs are controls: they carry shadow-sm.
const CARDS =
  ":is(.rounded-xl, .rounded-2xl, .shadow-sm):not(button, input, textarea, [role=combobox])";
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
  expect(document.body.querySelectorAll(CARDS)).toHaveLength(0);
});

it("draws readiness and constraint review inside the vehicle panels", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) =>
      Promise.resolve(
        json(
          url.includes("permissions")
            ? { data: ["emergency.fleet.read", "emergency.fleet.manage", "emergency.fleet.check"] }
            : page(url.endsWith("/vehicles") ? [vehicle] : []),
        ),
      ),
    ),
  );
  const { user } = renderWithProviders(<EmergencyFleet base={base} level="supervisor" />);
  await user.click(await screen.findByRole("button", { name: "Record readiness for SYN-ALS-1" }));
  expect(await screen.findByText("Readiness observation · SYN-ALS-1")).toBeInTheDocument();
  await user.click(screen.getByText("Review Defibrillator check failed"));
  expect(screen.getByLabelText("Repair evidence reference")).toBeVisible();
  expect(document.body.querySelectorAll(CARDS)).toHaveLength(0);
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
  await chooseOption(user, await screen.findByLabelText("Crew member"), "QA Driver · Staff member");
  await user.type(screen.getByLabelText("Credential valid until"), "2026-10-20T12:00");
  await user.type(screen.getByLabelText("Credential evidence reference"), "SYN-CRED-1");
  await user.click(screen.getByRole("button", { name: "Save credential" }));
  await waitFor(() => expect(paths).toHaveLength(1));
  await user.click(screen.getByRole("button", { name: "Verify another person" }));
  await chooseOption(user, await screen.findByLabelText("Crew member"), "QA Medic · Staff member");
  await user.type(screen.getByLabelText("Credential valid until"), "2026-10-20T12:00");
  await user.type(screen.getByLabelText("Credential evidence reference"), "SYN-CRED-2");
  await user.click(screen.getByRole("button", { name: "Save credential" }));
  await waitFor(() => expect(paths).toHaveLength(2));
  expect(paths[1]).not.toEqual(paths[0]);
});

it("drops the refused fleet from every form once access is withdrawn", async () => {
  let refused = false;
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) =>
      Promise.resolve(
        url.includes("permissions")
          ? json({ data: ["emergency.fleet.read", "emergency.fleet.manage"] })
          : refused
            ? json({ error: "forbidden", message: "secret detail" }, 403)
            : json(
                page(
                  url.endsWith("/vehicles")
                    ? [vehicle]
                    : url.endsWith("/crew")
                      ? [
                          {
                            id: "c1",
                            subject: "SYN-CREW-SUBJECT",
                            qualification: "emt",
                            suspended: true,
                          },
                        ]
                      : url.endsWith("/duties")
                        ? [
                            {
                              id: "d1",
                              vehicle_id: vehicle.id,
                              driver_subject: "SYN-DUTY-DRIVER",
                              clinician_subject: "SYN-DUTY-CLINICIAN",
                              ends_at: "2026-10-20T00:00:00Z",
                              ended_at: null,
                            },
                          ]
                        : [],
                ),
              ),
      ),
    ),
  );
  const { user } = renderWithProviders(<EmergencyFleet base={base} level="supervisor" />);
  await user.click(await screen.findByRole("button", { name: "Configure SYN-ALS-1" }));
  expect(screen.getByDisplayValue("SYN-ALS-1")).toBeInTheDocument();
  expect(screen.getAllByText(/QA Crew/).length).toBeGreaterThan(0);
  expect(screen.getAllByText(/Assigned QA Driver/).length).toBeGreaterThan(0);
  expect(document.body.querySelectorAll(CARDS)).toHaveLength(0);
  refused = true;
  // A background re-read (window focus), not the Refresh button, brings the refusal.
  window.dispatchEvent(new Event("visibilitychange"));
  expect(
    await screen.findAllByText("This wasn't found, or you don't have access to it."),
  ).not.toHaveLength(0);
  expect(screen.queryByDisplayValue("SYN-ALS-1")).not.toBeInTheDocument();
  expect(screen.queryAllByText(/SYN-ALS-1/)).toHaveLength(0);
  expect(screen.queryAllByText(/QA Crew/)).toHaveLength(0);
  expect(screen.queryAllByText(/Assigned QA Driver/)).toHaveLength(0);
  expect(screen.queryByText(/secret detail/)).not.toBeInTheDocument();
});
