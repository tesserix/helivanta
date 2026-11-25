import userEvent from "@testing-library/user-event";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { NavigationGuardProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import type { PharmacyPayableDetail } from "@helivanta/api";
import { PurchaseTax } from "./purchase-tax";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
  subject: "TEST-owner",
};
const doc = {
  id: "0199e000-0000-7000-8000-000000000020",
  document_reference: "TEST-BILL",
  currency: "INR",
  lines: [
    {
      description: "TEST inventory",
      category: "other",
      receipt_id: null,
      net_minor: 10000,
      tax_minor: 1200,
      recoverable_tax_minor: 600,
      landed_minor: 0,
    },
  ],
} as PharmacyPayableDetail;
afterEach(() => vi.unstubAllGlobals());
it("explains missing source evidence and shows the original posted line totals", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ evidence: null })),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <PurchaseTax scope={scope} doc={doc} writable />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByText(/No purchase tax evidence recorded/)).toBeVisible();
  expect(screen.getByText(/Posted tax: INR 12.00/)).toBeVisible();
  expect(screen.getByRole("button", { name: "Record purchase tax evidence" })).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Record purchase tax evidence" }));
  expect(await screen.findByText(/must match/)).toBeVisible();
});
it("keeps evidence read-only without outlet editor access", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ evidence: null })),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <PurchaseTax scope={scope} doc={doc} writable={false} />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByText(/No purchase tax evidence recorded/)).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "Record purchase tax evidence" }),
  ).not.toBeInTheDocument();
});

it("freezes an uncertain save and retries the original command without changing its identity", async () => {
  const puts: { url: string; body: Record<string, unknown> }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit = {}) => {
      if (init.method === "PUT") {
        const body = JSON.parse(String(init.body)) as Record<string, unknown>;
        puts.push({ url, body });
        if (puts.length === 1) throw new Error("TEST response lost after commit");
        return Response.json({
          ...body,
          id: url.split("/").at(-1),
          outlet_id: scope.outletId,
          payable_id: doc.id,
          recorded_by: scope.subject,
          recorded_at: "2026-11-26T00:00:00Z",
        });
      }
      return Response.json({ evidence: null });
    }),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <PurchaseTax scope={scope} doc={doc} writable />
    </NavigationGuardProvider>,
  );
  await screen.findByText(/No purchase tax evidence recorded/);
  const user = userEvent.setup();
  fireEvent.change(screen.getByLabelText("Supplier GSTIN (leave blank if unregistered)"), {
    target: { value: "07ABCDE1234F1Z2" },
  });
  for (const label of ["Supplier state / territory", "Place of supply on invoice"]) {
    await user.click(screen.getByRole("combobox", { name: label }));
    await user.click(screen.getByRole("option", { name: "Delhi" }));
  }
  for (const [label, value] of Object.entries({
    "Source / correction reason": "TEST source",
    "GST rate (%)": "12",
    "CGST (INR)": "6.00",
    "SGST (INR)": "6.00",
    "Recoverable CGST (INR)": "3.00",
    "Recoverable SGST (INR)": "3.00",
  }))
    fireEvent.change(screen.getByLabelText(label, { exact: true }), { target: { value } });
  await user.click(screen.getByRole("button", { name: "Record purchase tax evidence" }));
  expect(await screen.findByText(/recording outcome is uncertain/)).toBeVisible();
  expect(screen.getByLabelText("Source / correction reason")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Try again" }));
  await waitFor(() => expect(puts).toHaveLength(2));
  expect(puts[1]).toEqual(puts[0]);
  expect(await screen.findByRole("button", { name: "Record corrected evidence" })).toBeVisible();
});
