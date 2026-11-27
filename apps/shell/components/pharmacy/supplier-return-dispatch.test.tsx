import { fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { ApiError } from "@helivanta/api";
import { SupplierReturnDispatch } from "./supplier-return-dispatch";

const commands = vi.hoisted(() => ({ dispatch: vi.fn(), recover: vi.fn() }));
vi.mock("@helivanta/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@helivanta/api")>()),
  dispatchPharmacySupplierReturn: commands.dispatch,
  recoverPharmacySupplierReturnDispatch: commands.recover,
}));
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/pharmacy/accounts",
  useSearchParams: () => new URLSearchParams(),
}));
const scope = {
  tenantId: "0199e000-0000-7000-8000-000000000001",
  pharmacyId: "0199e000-0000-7000-8000-000000000002",
  outletId: "0199e000-0000-7000-8000-000000000003",
  subject: "TEST-owner",
};
const value = {
  id: "0199e000-0000-7000-8000-000000000004",
  number: 17,
  state: "draft" as const,
  version: 0,
};
const licence = "0199e000-0000-7000-8000-000000000005";
const render = (allowed = true) =>
  renderWithProviders(
    <NavigationGuardProvider>
      <SupplierReturnDispatch
        scope={scope}
        value={value}
        canStart={allowed}
        timeZone="Asia/Kolkata"
      />
    </NavigationGuardProvider>,
  );
beforeEach(() => {
  sessionStorage.clear();
  vi.clearAllMocks();
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            data: [
              {
                id: licence,
                outlet_id: scope.outletId,
                licence_number: "TEST-LIC-42",
                form_label: "TEST 20B",
                decision: "approved",
                reviewed_evidence_file_id: licence,
                valid_from: "2020-01-01",
                valid_until: "2035-01-01",
              },
            ],
            page: { has_more: false, next_cursor: null },
          }),
        ),
    ),
  );
});
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
async function fill(user: ReturnType<typeof renderWithProviders>["user"]) {
  await user.click(await screen.findByRole("combobox", { name: "Reviewed outlet licence" }));
  await user.click(await screen.findByRole("option", { name: /TEST-LIC-42/ }));
  await user.type(screen.getByLabelText("Outward document reference"), "TEST-OUT-1");
  await user.type(screen.getByLabelText("Recipient name"), "TEST Driver");
  fireEvent.change(screen.getByLabelText(/Handoff time/), {
    target: { value: "2026-11-20T10:30" },
  });
  await user.type(screen.getByLabelText("Dispatch reason"), "TEST actual handoff");
  await user.click(screen.getByLabelText(/physically handed/));
}
it("records explicit physical evidence using readable licence choices", async () => {
  commands.dispatch.mockResolvedValue({ document_reference: "TEST-OUT-1" });
  const { user } = render();
  await fill(user);
  await user.click(screen.getByRole("button", { name: "Record physical dispatch" }));
  await waitFor(() => expect(commands.dispatch).toHaveBeenCalledTimes(1));
  expect(commands.dispatch.mock.calls[0]).toEqual([
    scope,
    value.id,
    expect.any(String),
    expect.objectContaining({
      licence_id: licence,
      recipient_name: "TEST Driver",
      physical_disposition: "handed_to_supplier",
      expected_version: 0,
    }),
  ]);
  expect(await screen.findByText(/Dispatch verified/)).toBeVisible();
  expect(screen.queryByText(licence)).not.toBeInTheDocument();
  expect(sessionStorage.length).toBe(0);
});
it("recovers response loss after a reload with Stock paused and never submits twice", async () => {
  commands.dispatch.mockRejectedValue(new Error("TEST response lost"));
  commands.recover.mockResolvedValue({ document_reference: "TEST-OUT-1" });
  const first = render();
  await fill(first.user);
  await first.user.click(screen.getByRole("button", { name: "Record physical dispatch" }));
  expect(await screen.findByRole("button", { name: "Check dispatch result" })).toBeVisible();
  expect(commands.dispatch).toHaveBeenCalledTimes(1);
  const stored = Object.values(sessionStorage).join("");
  expect(stored).not.toContain("TEST Driver");
  expect(stored).not.toContain("TEST-OUT-1");
  first.unmount();
  const second = render(false);
  await second.user.click(await screen.findByRole("button", { name: "Check dispatch result" }));
  expect(await screen.findByText(/Dispatch verified/)).toBeVisible();
  expect(commands.dispatch).toHaveBeenCalledTimes(1);
  expect(commands.recover).toHaveBeenCalledTimes(1);
});
it("refuses physical dispatch when browser recovery storage cannot be written", async () => {
  const { user } = render();
  await fill(user);
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("TEST quota");
  });
  await user.click(screen.getByRole("button", { name: "Record physical dispatch" }));
  expect(await screen.findByText(/Recovery storage is unavailable/)).toBeVisible();
  expect(commands.dispatch).not.toHaveBeenCalled();
});
it("keeps a paused or unapproved workflow hidden", () => {
  render(false);
  expect(
    screen.queryByRole("button", { name: "Record physical dispatch" }),
  ).not.toBeInTheDocument();
  expect(commands.dispatch).not.toHaveBeenCalled();
});

it("retries only the checked original command reference after a missing result", async () => {
  commands.dispatch
    .mockRejectedValueOnce(new Error("TEST response loss"))
    .mockResolvedValue({ document_reference: "TEST-OUT-1" });
  commands.recover.mockRejectedValue(new ApiError("not_found", "TEST missing", 404));
  const { user } = render();
  await fill(user);
  await user.click(screen.getByRole("button", { name: "Record physical dispatch" }));
  await user.click(await screen.findByRole("button", { name: "Check dispatch result" }));
  await user.click(await screen.findByRole("button", { name: "Retry original physical dispatch" }));
  expect(await screen.findByText(/Dispatch verified/)).toBeVisible();
  expect(commands.dispatch).toHaveBeenCalledTimes(2);
  expect(commands.dispatch.mock.calls[1]?.[2]).toBe(commands.dispatch.mock.calls[0]?.[2]);
});
it("refuses silently discarded recovery storage writes", async () => {
  const { user } = render();
  await fill(user);
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {});
  await user.click(screen.getByRole("button", { name: "Record physical dispatch" }));
  expect(await screen.findByText(/Recovery storage is unavailable/)).toBeVisible();
  expect(commands.dispatch).not.toHaveBeenCalled();
});
it("refuses dispatch while the original cancellation remains unresolved", async () => {
  sessionStorage.setItem(
    "helivanta:supplier-return-cancellation:v1:" +
      JSON.stringify([scope.tenantId, scope.subject, scope.pharmacyId, scope.outletId, value.id]),
    licence,
  );
  const { user } = render();
  await fill(user);
  await user.click(screen.getByRole("button", { name: "Record physical dispatch" }));
  expect(await screen.findByText(/Verify the pending cancellation/)).toBeVisible();
  expect(commands.dispatch).not.toHaveBeenCalled();
});
it("conceals licence choices returned for another outlet", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            data: [{ id: licence, outlet_id: licence, licence_number: "TEST FOREIGN LICENCE" }],
            page: { has_more: false, next_cursor: null },
          }),
        ),
    ),
  );
  render();
  expect(await screen.findByText(/could not be verified for this outlet/)).toBeVisible();
  expect(screen.queryByText("TEST FOREIGN LICENCE")).not.toBeInTheDocument();
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
});
it.each([
  ["Outward document reference", 101, "Shorten the outward reference to at most 100 characters."],
  ["Recipient name", 201, "Shorten the recipient to at most 200 characters."],
  ["Dispatch reason", 501, "Shorten the reason to at most 500 characters."],
])("refuses an overlong %s inline without sending a dispatch", async (label, length, message) => {
  const { user } = render();
  await fill(user);
  fireEvent.change(screen.getByLabelText(label), { target: { value: "x".repeat(length) } });
  await user.click(screen.getByRole("button", { name: "Record physical dispatch" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(message);
  expect(screen.getByLabelText(label)).toHaveValue("x".repeat(length));
  expect(commands.dispatch).not.toHaveBeenCalled();
});
