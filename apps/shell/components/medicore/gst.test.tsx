import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { GstSetup, TaxInvoices } from "./gst";

const hospital = {
  id: "0199e000-0000-7000-8000-00000000a001",
  name: "TEST General",
  access_level: "manager" as const,
};
const admission = "0199e000-0000-7000-8000-0000000000a9";
const invoice = {
  id: "0199e000-0000-7000-8000-0000000000f1",
  hospital_id: hospital.id,
  admission_id: admission,
  number: "IP/2627/1",
  issue_date: "2026-10-31",
  issued_at: "2026-10-31T06:00:00Z",
  issued_by: "user-clerk",
  gstin: "27AAPFU0939F1ZV",
  supplier_name: "TEST General Hospital Pvt Ltd",
  supplier_address: "1 MG Road, Pune",
  state_code: "27",
  recipient_name: "Asha Rao",
  recipient_address: "12 Park Street, Pune",
  lines: 2,
  value_minor: 1400000,
  discount_minor: 100000,
  taxable_minor: 600000,
  exempt_minor: 800000,
  cgst_minor: 15000,
  sgst_minor: 15000,
  total_minor: 1430000,
  creditable_lines: 0,
  currency: "INR",
};
// A credit note's totals carry no discount: its lines copy the invoice's values.
const note = {
  ...Object.fromEntries(Object.entries(invoice).filter(([k]) => k !== "discount_minor")),
  id: "0199e000-0000-7000-8000-0000000000c1",
  number: "CN/2627/1",
  invoice_id: invoice.id,
  invoice_number: invoice.number,
  invoice_date: "2026-10-31",
  issue_date: "2026-11-01",
  lines: 1,
  value_minor: 600000,
  taxable_minor: 600000,
  exempt_minor: 0,
  total_minor: 630000,
};
const lines = [
  {
    entry_id: "e1",
    census_date: "2026-10-30",
    bed_class: "private",
    sac: "9993",
    policy_version: "IN-ROOM-2022-07-18",
    value_minor: 600000,
    discount_minor: 0,
    taxable: true,
    cgst_bp: 250,
    sgst_bp: 250,
    cgst_minor: 15000,
    sgst_minor: 15000,
  },
  {
    entry_id: "e2",
    census_date: "2026-10-31",
    bed_class: "icu",
    sac: "9993",
    policy_version: "IN-ROOM-2022-07-18",
    value_minor: 800000,
    discount_minor: 100000,
    taxable: false,
    cgst_bp: 0,
    sgst_bp: 0,
    cgst_minor: 0,
    sgst_minor: 0,
  },
];
const json = (status: number, body: unknown) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
const page = (data: unknown[]) => json(200, { data, page: { next_cursor: null, has_more: false } });

function server(
  opts: {
    perms?: string[];
    registered?: boolean;
    zone?: boolean;
    creditable?: number;
    notes?: (typeof note)[];
  } = {},
) {
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.includes("/iam/me/permissions"))
      return json(200, {
        subject: "user-admin",
        data: opts.perms ?? ["billing.account.read", "billing.tax.manage", "billing.bill.issue"],
      });
    if (url.endsWith("/gst-registration") && init?.method === "PUT")
      return json(200, { ...JSON.parse(String(init.body)), applied_at: null });
    if (url.endsWith("/gst-registration"))
      return opts.registered
        ? json(200, {
            hospital_id: hospital.id,
            gstin: invoice.gstin,
            legal_name: invoice.supplier_name,
            address: invoice.supplier_address,
            effective_from: "2026-10-31",
            applied_at: "2026-10-31T06:00:00Z",
          })
        : json(404, { error: "not_configured", message: "not registered" });
    if (url.endsWith("/bed-charging"))
      return opts.zone === false
        ? json(404, { error: "not_configured", message: "not set up" })
        : json(200, { hospital_id: hospital.id, timezone: "Asia/Kolkata" });
    if (url.includes("/credit-notes/") && init?.method === "PUT") return json(201, note);
    if (url.includes("/tax-invoices/") && init?.method === "PUT") return json(201, invoice);
    if (url.endsWith("/lines")) return page(lines);
    if (url.endsWith("/tax-invoices"))
      return page([{ ...invoice, creditable_lines: opts.creditable ?? 0 }]);
    if (url.endsWith("/credit-notes")) return page(opts.notes ?? []);
    return page([]);
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
const calls = (f: ReturnType<typeof server>, method: string) =>
  f.mock.calls.filter(([, init]) => (init as RequestInit | undefined)?.method === method);

beforeEach(() => {
  // 20:00 UTC on 30 Oct is already 31 Oct in the hospital (IST).
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-10-30T20:00:00Z"));
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("GstSetup", () => {
  it("records the registration from the hospital's tomorrow, refusing a malformed GSTIN inline", async () => {
    const fetcher = server();
    const { user } = renderWithProviders(<GstSetup hospital={hospital} />);
    const from = await screen.findByLabelText("Taxed from");
    expect(from).toHaveValue("2026-11-01");
    await user.type(screen.getByLabelText("GSTIN"), "27AAPFU0939");
    await user.type(screen.getByLabelText("Legal name"), "TEST General Hospital Pvt Ltd");
    await user.type(screen.getByLabelText("Registered address"), "1 MG Road, Pune");
    await user.click(screen.getByRole("button", { name: "Record GST registration" }));
    expect(await screen.findByText("Enter the 15-character GSTIN")).toBeInTheDocument();
    expect(calls(fetcher, "PUT")).toHaveLength(0);
    await user.type(screen.getByLabelText("GSTIN"), "f1zv");
    await user.click(screen.getByRole("button", { name: "Record GST registration" }));
    await waitFor(() => expect(calls(fetcher, "PUT")).toHaveLength(1));
    const [url, init] = calls(fetcher, "PUT")[0]!;
    expect(String(url)).toContain(`/billing/hospitals/${hospital.id}/gst-registration`);
    expect(JSON.parse(String((init as RequestInit).body))).toEqual({
      gstin: "27AAPFU0939F1ZV",
      legal_name: "TEST General Hospital Pvt Ltd",
      address: "1 MG Road, Pune",
      effective_from: "2026-11-01",
    });
  });

  it("shows a recorded registration", async () => {
    server({ registered: true });
    renderWithProviders(<GstSetup hospital={hospital} />);
    const dl = await screen.findByLabelText("GST registration");
    expect(dl).toHaveTextContent("27AAPFU0939F1ZV");
    expect(dl).toHaveTextContent("2026-10-31");
    expect(
      screen.queryByRole("button", { name: "Record GST registration" }),
    ).not.toBeInTheDocument();
  });

  it("asks for the timezone before a registration can be dated", async () => {
    server({ zone: false });
    renderWithProviders(<GstSetup hospital={hospital} />);
    expect(
      await screen.findByText(/Set the hospital's timezone under bed charges/),
    ).toBeInTheDocument();
  });

  it("is not shown without the tax permission", async () => {
    const fetcher = server({ perms: ["billing.account.read"] });
    renderWithProviders(<GstSetup hospital={hospital} />);
    await waitFor(() =>
      expect(fetcher.mock.calls.some(([u]) => String(u).includes("/iam/me/permissions"))).toBe(
        true,
      ),
    );
    expect(screen.queryByRole("heading", { name: /GST at/ })).not.toBeInTheDocument();
  });
});

describe("TaxInvoices", () => {
  it("opens an invoice with its taxable and exempt rooms and its totals", async () => {
    server();
    const { user } = renderWithProviders(
      <TaxInvoices hospital={hospital} admissionId={admission} hasFinalBill />,
    );
    await user.click(await screen.findByRole("button", { name: "IP/2627/1" }));
    const table = await screen.findByRole("table", { name: "Invoice lines" });
    const [, taxed, exempt] = within(table).getAllByRole("row");
    expect(taxed).toHaveTextContent("2026-10-30 · Private");
    expect(taxed).toHaveTextContent("2.5% INR 150.00");
    expect(exempt).toHaveTextContent("ICU · exempt");
    expect(exempt).not.toHaveTextContent("%");
    // Rule 46(1)(i): the value after the discount, and the discount itself.
    expect(exempt).toHaveTextContent("INR 8,000.00after INR 1,000.00 discount");
    expect(taxed).not.toHaveTextContent("discount");
    const totals = screen.getByLabelText("Invoice totals");
    expect(totals).toHaveTextContent("Taxable valueINR 6,000.00");
    expect(totals).toHaveTextContent("Exempt valueINR 8,000.00");
    expect(totals).toHaveTextContent("DiscountsINR 1,000.00");
    expect(totals).toHaveTextContent("Invoice totalINR 14,300.00");
    expect(screen.getByText(/GSTIN 27AAPFU0939F1ZV/)).toBeInTheDocument();
  });

  it("issues an invoice to a named recipient under a fresh UUIDv7 id", async () => {
    const fetcher = server();
    const { user } = renderWithProviders(
      <TaxInvoices hospital={hospital} admissionId={admission} hasFinalBill />,
    );
    await user.click(await screen.findByRole("button", { name: "Issue tax invoice" }));
    expect(await screen.findByText("Enter the name on the invoice")).toBeInTheDocument();
    expect(calls(fetcher, "PUT")).toHaveLength(0);
    await user.type(screen.getByLabelText("Bill to"), "Asha Rao");
    await user.type(screen.getByLabelText("Address"), "12 Park Street, Pune");
    await user.click(screen.getByRole("button", { name: "Issue tax invoice" }));
    await waitFor(() => expect(calls(fetcher, "PUT")).toHaveLength(1));
    const [url, init] = calls(fetcher, "PUT")[0]!;
    expect(String(url)).toMatch(
      new RegExp(`/admissions/${admission}/tax-invoices/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-`),
    );
    expect(JSON.parse(String((init as RequestInit).body))).toEqual({
      recipient_name: "Asha Rao",
      recipient_address: "12 Park Street, Pune",
    });
  });

  it("offers no issue before a final bill or without the desk's permission", async () => {
    server();
    const { unmount } = renderWithProviders(
      <TaxInvoices hospital={hospital} admissionId={admission} hasFinalBill={false} />,
    );
    await screen.findByRole("button", { name: "IP/2627/1" });
    expect(screen.queryByRole("button", { name: "Issue tax invoice" })).not.toBeInTheDocument();
    unmount();
    server({ perms: ["billing.account.read"] });
    renderWithProviders(<TaxInvoices hospital={hospital} admissionId={admission} hasFinalBill />);
    await screen.findByRole("button", { name: "IP/2627/1" });
    expect(screen.queryByRole("button", { name: "Issue tax invoice" })).not.toBeInTheDocument();
  });

  it("issues a credit note for an invoice's reversed days under a fresh UUIDv7 id", async () => {
    const fetcher = server({ creditable: 1 });
    const { user } = renderWithProviders(
      <TaxInvoices hospital={hospital} admissionId={admission} hasFinalBill />,
    );
    expect(await screen.findByText(/1 reversed, awaiting a credit note/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "IP/2627/1" }));
    await user.click(await screen.findByRole("button", { name: "Issue credit note" }));
    await waitFor(() => expect(calls(fetcher, "PUT")).toHaveLength(1));
    const [url, init] = calls(fetcher, "PUT")[0]!;
    expect(String(url)).toMatch(
      new RegExp(
        `/admissions/${admission}/tax-invoices/${invoice.id}/credit-notes/[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-`,
      ),
    );
    expect((init as RequestInit).body).toBeUndefined();
    expect(await screen.findByText("Credit note issued")).toBeInTheDocument();
  });

  it("refreshes the invoice when a credit note is refused", async () => {
    let creditable = 1;
    const fetcher = server({ creditable: 1 });
    const base = fetcher.getMockImplementation()!;
    fetcher.mockImplementation(async (url: string, init?: RequestInit) => {
      if (url.includes("/credit-notes/") && init?.method === "PUT") {
        creditable = 0;
        return json(409, { error: "nothing_to_credit", message: "nothing to credit" });
      }
      if (url.endsWith("/tax-invoices"))
        return page([{ ...invoice, creditable_lines: creditable }]);
      return base(url, init);
    });
    const { user } = renderWithProviders(
      <TaxInvoices hospital={hospital} admissionId={admission} hasFinalBill />,
    );
    await user.click(await screen.findByRole("button", { name: "IP/2627/1" }));
    await user.click(await screen.findByRole("button", { name: "Issue credit note" }));
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Issue credit note" })).not.toBeInTheDocument(),
    );
  });

  it("offers no credit note without a reversed line or without the desk's permission", async () => {
    server();
    const { user, unmount } = renderWithProviders(
      <TaxInvoices hospital={hospital} admissionId={admission} hasFinalBill />,
    );
    await user.click(await screen.findByRole("button", { name: "IP/2627/1" }));
    await screen.findByRole("table", { name: "Invoice lines" });
    expect(screen.queryByRole("button", { name: "Issue credit note" })).not.toBeInTheDocument();
    unmount();
    server({ creditable: 1, perms: ["billing.account.read"] });
    const view = renderWithProviders(
      <TaxInvoices hospital={hospital} admissionId={admission} hasFinalBill />,
    );
    await view.user.click(await screen.findByRole("button", { name: "IP/2627/1" }));
    await screen.findByRole("table", { name: "Invoice lines" });
    expect(screen.queryByRole("button", { name: "Issue credit note" })).not.toBeInTheDocument();
  });

  it("lists credit notes against their invoice and opens one", async () => {
    server({ notes: [note] });
    const { user } = renderWithProviders(
      <TaxInvoices hospital={hospital} admissionId={admission} hasFinalBill />,
    );
    await user.click(await screen.findByRole("button", { name: "CN/2627/1" }));
    expect(screen.getByText(/against IP\/2627\/1 · INR 6,300.00/)).toBeInTheDocument();
    expect(
      await screen.findByText("Credit note CN/2627/1 against IP/2627/1 of 2026-10-31"),
    ).toBeInTheDocument();
    expect(await screen.findByRole("table", { name: "Credit note lines" })).toBeInTheDocument();
    expect(screen.getByLabelText("Credit note totals")).toHaveTextContent(
      "Total creditedINR 6,300.00",
    );
  });
});
