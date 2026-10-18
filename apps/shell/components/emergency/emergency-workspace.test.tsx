import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { EmergencyWorkspace } from "./emergency-workspace";
import { EmergencyIntake } from "./emergency-intake";
import { EmergencyCaseDetailPanel } from "./emergency-case-detail";
import { EmergencyBaseAdmin } from "./emergency-base-admin";

const base = {
  id: "0199abc0-0000-7000-8000-000000000001",
  name: "Synthetic Odisha Base",
  timezone: "Asia/Kolkata",
  policy_reference: "SYN-01",
  contact_retention_days: 30,
  access_level: "supervisor" as const,
  created_at: "2026-10-18T00:00:00Z",
  updated_at: "2026-10-18T00:00:00Z",
};
const record = {
  id: "0199abc0-0000-7000-8000-000000000002",
  base_id: base.id,
  case_number: "EC-SYNTHETIC",
  channel: "phone",
  requested_service: "unknown",
  processing_basis: "medical_emergency",
  policy_reference: "SYN-01",
  status: "received",
  priority: "untriaged",
  version: 1,
  created_at: "2026-10-18T00:00:00Z",
  updated_at: "2026-10-18T00:00:00Z",
  contact_retain_until: "2026-11-18T00:00:00Z",
  contact_purged_at: null,
  closure_reason: null,
  closed_at: null,
};
const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});

it("opens only an explicitly granted base and shows metadata without caller details", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) =>
      Promise.resolve(
        json(
          url.includes("permissions")
            ? { data: ["emergency.base.read", "emergency.case.read"] }
            : url.includes("/cases")
              ? page([record])
              : page([base]),
        ),
      ),
    ),
  );
  const { user } = renderWithProviders(<EmergencyWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic Odisha Base" }));
  expect(await screen.findByRole("link", { name: "EC-SYNTHETIC" })).toHaveAttribute(
    "href",
    `/emergency/bases/${base.id}/cases/${record.id}`,
  );
  expect(screen.getByText(/No ambulance has been assigned/)).toBeInTheDocument();
  expect(screen.queryByLabelText("Caller name (optional)")).not.toBeInTheDocument();
});

it("does not expose case actions to a tenant administrator with no base grant", async () => {
  const fetcher = vi.fn((url: string) =>
    Promise.resolve(
      json(
        url.includes("permissions")
          ? {
              data: [
                "emergency.base.read",
                "emergency.base.manage",
                "emergency.case.read",
                "emergency.case.intake",
              ],
            }
          : page([{ ...base, access_level: null }]),
      ),
    ),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<EmergencyWorkspace />);
  expect(await screen.findByText(/No case access/)).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Open Synthetic Odisha Base" }),
  ).not.toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.includes("/cases"))).toBe(false);
});

it("retries a failed intake with the same case ID and preserves the report", async () => {
  const paths: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) => {
      paths.push(url);
      return Promise.resolve(
        paths.length === 1
          ? json({ error: "encryption_unavailable", message: "Protected data unavailable" }, 503)
          : json(record, 202),
      );
    }),
  );
  const { user } = renderWithProviders(<EmergencyIntake base={base} />);
  await user.type(screen.getByLabelText("Incident location (optional)"), "Synthetic Gate C");
  await user.click(screen.getByRole("button", { name: "Receive report" }));
  expect(await screen.findByText("Protected data unavailable")).toBeInTheDocument();
  expect(screen.getByLabelText("Incident location (optional)")).toHaveValue("Synthetic Gate C");
  await user.click(screen.getByRole("button", { name: "Receive report" }));
  expect(await screen.findByRole("link", { name: /Open received case/ })).toBeInTheDocument();
  expect(paths).toHaveLength(2);
  expect(paths[0]).toEqual(paths[1]);
});

it("fails closed on an unauthorised detail read and hides cached clinical data", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) =>
      Promise.resolve(
        url.includes("permissions")
          ? json({ data: ["emergency.case.read"] })
          : json({ error: "not_found", message: "Resource not found" }, 404),
      ),
    ),
  );
  renderWithProviders(<EmergencyCaseDetailPanel baseID={base.id} caseID={record.id} />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Resource not found");
  expect(screen.queryByText("EC-SYNTHETIC")).not.toBeInTheDocument();
});

it("allows correcting a definitively rejected report without replaying rejected content", async () => {
  const bodies: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn((_url: string, init?: RequestInit) => {
      bodies.push(String(init?.body));
      return Promise.resolve(
        bodies.length === 1
          ? json({ error: "invalid_request", message: "Request rejected" }, 400)
          : json(record, 202),
      );
    }),
  );
  const { user } = renderWithProviders(<EmergencyIntake base={base} />);
  await user.type(
    screen.getByLabelText("Incident location (optional)"),
    "Synthetic rejected location",
  );
  await user.click(screen.getByRole("button", { name: "Receive report" }));
  await screen.findByText("Request rejected");
  expect(screen.getByLabelText("Incident location (optional)")).toBeEnabled();
  await user.clear(screen.getByLabelText("Incident location (optional)"));
  await user.type(
    screen.getByLabelText("Incident location (optional)"),
    "Synthetic corrected location",
  );
  await user.click(screen.getByRole("button", { name: "Receive report" }));
  await screen.findByRole("link", { name: /Open received case/ });
  expect(JSON.parse(bodies[1]).incident.location_text).toBe("Synthetic corrected location");
});

it("requires confirmation before revoking a grant and accepts an empty 204 response", async () => {
  const fetcher = vi.fn((_url: string, init?: RequestInit) =>
    Promise.resolve(init?.method === "DELETE" ? new Response(null, { status: 204 }) : json({})),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(<EmergencyBaseAdmin base={base} />);
  await user.type(screen.getByLabelText("Verified identity subject"), "synthetic-dispatcher");
  await user.click(screen.getByRole("button", { name: "Revoke access" }));
  expect(fetcher).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Confirm revoke" }));
  await waitFor(() =>
    expect(fetcher).toHaveBeenCalledWith(
      expect.stringContaining("/access/synthetic-dispatcher"),
      expect.objectContaining({ method: "DELETE" }),
    ),
  );
  expect(await screen.findByText("Access revoked")).toBeInTheDocument();
});
