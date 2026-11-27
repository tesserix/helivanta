import { fireEvent, screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { ApiError } from "@helivanta/api";
import { SupplierReturnPrepare } from "./supplier-return-prepare";
const commands = vi.hoisted(() => ({
  prepare: vi.fn(),
  recover: vi.fn(),
  source: vi.fn(),
  bills: vi.fn(),
}));
vi.mock("@helivanta/api", async (original) => ({
  ...(await original<typeof import("@helivanta/api")>()),
  preparePharmacySupplierReturn: commands.prepare,
  recoverPharmacySupplierReturnPreparation: commands.recover,
  readPharmacySupplierReturnPreparation: commands.source,
  listPharmacySupplierReturnOriginalBills: commands.bills,
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
const bill = "0199e000-0000-7000-8000-000000000004",
  receipt = "0199e000-0000-7000-8000-000000000005",
  batch = "0199e000-0000-7000-8000-000000000006",
  bucket = "0199e000-0000-7000-8000-000000000007";
const line = {
  receipt_id: receipt,
  batch_id: batch,
  product_id: batch,
  product_version: 1,
  product_name: "TEST Medicine",
  generic_name: "TEST Generic",
  unit_code: "tablet",
  lot_number: "TEST LOT",
  expires_on: "2028-01-01",
  receipt_units: 80,
  source_reference: "TEST delivery",
  source_created_at: "2026-11-21T00:00:00Z",
  dispatched_units: 48,
  origin: "external",
};
const sources = {
  payable_id: bill,
  outlet_id: scope.outletId,
  payee_reference: "TEST-EXT",
  payee_name: "TEST Supplier",
  document_reference: "TEST-BILL",
  currency: "INR",
  stock_as_of: "2026-11-21T00:00:00Z",
  lines: [line],
};
const render = (allowed = true) =>
  renderWithProviders(
    <NavigationGuardProvider>
      <SupplierReturnPrepare scope={scope} canStart={allowed} />
    </NavigationGuardProvider>,
  );
beforeEach(() => {
  sessionStorage.clear();
  vi.clearAllMocks();
  commands.source.mockResolvedValue(sources);
  commands.bills.mockResolvedValue({
    data: [
      {
        id: bill,
        outlet_id: scope.outletId,
        document_reference: "TEST-BILL",
        payee_name: "TEST Supplier",
        issued_on: "2026-11-01",
        currency: "INR",
      },
    ],
    page: { has_more: false, next_cursor: null },
  });
  commands.prepare.mockResolvedValue({ id: receipt, number: 1 });
  commands.recover.mockResolvedValue({ id: receipt, number: 1 });
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify({
            data: url.includes("/stock/positions")
              ? [
                  {
                    id: bucket,
                    batch_id: batch,
                    product_id: batch,
                    location_id: bucket,
                    location_code: "TEST-Q",
                    location_name: "TEST Quarantine",
                    stock_status: "quarantine",
                    on_hand_units: 80,
                  },
                ]
              : [
                  {
                    id: bill,
                    outlet_id: scope.outletId,
                    document_reference: "TEST-BILL",
                    payee_name: "TEST Supplier",
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
async function choose(user: ReturnType<typeof render>["user"], label: string, text: RegExp) {
  await user.click(await screen.findByRole("combobox", { name: label }));
  await user.click(await screen.findByRole("option", { name: text }));
}
async function entry(user: ReturnType<typeof render>["user"]) {
  await choose(user, "Original supplier bill", /TEST-BILL/);
  await choose(user, "Original receipt", /TEST Medicine/);
  await choose(user, "Quarantine location", /TEST Quarantine/);
  await user.type(screen.getByLabelText("Return units"), "4");
}
it("requires explicit credit and creates a readable bill-backed return without ID entry", async () => {
  const { user } = render();
  await entry(user);
  expect(screen.getByLabelText("Expected credit (INR)")).toHaveValue("");
  await user.click(screen.getByRole("button", { name: "Add return line" }));
  expect(await screen.findByText(/Enter expected credit explicitly/)).toBeVisible();
  await user.type(screen.getByLabelText("Expected credit (INR)"), "15.00");
  await user.click(screen.getByRole("button", { name: "Add return line" }));
  await user.type(screen.getByLabelText("Return reason"), "TEST short-dated review");
  await user.click(screen.getByRole("button", { name: "Prepare supplier return" }));
  await waitFor(() => expect(commands.prepare).toHaveBeenCalledTimes(1));
  expect(commands.prepare.mock.calls[0]?.[2]).toEqual({
    payable_id: bill,
    reason: "TEST short-dated review",
    lines: [
      {
        receipt_id: receipt,
        quarantine_location_id: bucket,
        quantity_units: 4,
        expected_credit_minor: 1500,
      },
    ],
  });
  expect(await screen.findByText(/Return 1 prepared/)).toBeVisible();
  expect(screen.queryByText(bill)).not.toBeInTheDocument();
  expect(sessionStorage.length).toBe(0);
});
it("recovers response loss after reload while new Stock writes are paused", async () => {
  commands.prepare.mockRejectedValue(new Error("TEST response loss"));
  const first = render();
  await entry(first.user);
  await first.user.type(screen.getByLabelText("Expected credit (INR)"), "0");
  await first.user.click(screen.getByRole("button", { name: "Add return line" }));
  await first.user.type(screen.getByLabelText("Return reason"), "TEST reason");
  await first.user.click(screen.getByRole("button", { name: "Prepare supplier return" }));
  expect(await screen.findByRole("button", { name: "Check preparation result" })).toBeVisible();
  expect(Object.values(sessionStorage).join("")).not.toContain("TEST reason");
  first.unmount();
  const second = render(false);
  await second.user.click(await screen.findByRole("button", { name: "Check preparation result" }));
  expect(await screen.findByText(/Return 1 prepared/)).toBeVisible();
  expect(commands.prepare).toHaveBeenCalledTimes(1);
  expect(commands.recover).toHaveBeenCalledTimes(1);
});
it("refuses units beyond the original receipt's observed unreturned quantity", async () => {
  const { user } = render();
  await entry(user);
  await user.clear(screen.getByLabelText("Return units"));
  await user.type(screen.getByLabelText("Return units"), "33");
  await user.type(screen.getByLabelText("Expected credit (INR)"), "0");
  await user.click(screen.getByRole("button", { name: "Add return line" }));
  expect(await screen.findByText(/original receipt has only 32 unreturned units/)).toBeVisible();
  expect(commands.prepare).not.toHaveBeenCalled();
});
it("does not expose a new preparation when Stock is paused", () => {
  render(false);
  expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  expect(commands.source).not.toHaveBeenCalled();
});
it("keeps platform-supplier sources explicitly unsupported", async () => {
  commands.source.mockResolvedValue({ ...sources, lines: [{ ...line, origin: "platform" }] });
  const { user } = render();
  await choose(user, "Original supplier bill", /TEST-BILL/);
  expect(await screen.findByText(/reverse acknowledgement/)).toBeVisible();
  expect(screen.queryByRole("button", { name: "Add return line" })).not.toBeInTheDocument();
});
it("only retries the checked original preparation after a missing recovery", async () => {
  commands.prepare
    .mockRejectedValueOnce(new Error("TEST lost"))
    .mockResolvedValue({ id: receipt, number: 1 });
  commands.recover.mockRejectedValue(new ApiError("not_found", "TEST missing", 404));
  const { user } = render();
  await entry(user);
  await user.type(screen.getByLabelText("Expected credit (INR)"), "0");
  await user.click(screen.getByRole("button", { name: "Add return line" }));
  await user.type(screen.getByLabelText("Return reason"), "TEST reason");
  await user.click(screen.getByRole("button", { name: "Prepare supplier return" }));
  await user.click(await screen.findByRole("button", { name: "Check preparation result" }));
  await user.click(await screen.findByRole("button", { name: "Retry original preparation" }));
  expect(await screen.findByText(/Return 1 prepared/)).toBeVisible();
  expect(commands.prepare.mock.calls[1]?.[1]).toBe(commands.prepare.mock.calls[0]?.[1]);
});

it("prepares multiple original receipts in the same bin without mixing their batch counts", async () => {
  const secondReceipt = "0199e000-0000-7000-8000-000000000008",
    secondBatch = "0199e000-0000-7000-8000-000000000009";
  commands.source.mockResolvedValue({
    ...sources,
    lines: [
      line,
      {
        ...line,
        receipt_id: secondReceipt,
        batch_id: secondBatch,
        product_id: secondBatch,
        product_name: "TEST Other",
        lot_number: "TEST LOT 2",
        dispatched_units: 0,
      },
    ],
  });
  const firstFetch = vi.mocked(fetch).getMockImplementation()!;
  vi.mocked(fetch).mockImplementation(async (input, init) => {
    const path = String(input);
    if (path.includes("/stock/positions") && path.includes(secondBatch))
      return new Response(
        JSON.stringify({
          data: [
            {
              id: bucket,
              batch_id: secondBatch,
              product_id: secondBatch,
              location_id: bucket,
              location_code: "TEST-Q",
              location_name: "TEST Quarantine",
              stock_status: "quarantine",
              on_hand_units: 80,
            },
          ],
          page: { has_more: false, next_cursor: null },
        }),
      );
    return firstFetch(input, init);
  });
  const { user } = render();
  await entry(user);
  await user.clear(screen.getByLabelText("Return units"));
  await user.type(screen.getByLabelText("Return units"), "32");
  await user.type(screen.getByLabelText("Expected credit (INR)"), "0");
  await user.click(screen.getByRole("button", { name: "Add return line" }));
  await choose(user, "Original receipt", /TEST Other/);
  await choose(user, "Quarantine location", /TEST Quarantine/);
  await user.type(screen.getByLabelText("Return units"), "80");
  await user.type(screen.getByLabelText("Expected credit (INR)"), "0");
  await user.click(screen.getByRole("button", { name: "Add return line" }));
  await user.type(screen.getByLabelText("Return reason"), "TEST two batches");
  await user.click(screen.getByRole("button", { name: "Prepare supplier return" }));
  expect(await screen.findByText(/Return 1 prepared/)).toBeVisible();
  expect(commands.prepare.mock.calls[0]?.[2].lines).toHaveLength(2);
  expect(
    commands.prepare.mock.calls[0]?.[2].lines.map(
      (row: { quantity_units: number }) => row.quantity_units,
    ),
  ).toEqual([32, 80]);
});
it("conceals cached original receipt evidence after a recheck fails", async () => {
  const { user } = render();
  await entry(user);
  commands.source.mockRejectedValue(new Error("TEST unavailable"));
  await user.click(screen.getByRole("button", { name: "Recheck receipt and quarantine stock" }));
  expect(await screen.findByText(/Original receipt evidence is unavailable/)).toBeVisible();
  expect(screen.queryByRole("combobox", { name: "Original receipt" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add return line" })).not.toBeInTheDocument();
});
it("refuses duplicate receipt/bucket lines instead of merging expected credits", async () => {
  const { user } = render();
  await entry(user);
  await user.type(screen.getByLabelText("Expected credit (INR)"), "0");
  await user.click(screen.getByRole("button", { name: "Add return line" }));
  await choose(user, "Quarantine location", /TEST Quarantine/);
  await user.type(screen.getByLabelText("Return units"), "4");
  await user.type(screen.getByLabelText("Expected credit (INR)"), "15");
  await user.click(screen.getByRole("button", { name: "Add return line" }));
  expect(await screen.findByText(/already included/)).toBeVisible();
  expect(screen.getAllByRole("button", { name: /Remove return line/ })).toHaveLength(1);
});
it("does not submit when its preparation recovery reference cannot be saved", async () => {
  const { user } = render();
  await entry(user);
  await user.type(screen.getByLabelText("Expected credit (INR)"), "0");
  await user.click(screen.getByRole("button", { name: "Add return line" }));
  await user.type(screen.getByLabelText("Return reason"), "TEST reason");
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {});
  await user.click(screen.getByRole("button", { name: "Prepare supplier return" }));
  expect(await screen.findByText(/Recovery storage is unavailable/)).toBeVisible();
  expect(commands.prepare).not.toHaveBeenCalled();
});
it("refuses an eleven-digit unit count inline without preparing a return", async () => {
  const { user } = render();
  await entry(user);
  fireEvent.change(screen.getByLabelText("Return units"), { target: { value: "10000000000" } });
  await user.type(screen.getByLabelText("Expected credit (INR)"), "0");
  await user.click(screen.getByRole("button", { name: "Add return line" }));
  expect(await screen.findByText(/whole positive return units/)).toBeVisible();
  expect(screen.getByLabelText("Return units")).toHaveValue("10000000000");
  expect(commands.prepare).not.toHaveBeenCalled();
});
it("refuses an overlong return reason inline without preparing a return", async () => {
  const { user } = render();
  await entry(user);
  await user.type(screen.getByLabelText("Expected credit (INR)"), "15.00");
  await user.click(screen.getByRole("button", { name: "Add return line" }));
  fireEvent.change(screen.getByLabelText("Return reason"), { target: { value: "x".repeat(501) } });
  await user.click(screen.getByRole("button", { name: "Prepare supplier return" }));
  expect(await screen.findByText(/provide a return reason/)).toBeVisible();
  expect(screen.getByLabelText("Return reason")).toHaveValue("x".repeat(501));
  expect(commands.prepare).not.toHaveBeenCalled();
});
