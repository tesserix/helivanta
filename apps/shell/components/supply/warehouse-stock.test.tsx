import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { WarehouseStock } from "./warehouse-stock";

afterEach(() => vi.unstubAllGlobals());
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-inventory",
  partyId: "019a55c0-1111-7111-8111-111111111112",
  premiseId: "019a55c0-1111-7111-8111-111111111113",
};
const location = {
  id: scope.premiseId,
  code: "TEST-PUNE",
  name: "TEST Pune warehouse",
  status: "active",
};
const product = {
  product_id: "019a55c0-1111-7111-8111-111111111114",
  version_id: "019a55c0-1111-7111-8111-111111111115",
  formulation_id: "019a55c0-1111-7111-8111-111111111116",
  formulation_version_id: "019a55c0-1111-7111-8111-111111111117",
  brand_name: "TEST Product",
  manufacturer: "TEST Manufacturer",
  formulation_label: "TEST capsule",
  pack_size: 10,
  unit_of_issue: "capsule",
  regulatory_class: "H",
  ndps: false,
};

it("recovers a lost receipt response without posting the count again", async () => {
  let saved: Record<string, unknown> | null = null;
  let writes = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return Response.json({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: ["supply.operations.read", "supply.operations.write"],
        });
      if (url.includes("/stock/products/by-gtin/")) return Response.json(product);
      if (init?.method === "PUT") {
        writes++;
        saved = {
          id: url.split("/").at(-1),
          owner_tenant_id: scope.tenantId,
          party_id: scope.partyId,
          premise_id: scope.premiseId,
          input: JSON.parse(String(init.body)),
          stock_status: "quarantine",
          available_units: 0,
          product_identity_verified: false,
          actor_subject: scope.subject,
          created_at: "2026-11-07T00:00:00Z",
        };
        throw new TypeError("TEST response lost after commit");
      }
      if (/\/stock\/receipts\/[0-9a-f-]+$/.test(url)) return Response.json(saved);
      return Response.json({ data: [], page: { has_more: false, next_cursor: null } });
    }),
  );
  renderWithProviders(
    <WarehouseStock
      scope={scope}
      location={location}
      roles={["inventory"]}
      canWrite
      onClose={() => {}}
    />,
  );
  const user = userEvent.setup();
  await user.type(await screen.findByLabelText("Product barcode"), "8901234567890");
  await user.click(screen.getByRole("button", { name: "Find product" }));
  expect(await screen.findByText("TEST Product")).toBeInTheDocument();
  await user.type(screen.getByLabelText("Lot number"), "TEST-LOT");
  await user.type(screen.getByLabelText("Expiry date"), "2030-12-31");
  await user.type(screen.getByLabelText("Packs counted"), "100");
  await user.type(screen.getByLabelText("Source document line"), "TEST-GRN:1");
  await user.type(screen.getByLabelText("Reason"), "TEST physical receipt counted");
  await user.click(screen.getByRole("button", { name: "Record receipt" }));
  await screen.findByRole("button", { name: "Recover receipt" });
  expect(screen.getByLabelText("Packs counted")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Recover receipt" }));
  await screen.findByText("Receipt recorded in quarantine.");
  await waitFor(() => expect(writes).toBe(1));
});
