import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { EmergencyCrewWorkspace } from "./emergency-crew";

const base = "019a1234-5678-7000-8000-000000000001";
const id = "019a1234-5678-7000-8000-000000000002";
const row = {
  id,
  base_id: base,
  state: "offered",
  version: 2,
  own_acknowledged: false,
  driver_acknowledged: false,
  clinician_acknowledged: false,
  acknowledgement_deadline: "2030-01-01T00:00:00Z",
};
const json = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status });
function fixture(
  permissions = ["emergency.crew.read", "emergency.crew.act"],
  acknowledged = false,
) {
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("permissions")) return json({ data: permissions, subject: "TEST-driver" });
    if (url.endsWith("assignments"))
      return json({ data: [row], page: { has_more: false, next_cursor: null } });
    if (url.endsWith("incident"))
      return json({
        incident: { location_text: "Synthetic Gate C", patient_description: "Synthetic report" },
        contact: { callback_phone: "0000000000" },
      });
    if (init?.method === "PUT")
      return json({ error: "unavailable", message: "TEST response uncertain" }, 503);
    return json({ ...row, own_acknowledged: acknowledged });
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});
it("does not discover assignments without crew read permission", async () => {
  const fetcher = fixture([]);
  renderWithProviders(<EmergencyCrewWorkspace />);
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([url]) => url.endsWith("permissions"))).toBe(true),
  );
  expect(fetcher.mock.calls.some(([url]) => url.includes("/emergency/"))).toBe(false);
});
it("opens only the selected assignment and retries the unchanged acknowledgement", async () => {
  const fetcher = fixture();
  const { user } = renderWithProviders(<EmergencyCrewWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Open assignment" }));
  expect(await screen.findByText("Synthetic Gate C")).toBeInTheDocument();
  await user.type(screen.getByLabelText("Operational evidence reference"), "TEST-ACK-1");
  await user.click(screen.getByRole("button", { name: "Acknowledge assignment" }));
  await screen.findByText(/TEST response uncertain/);
  expect(screen.getByLabelText("Operational evidence reference")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry unchanged command" }));
  await waitFor(() =>
    expect(fetcher.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(2),
  );
  const writes = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
  expect(writes[0]).toEqual(writes[1]);
  expect(writes[0][0]).toContain(`/emergency/crew/bases/${base}/dispatches/${id}/commands/`);
  expect(JSON.parse(String(writes[0][1]?.body))).toEqual({
    version: 2,
    operation: "acknowledge",
    evidence_reference: "TEST-ACK-1",
  });
  expect(writes[0][1]?.body).not.toContain("Synthetic Gate C");
});
it("does not offer a second acknowledgement for the same crew member", async () => {
  fixture(undefined, true);
  const { user } = renderWithProviders(<EmergencyCrewWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Open assignment" }));
  expect(await screen.findByText(/Your acknowledgement is recorded/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Acknowledge assignment" })).not.toBeInTheDocument();
});

it("does not queue an action while the browser is offline", async () => {
  const fetcher = fixture();
  const { user } = renderWithProviders(<EmergencyCrewWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Open assignment" }));
  await user.type(await screen.findByLabelText("Operational evidence reference"), "TEST-OFFLINE");
  const original = Object.getOwnPropertyDescriptor(window.navigator, "onLine");
  Object.defineProperty(window.navigator, "onLine", { configurable: true, value: false });
  try {
    await user.click(screen.getByRole("button", { name: "Acknowledge assignment" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("actions are not queued");
    expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
  } finally {
    if (original) Object.defineProperty(window.navigator, "onLine", original);
    else Reflect.deleteProperty(window.navigator, "onLine");
  }
});

it("waits for a current read after a successful command receipt", async () => {
  const fetcher = fixture();
  const read = fetcher.getMockImplementation()!;
  fetcher.mockImplementation(async (url, init) =>
    init?.method === "PUT" ? json({ version: 3, state: "offered" }) : read(url, init),
  );
  const { user } = renderWithProviders(<EmergencyCrewWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Open assignment" }));
  await user.type(await screen.findByLabelText("Operational evidence reference"), "TEST-RECEIPT");
  await user.click(screen.getByRole("button", { name: "Acknowledge assignment" }));
  expect(
    await screen.findByText(/Command recorded. Refresh the current assignment/),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Acknowledge assignment" })).not.toBeInTheDocument();
});

it("hides protected details after assignment access is revoked", async () => {
  const fetcher = fixture();
  const read = fetcher.getMockImplementation()!;
  let revoked = false;
  fetcher.mockImplementation(async (url, init) =>
    revoked && url.includes("/dispatches/")
      ? json({ error: "not_found", message: "Resource not found" }, 404)
      : read(url, init),
  );
  const { user } = renderWithProviders(<EmergencyCrewWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Open assignment" }));
  expect(await screen.findByText("Synthetic Gate C")).toBeInTheDocument();
  revoked = true;
  await user.click(screen.getByRole("button", { name: "Refresh current assignment" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Assignment or incident access is unavailable",
  );
  expect(screen.queryByText("Synthetic Gate C")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Operational evidence reference")).not.toBeInTheDocument();
});

it("retains an uncertain command across a failed refresh and a newer current version", async () => {
  const fetcher = fixture();
  const read = fetcher.getMockImplementation()!;
  let failRead = false;
  let advanced = false;
  fetcher.mockImplementation(async (url, init) => {
    if (init?.method !== "PUT" && url.includes("/dispatches/") && !url.endsWith("incident")) {
      if (failRead) return json({ error: "unavailable", message: "TEST read unavailable" }, 503);
      if (advanced) return json({ ...row, version: 3, own_acknowledged: true });
    }
    return read(url, init);
  });
  const { user } = renderWithProviders(<EmergencyCrewWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Open assignment" }));
  await user.type(await screen.findByLabelText("Operational evidence reference"), "TEST-UNCERTAIN");
  await user.click(screen.getByRole("button", { name: "Acknowledge assignment" }));
  await screen.findByText(/TEST response uncertain/);
  failRead = true;
  await user.click(screen.getByRole("button", { name: "Refresh current assignment" }));
  await screen.findByText(/Assignment or incident access is unavailable/);
  failRead = false;
  advanced = true;
  await user.click(screen.getByRole("button", { name: "Refresh access" }));
  await user.click(await screen.findByRole("button", { name: "Retry unchanged command" }));
  await waitFor(() =>
    expect(fetcher.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(2),
  );
  const writes = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
  expect(writes[0]).toEqual(writes[1]);
});

it("binds hospital transport to the current actionable receiving request", async () => {
  const fetcher = fixture();
  const read = fetcher.getMockImplementation()!;
  const receiving = {
    id: base,
    provider_tenant_id: base,
    base_id: base,
    dispatch_id: id,
    facility_id: base,
    version: 2,
    sequence: 1,
    status: "accepted",
    usable: true,
    handover_ready: false,
    destination_name: "Synthetic Receiving Hospital",
    superseded: false,
  };
  fetcher.mockImplementation(async (url, init) => {
    if (init?.method !== "PUT" && url.includes("/dispatches/")) {
      if (url.endsWith("/incident")) return read(url, init);
      if (url.endsWith("/receiving"))
        return json({ data: [receiving], page: { has_more: false, next_cursor: null } });
      if (url.includes("/receiving/")) return json(receiving);
      return json({ ...row, version: 6, state: "on_scene", own_acknowledged: true });
    }
    return read(url, init);
  });
  const { user } = renderWithProviders(<EmergencyCrewWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Open assignment" }));
  expect(await screen.findByText("Synthetic Receiving Hospital")).toBeInTheDocument();
  await user.type(
    screen.getByLabelText("Operational evidence reference"),
    "TEST-HOSPITAL-TRANSPORT",
  );
  await user.click(screen.getByRole("button", { name: "Start hospital transport" }));
  await screen.findByText(/TEST response uncertain/);
  const writes = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
  expect(writes).toHaveLength(1);
  expect(JSON.parse(String(writes[0][1]?.body))).toEqual({
    version: 6,
    operation: "transporting",
    receiving_request_id: base,
    evidence_reference: "TEST-HOSPITAL-TRANSPORT",
  });
});
