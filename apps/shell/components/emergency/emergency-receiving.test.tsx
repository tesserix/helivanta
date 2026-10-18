import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { EmergencyReceivingWorkspace } from "./emergency-receiving";
const facility = "019a1234-5678-7000-8000-000000000001";
const request = "019a1234-5678-7000-8000-000000000002";
const provider = "11111111-1111-4111-8111-111111111111";
const row = {
  id: request,
  provider_tenant_id: provider,
  base_id: facility,
  dispatch_id: request,
  facility_id: facility,
  version: 1,
  status: "pending",
  required_class: "als",
  sequence: 1,
  response_deadline: "2030-01-01T00:00:00Z",
  superseded: false,
  arrival_recorded: false,
};
const json = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status });
function fixture(permissions = ["emergency.receiving.read", "emergency.receiving.act"]) {
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    const path = new URL(url, "https://test.local").pathname;
    if (path.endsWith("permissions")) return json({ data: permissions, subject: "user-receiver" });
    if (init?.method === "PUT")
      return json({ error: "unavailable", message: "TEST receiving uncertainty" }, 503);
    if (path.endsWith("/facilities"))
      return json({
        data: [
          {
            id: facility,
            name: "Synthetic Campus",
            version: 1,
            enabled: true,
            access_level: "receiver",
            expires_at: "2030-01-01T00:00:00Z",
          },
        ],
        page: { has_more: false, next_cursor: null },
      });
    if (path.endsWith("/links"))
      return json({ data: [], page: { has_more: false, next_cursor: null } });
    if (path.endsWith("/requests"))
      return json({ data: [row], page: { has_more: false, next_cursor: null } });
    return json(row);
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
afterEach(() => {
  vi.unstubAllGlobals();
  localStorage.clear();
});
it("does not discover receiving records without permission", async () => {
  const fetcher = fixture([]);
  renderWithProviders(<EmergencyReceivingWorkspace />);
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([url]) => url.endsWith("permissions"))).toBe(true),
  );
  expect(fetcher.mock.calls.some(([url]) => url.includes("/emergency/"))).toBe(false);
});
it("keeps a receiver acceptance path, lease and reference unchanged across an uncertain retry", async () => {
  const fetcher = fixture();
  const { user } = renderWithProviders(<EmergencyReceivingWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic Campus" }));
  await user.click(await screen.findByRole("button", { name: "Open pre-alert" }));
  await user.type(
    await screen.findByLabelText("Clinical communication reference"),
    "TEST-VOICE-ACCEPTANCE",
  );
  await user.click(screen.getByRole("button", { name: "Confirm hospital acceptance" }));
  await screen.findByText(/TEST receiving uncertainty/);
  expect(screen.getByLabelText("Clinical communication reference")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry unchanged decision" }));
  await waitFor(() =>
    expect(fetcher.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(2),
  );
  const writes = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
  expect(writes[0]).toEqual(writes[1]);
  expect(writes[0][0]).toContain(
    `/facilities/${facility}/requests/${provider}/${request}/commands/`,
  );
  const body = JSON.parse(String(writes[0][1]?.body));
  expect(body.operation).toBe("accept");
  expect(body.version).toBe(1);
  expect(body.evidence_reference).toBe("TEST-VOICE-ACCEPTANCE");
  expect(body.valid_until).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Approve service link" })).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Confirm physical receipt" }),
  ).not.toBeInTheDocument();
});
