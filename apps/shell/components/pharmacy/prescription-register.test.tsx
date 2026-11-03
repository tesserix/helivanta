import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import type { PharmacyPrescriptionRegisterEntry } from "@helivanta/api";
import { chooseOption } from "../../test/select";
import { PrescriptionRegister } from "./prescription-register";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const patient = "0199e000-0000-7000-8000-0000000000a1";
const entry = (
  over: Partial<PharmacyPrescriptionRegisterEntry>,
): PharmacyPrescriptionRegisterEntry => ({
  id: "0199e000-0000-7000-8000-000000000070",
  serial: "TS/2627/1 · line 1",
  supply_date: "2026-11-01",
  invoice_number: "TS/2627/1",
  sale_id: "0199e000-0000-7000-8000-000000000030",
  line_number: 1,
  regulatory_class: "H",
  prescription_id: "0199e000-0000-7000-8000-000000000040",
  prescribed_on: "2026-10-30",
  prescriber_name: "TEST Dr Asha Rao",
  prescriber_registration: "MMC/2011/04567",
  prescriber_address: "TEST 12 Clinic Lane",
  patient_name: "TEST Ravi Kumar",
  patient_address: "TEST 4 Station Road",
  patient_id: patient,
  product_id: "0199e000-0000-7000-8000-000000000003",
  sku: "TEST-H-DRUG",
  brand_name: "TEST H Drug",
  generic_name: "TEST generic",
  manufacturer: "TEST Pharma Ltd",
  lot_number: "TEST-LOT",
  expires_on: "2028-06-30",
  quantity_units: 10,
  returned_units: 3,
  pharmacist: "user-pharmacist",
  ...over,
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function serve(rows: PharmacyPrescriptionRegisterEntry[]) {
  const fetcher = vi.fn(async (url: string) => {
    if (url.includes("/catalogue/products"))
      return new Response(
        JSON.stringify({
          data: [{ id: rows[0]?.product_id ?? "x", sku: "TEST-H-DRUG", brand_name: "TEST H Drug" }],
          page: { has_more: false, next_cursor: null },
        }),
      );
    return new Response(
      JSON.stringify({ data: rows, page: { has_more: false, next_cursor: null } }),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

const registerReads = (fetcher: ReturnType<typeof serve>) =>
  fetcher.mock.calls
    .map(([url]) => String(url))
    .filter((u) => u.includes("/registers/prescriptions?"));

describe("PrescriptionRegister", () => {
  it("shows each supply with its serial, manufacturer, batch and returns", async () => {
    serve([
      entry({}),
      entry({
        id: "0199e000-0000-7000-8000-000000000071",
        regulatory_class: "G",
        prescriber_address: null,
        patient_address: null,
        patient_id: null,
      }),
    ]);
    renderWithProviders(<PrescriptionRegister scope={scope} today="2026-11-15" />);
    const table = await screen.findByRole("table", { name: "Prescription register" });
    const [, first, second] = within(table).getAllByRole("row");
    expect(first).toHaveTextContent("TS/2627/1 · line 1");
    expect(first).toHaveTextContent("TEST Dr Asha Rao (MMC/2011/04567), TEST 12 Clinic Lane");
    expect(first).toHaveTextContent("TEST Pharma Ltd, TEST-LOT, exp. 2028-06-30");
    expect(second).toHaveTextContent("TEST Dr Asha Rao (MMC/2011/04567), address not recorded");
    expect(second).toHaveTextContent("TEST Ravi Kumar, address not recorded");
  });

  it("filters by schedule and product, and narrows to one registered patient", async () => {
    const fetcher = serve([entry({})]);
    const { user } = renderWithProviders(<PrescriptionRegister scope={scope} today="2026-11-15" />);
    await screen.findByRole("table", { name: "Prescription register" });
    expect(registerReads(fetcher).at(-1)).toMatch(/\?from=2026-11-01&to=2026-11-15$/);

    await chooseOption(user, screen.getByRole("combobox", { name: "Schedule" }), "Schedule H");
    await chooseOption(user, screen.getByRole("combobox", { name: "Product" }), /TEST-H-DRUG/);
    await user.click(screen.getByRole("button", { name: "Show register" }));
    await waitFor(() =>
      expect(registerReads(fetcher).at(-1)).toMatch(
        /from=2026-11-01&to=2026-11-15&class=H&product_id=0199e000-0000-7000-8000-000000000003$/,
      ),
    );

    await user.click(await screen.findByRole("button", { name: "This patient's supplies" }));
    await waitFor(() => expect(registerReads(fetcher).at(-1)).toContain(`&patient_id=${patient}`));
    await user.click(await screen.findByRole("button", { name: "Show every patient" }));
    await waitFor(() => expect(registerReads(fetcher).at(-1)).not.toContain("patient_id"));
  });

  it("says so when nothing matches, and refuses a period over 366 days", async () => {
    const fetcher = serve([]);
    const { user } = renderWithProviders(<PrescriptionRegister scope={scope} today="2026-11-15" />);
    expect(
      await screen.findByText("No supplies on prescription match, from 2026-11-01 to 2026-11-15."),
    ).toBeVisible();
    const reads = registerReads(fetcher).length;
    const from = screen.getByLabelText("From");
    await user.clear(from);
    await user.type(from, "2025-01-01");
    await user.click(screen.getByRole("button", { name: "Show register" }));
    expect(await screen.findByText("A period is at most 366 days")).toBeVisible();
    expect(registerReads(fetcher)).toHaveLength(reads);
  });
});
