import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithNamedAccounts } from "../../test/account-names";
const renderWithProviders = (ui: Parameters<typeof renderWithNamedAccounts>[0]) =>
  renderWithNamedAccounts(
    ui,
    {
      "user-receiver": "QA Receiver",
      "synthetic-maker": "QA Maker",
      "synthetic-checker": "QA Checker",
      "user-member": "QA Member",
    },
    undefined,
    true,
  );
import { chooseOption } from "../../test/select";
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
// #1262: cards are Panel, so no hand-painted card class survives.
// Buttons and design-system inputs are controls: they carry shadow-sm.
const CARDS =
  ":is(.rounded-xl, .rounded-2xl, .shadow-sm):not(button, input, textarea, [role=combobox])";
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
  const reference = await screen.findByLabelText("Clinical communication reference");
  expect(document.body.querySelectorAll(CARDS)).toHaveLength(0);
  // The zod schema (reference max 120), not native maxLength, refuses an overlong reference (#1262).
  expect(reference).not.toHaveAttribute("maxlength");
  await user.click(reference);
  await user.paste("A".repeat(121));
  await user.click(screen.getByRole("button", { name: "Confirm hospital acceptance" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Enter a communication reference without personal details and an acceptance window of 1–120 minutes.",
  );
  expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
  await user.clear(reference);
  await user.type(reference, "TEST-VOICE-ACCEPTANCE");
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
// #1255 spec D6: the authority level is the design-system Select.
it("records the authority level chosen from the campus authority menu", async () => {
  const fetcher = fixture(["emergency.receiving.read", "emergency.receiving.manage"]);
  const { user } = renderWithProviders(<EmergencyReceivingWorkspace />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic Campus" }));
  await user.click(await screen.findByText("Delegate receiving authority"));
  await chooseOption(
    user,
    await screen.findByLabelText("Staff account"),
    "QA Member · Staff member",
  );
  await user.click(screen.getByRole("button", { name: "Load current delegation" }));
  await chooseOption(
    user,
    await screen.findByRole("combobox", { name: "Campus authority" }),
    "Receiving manager",
  );
  await user.type(screen.getByLabelText("Verification reference"), "TEST-VERIFY-1");
  await user.click(screen.getByRole("button", { name: "Record delegation revision" }));
  await waitFor(() =>
    expect(fetcher.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(1),
  );
  const [, init] = fetcher.mock.calls.find(([, call]) => call?.method === "PUT")!;
  expect(JSON.parse(String(init!.body))).toMatchObject({ level: "manager" });
  expect(document.body.querySelectorAll(CARDS)).toHaveLength(0);
});

it("refuses an overlong campus name or policy reference through zod, not native maxLength", async () => {
  const fetcher = fixture(["emergency.receiving.read", "emergency.receiving.manage"]);
  const { user } = renderWithProviders(<EmergencyReceivingWorkspace />);
  await user.click(await screen.findByText("Add receiving campus"));
  const name = screen.getByLabelText("Campus name");
  const policy = screen.getByLabelText("Approved receiving policy reference");
  expect(name).not.toHaveAttribute("maxlength");
  expect(policy).not.toHaveAttribute("maxlength");
  const save = screen.getByRole("button", { name: "Save campus revision" });
  await user.click(name);
  await user.paste("A".repeat(151));
  await user.type(policy, "TEST-POLICY-1");
  await user.click(save);
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Enter a campus name, approved policy reference and expiry.",
  );
  await user.clear(name);
  await user.type(name, "Synthetic Campus Two");
  await user.clear(policy);
  await user.click(policy);
  await user.paste("A".repeat(121));
  await user.click(save);
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Enter a campus name, approved policy reference and expiry.",
  );
  expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
  expect(document.body.querySelectorAll(CARDS)).toHaveLength(0);
});

it("offers to check campus access again when it is refused, and lists campuses once granted", async () => {
  const fetcher = fixture();
  const read = fetcher.getMockImplementation()!;
  let granted = false;
  fetcher.mockImplementation(async (url, init) =>
    !granted && new URL(url, "https://test.local").pathname.endsWith("/facilities")
      ? json({ error: "forbidden", message: "secret detail" }, 403)
      : read(url, init),
  );
  const { user } = renderWithProviders(<EmergencyReceivingWorkspace />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Campus access is unavailable.");
  expect(screen.queryByText(/secret detail/)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /refresh campuses|try again/i })).toBeNull();
  granted = true;
  await user.click(screen.getByRole("button", { name: "Check access again" }));
  expect(await screen.findByRole("button", { name: "Open Synthetic Campus" })).toBeEnabled();
  await user.click(screen.getByRole("button", { name: "Open Synthetic Campus" }));
  expect(await screen.findByText(/Share these identifiers/)).toBeInTheDocument();
  expect(document.body.querySelectorAll(CARDS)).toHaveLength(0);
});
