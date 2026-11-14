import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { WarehouseQuality } from "./warehouse-quality";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-reviewer",
  partyId: "019a55c0-1111-7111-8111-111111111112",
  premiseId: "019a55c0-1111-7111-8111-111111111113",
};
const receipt = {
  id: "019a55c0-1111-7111-8111-111111111114",
  owner_tenant_id: scope.tenantId,
  party_id: scope.partyId,
  premise_id: scope.premiseId,
  input: {
    product_version_id: "019a55c0-1111-7111-8111-111111111115",
    lot_number: "TEST-LOT",
    expires_on: "2030-12-31",
    quantity_units: 10,
    unit_code: "pack",
    source_reference: "TEST-1",
    reason: "TEST physical count",
  },
  stock_status: "quarantine" as const,
  available_units: 0,
  product_identity_verified: false as const,
  actor_subject: "TEST-receiver",
  created_at: "2026-11-08T00:00:00Z",
};
const identity = {
  id: "019a55c0-1111-7111-8111-111111111116",
  owner_tenant_id: scope.tenantId,
  party_id: scope.partyId,
  premise_id: scope.premiseId,
  receipt_id: receipt.id,
  input: {
    expected_version: 0,
    product_version_id: receipt.input.product_version_id,
    unit_kind: "pack",
    expected_pack_size: 10,
    expected_base_units: 100,
    reason: "TEST label checked",
  },
  version: 1,
  quantity_base_units: 100,
  source: {
    product_id: receipt.input.product_version_id,
    version_id: receipt.input.product_version_id,
    formulation_id: receipt.input.product_version_id,
    formulation_version_id: receipt.input.product_version_id,
    brand_name: "TEST Brand",
    manufacturer: "TEST Manufacturer",
    formulation_label: "TEST capsule",
    pack_size: 10,
    unit_of_issue: "capsule",
    regulatory_class: "H",
    ndps: false,
  },
  status: "matched",
  actor_subject: "TEST-counter",
  created_at: "2026-11-08T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());
it("recovers a lost quality response with one write and freezes intent", async () => {
  let saved: Record<string, unknown> | undefined;
  let writes = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/inspection"))
        return Response.json({
          owner_tenant_id: scope.tenantId,
          party_id: scope.partyId,
          premise_id: scope.premiseId,
          receipt_id: receipt.id,
          identity,
          ...(saved ? { quality: saved } : {}),
        });
      if (init?.method === "PUT") {
        writes++;
        saved = {
          id: url.split("/").at(-1),
          owner_tenant_id: scope.tenantId,
          party_id: scope.partyId,
          premise_id: scope.premiseId,
          receipt_id: receipt.id,
          input: JSON.parse(String(init.body)),
          version: 1,
          status: "released",
          released_base_units: 100,
          actor_subject: scope.subject,
          created_at: "2026-11-08T01:00:00Z",
        };
        throw new TypeError("TEST lost committed response");
      }
      return Response.json(saved);
    }),
  );
  renderWithProviders(
    <WarehouseQuality scope={scope} receipt={receipt} canCheck={false} canReview />,
  );
  const user = userEvent.setup();
  await user.type(
    await screen.findByLabelText("Quality review reason"),
    "TEST sealed packs checked",
  );
  await user.click(screen.getByRole("button", { name: "Release inspected stock" }));
  await screen.findByRole("button", { name: "Recover quality decision" });
  expect(screen.getByLabelText("Quality review reason")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Recover quality decision" }));
  await screen.findByText("Quality decision recorded.");
  await waitFor(() => expect(writes).toBe(1));
});
it("hides retained stock evidence after a current inspection refusal", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        Response.json({ error: "not_found", message: "unavailable" }, { status: 404 }),
      ),
  );
  renderWithProviders(<WarehouseQuality scope={scope} receipt={receipt} canCheck canReview />);
  await screen.findByText("Current stock inspection is unavailable.");
  expect(screen.queryByRole("button", { name: "Release inspected stock" })).not.toBeInTheDocument();
  expect(screen.queryByText("TEST Brand")).not.toBeInTheDocument();
});
