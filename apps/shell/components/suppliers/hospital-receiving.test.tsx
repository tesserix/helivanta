import { chooseOption } from "../../test/select";
import { act, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider, refusedDefault } from "@helivanta/ui";
import { HospitalReceiving } from "./hospital-receiving";
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: vi.fn(), replace: vi.fn() }) }));
const id = (n: number) => `0199e000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const scope = {
  tenantId: id(1),
  subject: "TEST-receiver",
  hospitalId: id(2),
  departmentId: id(3),
  pharmacyId: id(4),
  outletId: id(5),
};
const shipment = {
  id: id(6),
  allocation_id: id(7),
  order_id: id(8),
  line_id: id(9),
  order_input_sha256: "a".repeat(64),
  source_tenant_id: id(10),
  source_organization_id: id(11),
  source_location_id: id(12),
  buyer_tenant_id: scope.tenantId,
  hospital_id: scope.hospitalId,
  buyer_organization_id: scope.pharmacyId,
  buyer_location_id: scope.outletId,
  product: { owner: "supply", product_id: id(13), version_id: id(14) },
  unit_code: "capsule",
  quantity_units: 10,
  lot_number: "TEST-LOT",
  expires_on: "2030-12-31",
  handoff_reference: "TEST-CHALLAN",
  actor_subject: "TEST-source",
  product_description: "TEST medicine",
  pack_size: 10,
  dispatched_at: "2026-11-08T12:00:00Z",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});
it("lists only owner-verified shipments with their physical handoff evidence", async () => {
  const fetch = vi.fn().mockResolvedValue({ ok: true, json: async () => page([shipment]) });
  vi.stubGlobal("fetch", fetch);
  renderWithProviders(
    <NavigationGuardProvider>
      <HospitalReceiving scope={scope} />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByText(/TEST-CHALLAN/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Inspect TEST-CHALLAN" })).toBeEnabled();
  expect(fetch).toHaveBeenCalledWith(
    expect.stringContaining(
      `/hospitals/${scope.hospitalId}/departments/${scope.departmentId}/outlets/${scope.outletId}/supplier-shipments`,
    ),
    expect.objectContaining({ cache: "no-store" }),
  );
  const headers = new Headers(fetch.mock.calls[0]![1]?.headers);
  expect(headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});
it("shows refused authority instead of a fabricated empty shipment list", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: false,
      status: 403,
      json: async () => ({ error: "forbidden", message: "TEST no current duty" }),
    }),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <HospitalReceiving scope={scope} />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByText(refusedDefault)).toBeInTheDocument();
  expect(screen.queryByText("No supplier shipments for this outlet.")).not.toBeInTheDocument();
});

it("opens the shipment with unrecorded receiving and requires observed delivery facts", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path) => {
      const url = String(path);
      if (url.includes("/supplier-shipments?")) return Response.json(page([shipment]));
      if (url.endsWith("/receiving"))
        return Response.json({ state: "not_recorded", summary: null });
      if (url.includes("/receipts?") || url.includes("/receiving-responses?"))
        return Response.json(page([]));
      return Response.json({ error: "not_found", message: "TEST missing route" }, { status: 404 });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <HospitalReceiving scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Inspect TEST-CHALLAN" }));
  expect(
    await screen.findByText("No receiving has been recorded for this shipment."),
  ).toBeInTheDocument();
  expect(screen.getByLabelText("Observed lot number")).toHaveValue("");
  expect(screen.getByLabelText("Observed expiry date")).toHaveValue("");
  expect(screen.getByRole("button", { name: "Record inspected delivery" })).toBeEnabled();
});

it("retains the original delivery across an uncertain result and failed authority refresh", async () => {
  const writes: { path: string; body: string }[] = [];
  let unavailable = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path, options) => {
      const url = String(path),
        request = options as RequestInit;
      if (request?.method === "PUT") {
        writes.push({ path: url, body: String(request.body) });
        if (writes.length === 1) {
          unavailable = true;
          throw new TypeError("TEST response lost");
        }
        const input = JSON.parse(String(request.body));
        return Response.json({
          id: url.split("/").at(-1),
          owner_tenant_id: scope.tenantId,
          shipment_id: shipment.id,
          order_id: shipment.order_id,
          hospital_id: scope.hospitalId,
          pharmacy_id: scope.pharmacyId,
          outlet_id: scope.outletId,
          input,
          cumulative_counted_units: input.counted_units,
          cumulative_accepted_units: input.accepted_units,
          cumulative_rejected_units: input.rejected_units,
          actor_subject: scope.subject,
          actor_staff_id: id(30),
          created_at: "2026-11-09T01:00:00Z",
        });
      }
      if (url.includes("/supplier-shipments?")) return Response.json(page([shipment]));
      if (url.endsWith("/receiving"))
        return unavailable
          ? Response.json(
              { error: "fulfilment_unavailable", message: "TEST authority unavailable" },
              { status: 503 },
            )
          : Response.json({ state: "not_recorded", summary: null });
      if (url.includes("/receipts?") || url.includes("/receiving-responses?"))
        return Response.json(page([]));
      return Response.json(
        { error: "not_found", message: "TEST route not found" },
        { status: 404 },
      );
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <HospitalReceiving scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Inspect TEST-CHALLAN" }));
  await user.type(await screen.findByLabelText("Physically counted units"), "1");
  await user.clear(screen.getByLabelText("Rejected units"));
  await user.type(screen.getByLabelText("Rejected units"), "1");
  await user.type(await screen.findByLabelText("Observed lot number"), shipment.lot_number);
  await user.type(screen.getByLabelText("Observed expiry date"), shipment.expires_on);
  await user.type(
    screen.getByLabelText("Inspection and count notes"),
    "TEST rejected damaged delivery",
  );
  await user.click(screen.getByRole("button", { name: "Record inspected delivery" }));
  expect(await screen.findByText(/original receiving command is retained/)).toBeInTheDocument();
  expect(writes).toHaveLength(1);
  unavailable = false;
  await user.click(screen.getAllByRole("button", { name: /Try again|Retry|Recheck/ })[0]!);
  await user.click(await screen.findByRole("button", { name: "Retry original delivery" }));
  expect(
    await screen.findByText("Original delivery recorded and current receiving refreshed."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(2);
  expect(writes[1]).toEqual(writes[0]);
  expect(JSON.parse(writes[0]!.body)).not.toHaveProperty("destination_batch_id");
});

const originalReceipt = {
  id: id(40),
  owner_tenant_id: scope.tenantId,
  shipment_id: shipment.id,
  order_id: shipment.order_id,
  hospital_id: scope.hospitalId,
  pharmacy_id: scope.pharmacyId,
  outlet_id: scope.outletId,
  input: {
    destination_product_id: id(41),
    destination_product_version: 1,
    destination_batch_id: id(42),
    quarantine_location_id: id(43),
    counted_units: 5,
    accepted_units: 4,
    rejected_units: 1,
    mrp_minor: 12500,
    observed_lot_number: shipment.lot_number,
    observed_expires_on: shipment.expires_on,
    identity_confirmed: true,
    reason: "TEST original inspected delivery",
  },
  cumulative_counted_units: 5,
  cumulative_accepted_units: 4,
  cumulative_rejected_units: 1,
  movement_id: id(40),
  actor_subject: "TEST-original-recorder",
  actor_staff_id: id(44),
  created_at: "2026-11-08T13:00:00Z",
};
const receivingSummary = {
  id: id(45),
  owner_tenant_id: scope.tenantId,
  shipment_id: shipment.id,
  order_id: shipment.order_id,
  version: 1,
  counted_units: 5,
  accepted_units: 4,
  rejected_units: 1,
  short_units: 0,
  closed: false,
  reason: "TEST count",
  actor_subject: originalReceipt.actor_subject,
  recorded_at: originalReceipt.created_at,
};
it("requires a fresh identity decision for a correction instead of inheriting the original confirmation", async () => {
  const writes: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path, options) => {
      const url = String(path),
        request = options as RequestInit;
      if (request?.method === "PUT") {
        writes.push(JSON.parse(String(request.body)));
        return Response.json(
          { error: "fulfilment_changed", message: "TEST current evidence changed" },
          { status: 409 },
        );
      }
      if (url.includes("/supplier-shipments?")) return Response.json(page([shipment]));
      if (url.endsWith("/receiving"))
        return Response.json({ state: "recorded", summary: receivingSummary });
      if (url.includes("/receipts?")) return Response.json(page([originalReceipt]));
      if (
        url.includes("/corrections?") ||
        url.includes("/clearances?") ||
        url.includes("/releases?") ||
        url.includes("/receiving-responses?")
      )
        return Response.json(page([]));
      return Response.json({ error: "not_found", message: "TEST not found" }, { status: 404 });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <HospitalReceiving scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Inspect TEST-CHALLAN" }));
  expect(
    await screen.findByLabelText(
      "I re-inspected medicine identity before increasing accepted units",
    ),
  ).not.toBeChecked();
  await user.clear(screen.getByLabelText("Corrected accepted units"));
  await user.type(screen.getByLabelText("Corrected accepted units"), "3");
  await user.clear(screen.getByLabelText("Corrected rejected units"));
  await user.type(screen.getByLabelText("Corrected rejected units"), "2");
  await user.type(
    screen.getByLabelText("Correction evidence and reason"),
    "TEST independent recount",
  );
  await user.click(screen.getByRole("button", { name: "Record corrected count" }));
  await screen.findByText("The receiving action could not be verified.");
  expect(writes).toEqual([
    {
      expected_summary_version: 1,
      expected_receipt_version: 1,
      counted_units: 5,
      accepted_units: 3,
      rejected_units: 2,
      identity_confirmed: false,
      reason: "TEST independent recount",
    },
  ]);
});

it("records independent clearance without claiming release, then displays actual shelved stock evidence", async () => {
  let reviewed = false,
    released: Record<string, unknown> | null = null;
  const writes: { path: string; input: unknown }[] = [];
  const closed = { ...receivingSummary, version: 2, closed: true, short_units: 5 };
  const clearance = {
    id: id(60),
    owner_tenant_id: scope.tenantId,
    shipment_id: shipment.id,
    receipt_id: originalReceipt.id,
    pharmacy_id: scope.pharmacyId,
    outlet_id: scope.outletId,
    summary_version: 2,
    state: "review_recorded",
    reason: "TEST independent closed count review",
    actor_subject: scope.subject,
    actor_staff_id: id(61),
    created_at: "2026-11-09T01:00:00Z",
  };
  const shelf = {
    id: id(62),
    owner_tenant_id: scope.tenantId,
    shipment_id: shipment.id,
    pharmacy_id: scope.pharmacyId,
    outlet_id: scope.outletId,
    code: "TEST-S",
    name: "TEST sellable",
    stock_status: "sellable",
    created_at: clearance.created_at,
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path, options) => {
      const url = String(path),
        request = options as RequestInit;
      if (request?.method === "PUT") {
        const input = JSON.parse(String(request.body));
        writes.push({ path: url, input });
        if (url.includes("/clearances/")) {
          reviewed = true;
          return Response.json({
            id: url.split("/").at(-1),
            receipt_id: originalReceipt.id,
            summary_version: 2,
            state: "review_recorded",
          });
        }
        if (url.includes("/releases/")) {
          released = {
            id: url.split("/").at(-1),
            owner_tenant_id: scope.tenantId,
            shipment_id: shipment.id,
            receipt_id: originalReceipt.id,
            pharmacy_id: scope.pharmacyId,
            outlet_id: scope.outletId,
            batch_id: originalReceipt.input.destination_batch_id,
            from_location_id: originalReceipt.input.quarantine_location_id,
            input,
            actor_subject: scope.subject,
            actor_staff_id: id(61),
            created_at: clearance.created_at,
          };
          return Response.json(released);
        }
      }
      if (url.includes("/supplier-shipments?")) return Response.json(page([shipment]));
      if (url.endsWith("/receiving")) return Response.json({ state: "recorded", summary: closed });
      if (url.includes("/receipts?")) return Response.json(page([originalReceipt]));
      if (url.includes("/clearances?")) return Response.json(page(reviewed ? [clearance] : []));
      if (url.includes("/releases?")) return Response.json(page(released ? [released] : []));
      if (url.includes("/receiving/sellable-locations?")) return Response.json(page([shelf]));
      if (url.includes("/corrections?") || url.includes("/receiving-responses?"))
        return Response.json(page([]));
      return Response.json({ error: "not_found", message: "TEST not found" }, { status: 404 });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <HospitalReceiving scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Inspect TEST-CHALLAN" }));
  await user.type(await screen.findByLabelText("Independent review evidence"), clearance.reason);
  await user.click(screen.getByRole("button", { name: "Record independent clearance" }));
  expect(
    await screen.findByText(
      "Independent clearance recorded. Physical stock has not been released by this review.",
    ),
  ).toBeInTheDocument();
  expect(await screen.findByText("No physical release recorded.")).toBeInTheDocument();
  await chooseOption(
    user,
    await screen.findByRole("combobox", { name: "Actual sellable shelf" }),
    "TEST-S · TEST sellable",
  );
  await user.clear(screen.getByLabelText("Physically shelved units"));
  await user.type(screen.getByLabelText("Physically shelved units"), "2");
  await user.type(
    screen.getByLabelText("Physical release evidence"),
    "TEST two capsules physically shelved",
  );
  await user.click(screen.getByRole("button", { name: "Record physical release" }));
  expect(await screen.findByText(/2 units physically released/)).toBeInTheDocument();
  expect(writes).toHaveLength(2);
  expect(writes[1]!.input).toEqual({
    expected_summary_version: 2,
    to_location_id: shelf.id,
    quantity_units: 2,
    reason: "TEST two capsules physically shelved",
  });
});

it.each([
  { currency: "INR", text: "125.00", minor: 12500 },
  { currency: "JPY", text: "125", minor: 125 },
])(
  "freezes the inspected destination batch and owner-currency MRP ($currency) when a response is lost",
  async ({ currency, text, minor }) => {
    const product = {
      id: id(70),
      owner_tenant_id: scope.tenantId,
      shipment_id: shipment.id,
      pharmacy_id: scope.pharmacyId,
      outlet_id: scope.outletId,
      version: 1,
      brand_name: "TEST brand",
      currency,
      generic_name: "TEST generic",
      manufacturer: "TEST maker",
      dosage_form: "capsule",
      unit_code: shipment.unit_code,
      pack_size: shipment.pack_size,
      identity_description: "TEST molecule 500 mg · capsule · oral",
      created_at: "2026-11-08T12:00:00Z",
    };
    const location = {
      id: id(71),
      owner_tenant_id: scope.tenantId,
      shipment_id: shipment.id,
      pharmacy_id: scope.pharmacyId,
      outlet_id: scope.outletId,
      code: "TEST-Q",
      name: "TEST quarantine",
      stock_status: "quarantine",
      created_at: product.created_at,
    };
    const writes: { path: string; body: string }[] = [];
    let saved: Record<string, unknown> | null = null,
      unavailable = false;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (path, options) => {
        const url = String(path),
          request = options as RequestInit;
        if (request?.method === "PUT") {
          writes.push({ path: url, body: String(request.body) });
          if (!saved) {
            const input = JSON.parse(String(request.body));
            saved = {
              ...originalReceipt,
              id: url.split("/").at(-1),
              movement_id: url.split("/").at(-1),
              input,
              actor_subject: scope.subject,
              actor_staff_id: id(72),
            };
            unavailable = true;
            throw new TypeError("TEST accepted-stock response lost");
          }
          return Response.json(saved);
        }
        if (url.includes("/supplier-shipments?")) return Response.json(page([shipment]));
        if (url.endsWith("/receiving"))
          return unavailable
            ? Response.json(
                { error: "fulfilment_unavailable", message: "TEST authority unavailable" },
                { status: 503 },
              )
            : Response.json(
                saved
                  ? { state: "recorded", summary: receivingSummary }
                  : { state: "not_recorded", summary: null },
              );
        if (url.includes("/receipts?")) return Response.json(page(saved ? [saved] : []));
        if (url.includes("/receiving/product-choices?")) return Response.json(page([product]));
        if (url.includes("/receiving/quarantine-locations?"))
          return Response.json(page([location]));
        if (
          url.includes("/batches?") ||
          url.includes("/corrections?") ||
          url.includes("/clearances?") ||
          url.includes("/releases?") ||
          url.includes("/receiving-responses?")
        )
          return Response.json(page([]));
        return Response.json({ error: "not_found", message: "TEST not found" }, { status: 404 });
      }),
    );
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <HospitalReceiving scope={scope} />
      </NavigationGuardProvider>,
    );
    await user.click(await screen.findByRole("button", { name: "Inspect TEST-CHALLAN" }));
    await user.clear(await screen.findByLabelText("Physically counted units"));
    await user.type(screen.getByLabelText("Physically counted units"), "5");
    await user.clear(screen.getByLabelText("Accepted units"));
    await user.type(screen.getByLabelText("Accepted units"), "4");
    await user.clear(screen.getByLabelText("Rejected units"));
    await user.type(screen.getByLabelText("Rejected units"), "1");
    await chooseOption(
      user,
      await screen.findByRole("combobox", { name: "Destination medicine" }),
      /TEST brand/,
    );
    await chooseOption(user, screen.getByRole("combobox", { name: "Quarantine shelf" }), /TEST-Q/);
    await user.click(
      await screen.findByRole("button", { name: "Create a destination lot for this delivery" }),
    );
    await user.type(screen.getByLabelText(`Printed pack MRP (${currency})`), text);
    await user.click(
      screen.getByLabelText(
        "I inspected ingredient, strength, formulation, route, pack and units against the shipment and destination product",
      ),
    );
    await user.type(screen.getByLabelText("Observed lot number"), shipment.lot_number);
    await user.type(screen.getByLabelText("Observed expiry date"), shipment.expires_on);
    await user.type(
      screen.getByLabelText("Inspection and count notes"),
      "TEST physically inspected delivery",
    );
    await user.click(screen.getByRole("button", { name: "Record inspected delivery" }));
    expect(await screen.findByText(/original receiving command is retained/)).toBeInTheDocument();
    unavailable = false;
    await user.click(screen.getAllByRole("button", { name: /Try again|Retry|Recheck/ })[0]!);
    await user.click(await screen.findByRole("button", { name: "Retry original delivery" }));
    expect(
      await screen.findByText("Original delivery recorded and current receiving refreshed."),
    ).toBeInTheDocument();
    expect(writes).toHaveLength(2);
    expect(writes[1]).toEqual(writes[0]);
    expect(JSON.parse(writes[0]!.body)).toMatchObject({
      destination_product_id: product.id,
      destination_product_version: 1,
      destination_batch_id: expect.stringMatching(/^[a-f0-9-]{14}7/),
      quarantine_location_id: location.id,
      mrp_minor: minor,
      identity_confirmed: true,
      counted_units: 5,
      accepted_units: 4,
      rejected_units: 1,
    });
  },
);

it("closes a verified shipment as entirely short without inventing a physical receipt", async () => {
  let closure: Record<string, unknown> | null = null;
  const writes: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path, options) => {
      const url = String(path),
        request = options as RequestInit;
      if (request?.method === "PUT") {
        const input = JSON.parse(String(request.body));
        writes.push(input);
        closure = {
          id: url.split("/").at(-1),
          owner_tenant_id: scope.tenantId,
          shipment_id: shipment.id,
          counted_units: 0,
          accepted_units: 0,
          rejected_units: 0,
          short_units: shipment.quantity_units,
          reason: input.reason,
          actor_subject: scope.subject,
          actor_staff_id: id(80),
          created_at: "2026-11-09T01:00:00Z",
        };
        return Response.json(closure);
      }
      if (url.includes("/supplier-shipments?")) return Response.json(page([shipment]));
      if (url.endsWith("/receiving"))
        return Response.json(
          closure
            ? {
                state: "recorded",
                summary: {
                  ...receivingSummary,
                  id: closure.id,
                  counted_units: 0,
                  accepted_units: 0,
                  rejected_units: 0,
                  short_units: shipment.quantity_units,
                  closed: true,
                },
              }
            : { state: "not_recorded", summary: null },
        );
      if (url.includes("/receipts?") || url.includes("/receiving-responses?"))
        return Response.json(page([]));
      return Response.json({ error: "not_found", message: "TEST missing route" }, { status: 404 });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <HospitalReceiving scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Inspect TEST-CHALLAN" }));
  await user.type(
    await screen.findByLabelText("Closure and shortage reason"),
    "TEST delivery did not arrive; all units short",
  );
  await user.click(screen.getByRole("button", { name: "Close receiving" }));
  expect(
    await screen.findByText("Receiving closure recorded with current shortages."),
  ).toBeInTheDocument();
  expect(writes).toEqual([
    { expected_counted_units: 0, reason: "TEST delivery did not arrive; all units short" },
  ]);
  expect(
    await screen.findByText("Receiving is closed. No new delivery may be recorded."),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Record physical release" })).not.toBeInTheDocument();
});

it("does not silently apply an open correction draft to a newer receiving version", async () => {
  let changed = false,
    writes = 0;
  const correction = {
    id: id(81),
    owner_tenant_id: scope.tenantId,
    shipment_id: shipment.id,
    receipt_id: originalReceipt.id,
    outlet_id: scope.outletId,
    version: 2,
    input: {
      expected_summary_version: 1,
      expected_receipt_version: 1,
      counted_units: 5,
      accepted_units: 3,
      rejected_units: 2,
      identity_confirmed: false,
      reason: "TEST other independent review",
    },
    accepted_delta_units: -1,
    cumulative_counted_units: 5,
    cumulative_accepted_units: 3,
    cumulative_rejected_units: 2,
    actor_subject: "TEST-other-reviewer",
    actor_staff_id: id(82),
    created_at: "2026-11-09T01:00:00Z",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path, options) => {
      const url = String(path),
        request = options as RequestInit;
      if (request?.method === "PUT") {
        writes++;
        return Response.json(
          { error: "fulfilment_changed", message: "TEST stale input" },
          { status: 409 },
        );
      }
      if (url.includes("/supplier-shipments?")) return Response.json(page([shipment]));
      if (url.endsWith("/receiving"))
        return Response.json({
          state: "recorded",
          summary: changed
            ? {
                ...receivingSummary,
                id: correction.id,
                version: 2,
                accepted_units: 3,
                rejected_units: 2,
              }
            : receivingSummary,
        });
      if (url.includes("/receipts?")) return Response.json(page([originalReceipt]));
      if (url.includes("/corrections?")) return Response.json(page(changed ? [correction] : []));
      if (
        url.includes("/clearances?") ||
        url.includes("/releases?") ||
        url.includes("/receiving-responses?")
      )
        return Response.json(page([]));
      return Response.json({ error: "not_found", message: "TEST missing route" }, { status: 404 });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <HospitalReceiving scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Inspect TEST-CHALLAN" }));
  await user.type(
    await screen.findByLabelText("Correction evidence and reason"),
    "TEST unfinished independent recount",
  );
  changed = true;
  await act(async () => {
    const visibility = vi.spyOn(document, "visibilityState", "get");
    visibility.mockReturnValue("hidden");
    document.dispatchEvent(new Event("visibilitychange", { bubbles: true }));
    visibility.mockReturnValue("visible");
    document.dispatchEvent(new Event("visibilitychange", { bubbles: true }));
  });
  expect(
    await screen.findByText(
      "Receiving evidence changed. Recheck the latest count before correcting it.",
    ),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Record corrected count" })).toBeDisabled();
  expect(screen.getByLabelText("Correction evidence and reason")).toHaveValue(
    "TEST unfinished independent recount",
  );
  expect(writes).toBe(0);
});

it("does not prefill a physical count or create a receipt without one", async () => {
  let writes = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (path, options) => {
      const url = String(path),
        request = options as RequestInit;
      if (request?.method === "PUT") {
        writes++;
        return Response.json(
          { error: "fulfilment_changed", message: "TEST no commitment" },
          { status: 409 },
        );
      }
      if (url.includes("/supplier-shipments?")) return Response.json(page([shipment]));
      if (url.endsWith("/receiving"))
        return Response.json({ state: "not_recorded", summary: null });
      return Response.json(page([]));
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <HospitalReceiving scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Inspect TEST-CHALLAN" }));
  expect(await screen.findByLabelText("Physically counted units")).toHaveValue(null);
  await user.type(screen.getByLabelText("Observed lot number"), shipment.lot_number);
  await user.type(screen.getByLabelText("Observed expiry date"), shipment.expires_on);
  await user.type(
    screen.getByLabelText("Inspection and count notes"),
    "TEST no physical count supplied",
  );
  await user.click(screen.getByRole("button", { name: "Record inspected delivery" }));
  expect(await screen.findByText(/expected number/i)).toBeInTheDocument();
  expect(writes).toBe(0);
});
