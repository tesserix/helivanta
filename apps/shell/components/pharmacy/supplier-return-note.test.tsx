import { act, screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import type { PharmacySupplierReturn } from "@helivanta/api";
import { SupplierReturnNoteDownload } from "./supplier-return-note";

const scope = {
  subject: "TEST-reader",
  tenantId: "0199e000-0000-7000-8000-000000000001",
  pharmacyId: "0199e000-0000-7000-8000-000000000002",
  outletId: "0199e000-0000-7000-8000-000000000003",
};
const id = "0199e000-0000-7000-8000-000000000004";
const value: PharmacySupplierReturn = {
  id,
  outlet_id: scope.outletId,
  number: 1,
  payable_id: id,
  payee_reference: "TEST-EXT",
  payee_name: "TEST Supplier",
  document_reference: "TEST-BILL",
  currency: "INR",
  expected_credit_minor: 1500,
  reason: "TEST return reason",
  prepared_by: scope.subject,
  prepared_at: "2026-11-21T00:00:00Z",
  state: "dispatched",
  version: 1,
  lines: [
    {
      id,
      batch_id: id,
      product_id: id,
      product_version: 1,
      product_name: "TEST Medicine",
      generic_name: "TEST Generic",
      unit_code: "tablet",
      lot_number: "TEST LOT",
      expires_on: "2028-01-01",
      receipt_units: 80,
      source_reference: "TEST delivery",
      source_created_at: "2026-11-21T00:00:00Z",
      receipt_id: id,
      quarantine_location_id: id,
      quantity_units: 4,
      expected_credit_minor: 1500,
    },
  ],
};
const bytes = new TextEncoder().encode("<!doctype html><title>TEST Commercial note</title>");
async function response() {
  const digest = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)), (b) =>
    b.toString(16).padStart(2, "0"),
  ).join("");
  return new Response(bytes, {
    headers: {
      "Content-Type": "text/html; charset=utf-8",
      "Content-Disposition": 'attachment; filename="supplier-return-1.html"',
      "Cache-Control": "private, no-store",
      "Content-Security-Policy": "sandbox; default-src 'none'",
      "X-Supplier-Return-Tenant-Id": scope.tenantId,
      "X-Supplier-Return-Outlet-Id": scope.outletId,
      "X-Supplier-Return-Id": id,
      "X-Supplier-Return-Number": "1",
      "X-Export-Row-Count": "1",
      "X-Export-Byte-Count": String(bytes.byteLength),
      "X-Export-Sha256": digest,
    },
  });
}
function downloads() {
  const create = vi.fn(() => "blob:test-note"),
    revoke = vi.fn();
  vi.stubGlobal(
    "URL",
    Object.assign(class extends URL {}, { createObjectURL: create, revokeObjectURL: revoke }),
  );
  const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
  return { create, revoke, click };
}
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it("downloads a verified note using a named control and revokes its temporary URL", async () => {
  const { create, revoke, click } = downloads();
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(await response()));
  const { user } = renderWithProviders(
    <SupplierReturnNoteDownload scope={scope} value={value} disabled={false} />,
  );
  await user.click(screen.getByRole("button", { name: "Download commercial return note" }));
  await waitFor(() => expect(click).toHaveBeenCalledOnce());
  expect(create).toHaveBeenCalledOnce();
  expect(revoke).toHaveBeenCalledWith("blob:test-note");
  expect(screen.getByRole("status")).toHaveTextContent("Open the note to print or save as PDF");
});

it("hides the note control for a draft", () => {
  renderWithProviders(
    <SupplierReturnNoteDownload
      scope={scope}
      value={{ ...value, state: "draft", version: 0 }}
      disabled={false}
    />,
  );
  expect(
    screen.queryByRole("button", { name: "Download commercial return note" }),
  ).not.toBeInTheDocument();
});

it("never downloads a late response after unmount and suppresses duplicate clicks", async () => {
  const { create, click } = downloads();
  let resolve!: (response: Response) => void;
  const fetch = vi.fn<(url: string, init?: RequestInit) => Promise<Response>>(
    () =>
      new Promise((r) => {
        resolve = r;
      }),
  );
  vi.stubGlobal("fetch", fetch);
  const { user, unmount } = renderWithProviders(
    <SupplierReturnNoteDownload scope={scope} value={value} disabled={false} />,
  );
  await user.dblClick(screen.getByRole("button", { name: "Download commercial return note" }));
  expect(fetch).toHaveBeenCalledOnce();
  expect(screen.getByRole("button", { name: "Verifying commercial note…" })).toBeDisabled();
  unmount();
  expect(fetch.mock.calls[0]?.[1]?.signal?.aborted).toBe(true);
  await act(async () => resolve(await response()));
  expect(click).not.toHaveBeenCalled();
  expect(create).not.toHaveBeenCalled();
});

it("conceals private refusal details and requires an explicit access recheck", async () => {
  const { create } = downloads();
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        new Response(JSON.stringify({ error: "forbidden", message: "TEST private supplier" }), {
          status: 403,
        }),
      ),
  );
  const { user } = renderWithProviders(
    <SupplierReturnNoteDownload scope={scope} value={value} disabled={false} />,
  );
  await user.click(screen.getByRole("button", { name: "Download commercial return note" }));
  expect(await screen.findByRole("alert")).not.toHaveTextContent("TEST private supplier");
  expect(screen.getByRole("button", { name: "Download commercial return note" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Check return-note access" })).toBeVisible();
  expect(create).not.toHaveBeenCalled();
});
