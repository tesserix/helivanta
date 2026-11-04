import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { waitFor } from "@testing-library/react";
import {
  ItemSales,
  PatientSaleLines,
  PatientSales,
  ReceiptsRegister,
  StockSales,
} from "./sales-reports";
import { chooseOption } from "../../test/select";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const period = { from: "2026-11-01", to: "2026-11-15" };

const item = {
  product_id: "0199e000-0000-7000-8000-000000000101",
  sku: "TEST-PCM-500",
  brand_name: "TEST Paracetamol",
  generic_name: "TEST paracetamol",
  manufacturer: "TEST Manufacturer",
  pack_size: 10,
  unit_of_issue: "tablet",
  currency: "INR",
  units_sold: 7,
  sales_minor: 8850,
  discount_minor: 0,
  sales_taxable_minor: 7902,
  sales_cgst_minor: 474,
  sales_sgst_minor: 474,
  mrp_low_minor: 12500,
  mrp_high_minor: 13000,
  units_returned: 2,
  credits_minor: 2500,
  credits_taxable_minor: 2232,
  credits_cgst_minor: 134,
  credits_sgst_minor: 134,
  net_units: 5,
  net_minor: 6350,
};

const receipt = (over: object) => ({
  id: crypto.randomUUID(),
  posted_at: "2026-11-01T06:30:00Z",
  day: "2026-11-01",
  direction: "receipt",
  document_id: "0199e000-0000-7000-8000-000000000201",
  document_number: "PH/2627/1",
  kind: "cash",
  reference: null,
  amount_minor: 6250,
  currency: "INR",
  ...over,
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function serve(rows: unknown[] | "fail") {
  const fetcher = vi.fn<(url: string) => Promise<Response>>(async () =>
    rows === "fail"
      ? new Response(JSON.stringify({ error: "internal", message: "no" }), { status: 500 })
      : new Response(JSON.stringify({ data: rows, page: { next_cursor: null, has_more: false } })),
  );
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}

describe("ItemSales", () => {
  it("shows each product's sales, returns and net for the period", async () => {
    const fetcher = serve([item]);
    renderWithProviders(<ItemSales scope={scope} period={period} />);
    await screen.findByRole("table", { name: "Sales by item" });
    expect(
      fetcher.mock.calls.some(([url]) =>
        String(url).includes("/reports/items?from=2026-11-01&to=2026-11-15"),
      ),
    ).toBe(true);
    const row = screen.getByRole("row", { name: /TEST Paracetamol/ });
    expect(row).toHaveTextContent("TEST-PCM-500, pack of 10 tablet");
    expect(row).toHaveTextContent("7 units for INR 88.50");
    expect(row).toHaveTextContent("MRP INR 125.00–INR 130.00 a pack");
    expect(row).toHaveTextContent("2 units for INR 25.00");
    expect(row).toHaveTextContent("5 units, INR 63.50");
  });

  it("leaves out the MRP when the period only had returns", async () => {
    serve([{ ...item, units_sold: 0, sales_minor: 0, mrp_low_minor: null, mrp_high_minor: null }]);
    renderWithProviders(<ItemSales scope={scope} period={period} />);
    const row = await screen.findByRole("row", { name: /TEST Paracetamol/ });
    expect(row).not.toHaveTextContent("MRP");
  });

  it("says when nothing was sold and reports a failed load", async () => {
    serve([]);
    const { unmount } = renderWithProviders(<ItemSales scope={scope} period={period} />);
    expect(
      await screen.findByText("No items sold or returned from 2026-11-01 to 2026-11-15."),
    ).toBeVisible();
    unmount();
    serve("fail");
    renderWithProviders(<ItemSales scope={scope} period={period} />);
    expect(await screen.findByText("Could not load sales by item.")).toBeVisible();
  });
});

describe("ReceiptsRegister", () => {
  it("lists each receipt and refund with its mode and reference", async () => {
    serve([
      receipt({}),
      receipt({ kind: "upi", reference: "TEST-UPI-7", amount_minor: 2600 }),
      receipt({ kind: "admission_account", amount_minor: 4000 }),
      receipt({
        day: "2026-11-02",
        direction: "refund",
        document_number: "PH/2627/C1",
        amount_minor: 2500,
      }),
    ]);
    renderWithProviders(<ReceiptsRegister scope={scope} period={period} />);
    await screen.findByRole("table", { name: "Receipts and refunds" });
    const rows = screen.getAllByRole("row").slice(1);
    expect(rows).toHaveLength(4);
    expect(rows[0]).toHaveTextContent("PH/2627/1ReceiptCash—INR 62.50");
    expect(rows[1]).toHaveTextContent("TEST-UPI-7");
    expect(rows[2]).toHaveTextContent("Admission accountnot collected");
    expect(rows[3]).toHaveTextContent("PH/2627/C1RefundCash—INR 25.00");
  });

  it("says when there were no payments and reports a failed load", async () => {
    serve([]);
    const { unmount } = renderWithProviders(<ReceiptsRegister scope={scope} period={period} />);
    expect(
      await screen.findByText("No receipts or refunds from 2026-11-01 to 2026-11-15."),
    ).toBeVisible();
    unmount();
    serve("fail");
    renderWithProviders(<ReceiptsRegister scope={scope} period={period} />);
    expect(await screen.findByText("Could not load receipts and refunds.")).toBeVisible();
  });
});

const patient = {
  patient_id: "0199e000-0000-7000-8000-000000000301",
  hospital_id: "0199e000-0000-7000-8000-000000000302",
  name: "TEST Asha Rao",
};

const line = (over: object) => ({
  id: crypto.randomUUID(),
  sale_date: "2026-11-01",
  sale_id: "0199e000-0000-7000-8000-000000000201",
  invoice_number: "PH/2627/1",
  line_number: 1,
  admission_bed_code: null,
  product_id: item.product_id,
  sku: "TEST-PCM-500",
  brand_name: "TEST Paracetamol",
  generic_name: "TEST paracetamol",
  pack_size: 10,
  unit_of_issue: "tablet",
  batch_id: "0199e000-0000-7000-8000-000000000401",
  lot_number: "TEST-LOT-1",
  expires_on: "2028-06-30",
  quantity_units: 5,
  mrp_minor: 12500,
  discount_minor: 0,
  amount_minor: 6250,
  currency: "INR",
  returned_units: 2,
  ...over,
});

describe("PatientSales", () => {
  it("reads nothing until a patient is found", async () => {
    const fetcher = serve([]);
    renderWithProviders(<PatientSales scope={scope} period={period} />);
    await waitFor(() => expect(fetcher).toHaveBeenCalled());
    expect(fetcher.mock.calls.some(([url]) => String(url).includes("/reports/patient-sales"))).toBe(
      false,
    );
  });

  it("lists the patient's purchases by batch, with the bed for an admission", async () => {
    const fetcher = serve([
      line({}),
      line({ invoice_number: "PH/2627/2", admission_bed_code: "B-7", returned_units: 0 }),
    ]);
    renderWithProviders(<PatientSaleLines scope={scope} period={period} patient={patient} />);
    await screen.findByRole("table", { name: "Purchases by TEST Asha Rao" });
    expect(String(fetcher.mock.calls[0]![0])).toContain(
      `/reports/patient-sales?patient_id=${patient.patient_id}&from=2026-11-01&to=2026-11-15`,
    );
    const rows = screen.getAllByRole("row").slice(1);
    expect(rows[0]).toHaveTextContent("PH/2627/1, line 1");
    expect(rows[0]).toHaveTextContent("TEST-LOT-1expires 2028-06-30");
    expect(rows[0]).toHaveTextContent("INR 62.50MRP INR 125.00 a pack; discount INR 0.00");
    expect(rows[0]).not.toHaveTextContent("admission");
    expect(rows[1]).toHaveTextContent("admission, bed B-7");
  });

  it("says when the patient bought nothing and reports a failed load", async () => {
    serve([]);
    const { unmount } = renderWithProviders(
      <PatientSaleLines scope={scope} period={period} patient={patient} />,
    );
    expect(
      await screen.findByText("TEST Asha Rao bought nothing from 2026-11-01 to 2026-11-15."),
    ).toBeVisible();
    unmount();
    serve("fail");
    renderWithProviders(<PatientSaleLines scope={scope} period={period} patient={patient} />);
    expect(await screen.findByText("Could not load the patient's purchases.")).toBeVisible();
  });
});

describe("StockSales", () => {
  it("sets each product's sales beside its stock", async () => {
    const fetcher = serve([
      {
        product_id: item.product_id,
        sku: "TEST-PCM-500",
        brand_name: "TEST Paracetamol",
        generic_name: "TEST paracetamol",
        pack_size: 10,
        unit_of_issue: "tablet",
        units_sold: 7,
        units_returned: 2,
        on_hand_units: 195,
        sellable_units: 8,
      },
    ]);
    renderWithProviders(<StockSales scope={scope} period={period} />);
    await screen.findByRole("table", { name: "Sales and stock" });
    expect(
      fetcher.mock.calls.some(([url]) =>
        String(url).includes("/reports/stock-sales?from=2026-11-01&to=2026-11-15"),
      ),
    ).toBe(true);
    const row = screen.getByRole("row", { name: /TEST Paracetamol/ });
    expect(row).toHaveTextContent("TEST-PCM-500, pack of 10 tablet");
    expect(row).toHaveTextContent("721958");
  });

  it("says when nothing has held stock and reports a failed load", async () => {
    serve([]);
    const { unmount } = renderWithProviders(<StockSales scope={scope} period={period} />);
    expect(await screen.findByText("No product has held stock yet.")).toBeVisible();
    unmount();
    serve("fail");
    renderWithProviders(<StockSales scope={scope} period={period} />);
    expect(await screen.findByText("Could not load sales and stock.")).toBeVisible();
  });
});

describe("the category filter (#988)", () => {
  const category = (over: object) => ({
    id: crypto.randomUUID(),
    parent_id: null,
    code: "ANTI",
    name: "Antibiotics",
    retired_at: null,
    created_by: "user-editor",
    created_at: "2026-11-01T05:00:00Z",
    ...over,
  });
  const antibiotics = category({ id: "0199e000-0000-7000-8000-000000000501" });
  const oral = category({
    id: "0199e000-0000-7000-8000-000000000502",
    parent_id: antibiotics.id,
    code: "ORAL",
    name: "Oral",
  });
  const analgesics = category({
    id: "0199e000-0000-7000-8000-000000000503",
    code: "ANAL",
    name: "Analgesics",
    retired_at: "2026-11-02T05:00:00Z",
  });

  function serveWithCategories(permissions: string[]) {
    const page = (data: unknown[]) =>
      new Response(JSON.stringify({ data, page: { next_cursor: null, has_more: false } }));
    const fetcher = vi.fn<(url: string) => Promise<Response>>(async (url) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({ tenant_id: scope.tenantId, subject: "TEST-reader", data: permissions }),
        );
      if (url.includes("/catalogue/categories")) return page([antibiotics, oral, analgesics]);
      return page([item]);
    });
    vi.stubGlobal("fetch", fetcher);
    return fetcher;
  }
  const reports = (fetcher: ReturnType<typeof serveWithCategories>, part: string) =>
    fetcher.mock.calls.map(([url]) => String(url)).filter((url) => url.includes(part));

  it("cuts sales by item by a category, then a subcategory, and drops it with the category", async () => {
    const fetcher = serveWithCategories(["pharmacy.catalogue.read", "pharmacy.sale.read"]);
    const { user } = renderWithProviders(<ItemSales scope={scope} period={period} />);
    const picker = await screen.findByRole("combobox", { name: "Category" });
    expect(screen.queryByRole("combobox", { name: "Subcategory" })).toBeNull();
    await user.click(picker);
    expect(screen.getByRole("option", { name: "Analgesics (ANAL), retired" })).toBeVisible();
    await user.keyboard("{Escape}");
    await chooseOption(user, picker, "Antibiotics (ANTI)");
    await waitFor(() =>
      expect(reports(fetcher, "/reports/items").at(-1)).toContain(`&category_id=${antibiotics.id}`),
    );
    await chooseOption(user, screen.getByRole("combobox", { name: "Subcategory" }), "Oral (ORAL)");
    await waitFor(() =>
      expect(reports(fetcher, "/reports/items").at(-1)).toContain(
        `&category_id=${antibiotics.id}&subcategory_id=${oral.id}`,
      ),
    );
    await chooseOption(user, picker, "Analgesics (ANAL), retired");
    await waitFor(() =>
      expect(reports(fetcher, "/reports/items").at(-1)).toMatch(
        new RegExp(`&category_id=${analgesics.id}$`),
      ),
    );
    expect(screen.queryByRole("combobox", { name: "Subcategory" })).toBeNull();
    await chooseOption(user, picker, "All categories");
    await waitFor(() =>
      expect(reports(fetcher, "/reports/items").at(-1)).toMatch(/to=2026-11-15$/),
    );
  });

  it("cuts sales and stock by a category, and offers no picker without catalogue read", async () => {
    const fetcher = serveWithCategories(["pharmacy.catalogue.read", "pharmacy.stock.read"]);
    const { user, unmount } = renderWithProviders(<StockSales scope={scope} period={period} />);
    await chooseOption(
      user,
      await screen.findByRole("combobox", { name: "Category" }),
      "Antibiotics (ANTI)",
    );
    await waitFor(() =>
      expect(reports(fetcher, "/reports/stock-sales").at(-1)).toContain(
        `&category_id=${antibiotics.id}`,
      ),
    );
    unmount();
    // Not the last session's cached permissions: this reader never held catalogue read.
    localStorage.clear();
    const without = serveWithCategories(["pharmacy.stock.read"]);
    renderWithProviders(<StockSales scope={scope} period={period} />);
    await screen.findByRole("table", { name: "Sales and stock" });
    expect(screen.queryByRole("combobox", { name: "Category" })).toBeNull();
    expect(reports(without, "/catalogue/categories")).toHaveLength(0);
  });
});

describe("sales by item by lot and by prescriber (#1060)", () => {
  const rao = {
    prescriber_name: "TEST Dr Asha Rao",
    prescriber_registration: "MMC/2011/04567",
    prescriber_council: "Maharashtra Medical Council",
  };
  const iyer = {
    prescriber_name: "TEST Dr K Iyer",
    prescriber_registration: "KMC/2015/11",
    prescriber_council: "Karnataka Medical Council",
  };

  function serveReports(permissions: string[], more = false) {
    const page = (data: unknown[]) =>
      new Response(JSON.stringify({ data, page: { next_cursor: null, has_more: false } }));
    const fetcher = vi.fn<(url: string) => Promise<Response>>(async (url) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({ tenant_id: scope.tenantId, subject: "TEST-reader", data: permissions }),
        );
      if (url.includes("/reports/prescribers"))
        return new Response(JSON.stringify({ data: [rao, iyer], more }));
      if (url.includes("/catalogue/categories")) return page([]);
      return page([item]);
    });
    vi.stubGlobal("fetch", fetcher);
    return fetcher;
  }
  const itemReads = (fetcher: ReturnType<typeof serveReports>) =>
    fetcher.mock.calls.map(([url]) => String(url)).filter((url) => url.includes("/reports/items"));

  it("applies a lot trimmed, and refuses one over 100 characters", async () => {
    localStorage.clear();
    const fetcher = serveReports(["pharmacy.sale.read"]);
    const { user } = renderWithProviders(<ItemSales scope={scope} period={period} />);
    await screen.findByRole("table", { name: "Sales by item" });
    await user.type(screen.getByLabelText("Lot number"), "x".repeat(101));
    await user.click(screen.getByRole("button", { name: "Apply lot" }));
    expect(await screen.findByText("A lot number is at most 100 characters")).toBeVisible();
    expect(itemReads(fetcher).some((u) => u.includes("lot_number"))).toBe(false);
    await user.clear(screen.getByLabelText("Lot number"));
    await user.type(screen.getByLabelText("Lot number"), "  TEST-LOT-7 ");
    await user.click(screen.getByRole("button", { name: "Apply lot" }));
    await waitFor(() => expect(itemReads(fetcher).at(-1)).toContain("&lot_number=TEST-LOT-7"));
    expect(itemReads(fetcher).at(-1)).not.toContain("%20");
  });

  it("cuts by a prescriber from the period's list, sending the registration with its council", async () => {
    localStorage.clear();
    const fetcher = serveReports(["pharmacy.sale.read", "pharmacy.prescription.read"], true);
    const { user } = renderWithProviders(<ItemSales scope={scope} period={period} />);
    const picker = await screen.findByRole("combobox", { name: "Prescriber" });
    expect(
      await screen.findByText(
        "More than 200 prescribers in this period; choose a shorter period to see them all.",
      ),
    ).toBeVisible();
    await waitFor(() => expect(picker).toBeEnabled());
    await chooseOption(
      user,
      picker,
      "TEST Dr Asha Rao (MMC/2011/04567, Maharashtra Medical Council)",
    );
    await waitFor(() =>
      expect(itemReads(fetcher).at(-1)).toContain(
        "&prescriber_registration=MMC%2F2011%2F04567&prescriber_council=Maharashtra%20Medical%20Council",
      ),
    );
    await chooseOption(user, picker, "All sales, with or without a prescription");
    await waitFor(() => expect(itemReads(fetcher).at(-1)).not.toContain("prescriber"));
  });

  it("offers no prescriber and reads no prescribers without prescription read", async () => {
    localStorage.clear();
    const fetcher = serveReports(["pharmacy.sale.read"]);
    renderWithProviders(<ItemSales scope={scope} period={period} />);
    await screen.findByRole("table", { name: "Sales by item" });
    expect(screen.getByLabelText("Lot number")).toBeVisible();
    expect(screen.queryByRole("combobox", { name: "Prescriber" })).toBeNull();
    expect(fetcher.mock.calls.some(([url]) => String(url).includes("/reports/prescribers"))).toBe(
      false,
    );
  });
});
