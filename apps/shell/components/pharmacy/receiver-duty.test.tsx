import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { chooseOption } from "../../test/select";
import { ReceiverDuty } from "./receiver-duty";

const tenant = "11111111-1111-4111-8111-111111111111";
const subject = "TEST-receiver";
const pharmacyId = "019a55c0-1111-7111-8111-111111111112";
const outletId = "019a55c0-1111-7111-8111-111111111113";
const stock = `/api/v1/pharmacy/pharmacies/${pharmacyId}/operating-locations/${outletId}/stock`;
const created = "2026-11-08T00:00:00Z";
const product = {
  id: "019a55c0-1111-7111-8111-111111111117",
  outlet_id: outletId,
  sku: "TEST-SKU",
  brand_name: "TEST Brand",
  generic_name: "TEST generic",
  dosage_form: "tablet",
  ingredients: [{ name: "TEST", strength: "500", unit: "mg" }],
  pack_size: 10,
  unit_of_issue: "tablet",
  currency: "INR",
  version: 1,
  created_at: created,
};
const address = { zone_code: "", aisle_code: "", rack_code: "", shelf_code: "" };
const quarantine = {
  id: "019a55c0-1111-7111-8111-111111111114",
  outlet_id: outletId,
  code: "TEST-Q",
  name: "TEST Quarantine",
  storage_address: address,
  stock_status: "quarantine",
  created_at: created,
};
const sellable = {
  ...quarantine,
  id: "019a55c0-1111-7111-8111-111111111118",
  code: "TEST-S",
  stock_status: "sellable",
};
const lot = {
  id: "019a55c0-1111-7111-8111-111111111115",
  outlet_id: outletId,
  product_id: product.id,
  lot_number: "TEST-LOT",
  expires_on: "2030-12-31",
  mrp_minor: 12500,
  manufactured_on: null,
  purchase_cost_minor: null,
  product_version: 1,
  created_at: created,
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });

type Write = { method: string; path: string; body: Record<string, unknown> | null };
type Server = {
  products?: unknown[];
  lots?: unknown[];
  receipts?: Response;
  put?: (write: Write) => Response;
};

function serve({ products = [product], lots = [lot], receipts, put }: Server = {}) {
  const writes: Write[] = [];
  const stored = new Map<string, unknown>();
  const fetch = vi.fn<typeof globalThis.fetch>(async (input, init) => {
    const path = String(input);
    const method = init?.method ?? "GET";
    if (path === "/api/v1/iam/me/permissions")
      return Response.json({ tenant_id: tenant, subject, permissions: [] });
    if (!path.startsWith(stock)) return Response.json({ error: "not_found" }, { status: 404 });
    const rest = path.slice(stock.length).split("?")[0]!;
    if (method === "PUT") {
      const write = { method, path: rest, body: JSON.parse(String(init?.body)) };
      writes.push(write);
      const id = rest.split("/")[2]!;
      const row = rest.startsWith("/receipts/")
        ? { ...write.body, id, outlet_id: outletId, actor_subject: subject, created_at: created }
        : {
            manufactured_on: null,
            purchase_cost_minor: null,
            ...write.body,
            id,
            outlet_id: outletId,
            product_version: 1,
            created_at: created,
          };
      stored.set(rest, row);
      return put ? put(write) : Response.json(row);
    }
    writes.push({ method, path: rest, body: null });
    if (rest === "/products") return Response.json(page(products));
    if (rest === "/locations") return Response.json(page([quarantine, sellable]));
    if (rest === "/batches") return Response.json(page(lots));
    if (rest === "/receipts") return receipts ?? Response.json(page([]));
    if (stored.has(rest)) return Response.json(stored.get(rest));
    return Response.json({ error: "not_found" }, { status: 404 });
  });
  vi.stubGlobal("fetch", fetch);
  return { writes, puts: () => writes.filter((write) => write.method === "PUT") };
}
afterEach(() => vi.unstubAllGlobals());

async function fillReceipt(user: ReturnType<typeof userEvent.setup>) {
  await chooseOption(user, await screen.findByRole("combobox", { name: "Product" }), /TEST Brand/);
  await chooseOption(user, await screen.findByRole("combobox", { name: "Lot" }), /TEST-LOT/);
  await user.type(screen.getByLabelText(/Units counted/), "40");
  await user.type(screen.getByLabelText("Source document line"), "TEST-GRN:1");
  await user.type(screen.getByLabelText("Reason"), "TEST supplier delivery");
}
const receiptBody = {
  location_id: quarantine.id,
  batch_id: lot.id,
  kind: "receipt",
  quantity_units: 40,
  source_reference: "TEST-GRN:1",
  reason: "TEST supplier delivery",
};

it("refuses a malformed outlet link without calling the stock API", () => {
  const server = serve();
  renderWithProviders(<ReceiverDuty pharmacyId="not-a-pharmacy" outletId={outletId} />);
  expect(screen.getByRole("alert")).toHaveTextContent("not available to you");
  expect(server.writes).toEqual([]);
});

it("shows loading, then explains that an empty catalogue blocks receiving", async () => {
  serve({ products: [] });
  renderWithProviders(<ReceiverDuty pharmacyId={pharmacyId} outletId={outletId} />);
  expect(screen.getByRole("status")).toHaveTextContent("Loading");
  expect(
    await screen.findByText(/No active catalogue products at this outlet/),
  ).toBeInTheDocument();
  expect(screen.getByText("No receipts recorded at this outlet yet.")).toBeInTheDocument();
});

it("treats a 404 from the outlet as changed access, with no retry offered", async () => {
  serve({ receipts: Response.json({ error: "not_found", message: "gone" }, { status: 404 }) });
  renderWithProviders(<ReceiverDuty pharmacyId={pharmacyId} outletId={outletId} />);
  expect(await screen.findByRole("alert")).toHaveTextContent("not available to you");
  expect(screen.queryByRole("button", { name: /retry/i })).not.toBeInTheDocument();
  expect(screen.queryByRole("combobox", { name: "Product" })).not.toBeInTheDocument();
});

it("records a receipt into the only quarantine location at this exact outlet", async () => {
  const user = userEvent.setup();
  const server = serve();
  renderWithProviders(<ReceiverDuty pharmacyId={pharmacyId} outletId={outletId} />);
  await fillReceipt(user);
  await user.click(screen.getByRole("button", { name: "Record receipt" }));
  expect(await screen.findByText("Receipt recorded in quarantine.")).toBeInTheDocument();
  const [write] = server.puts();
  expect(write?.path).toMatch(/^\/receipts\/[0-9a-f]{8}-[0-9a-f]{4}-7/);
  expect(write?.body).toEqual(receiptBody);
});

it("records the lot on the label with its MRP in minor units", async () => {
  const user = userEvent.setup();
  const server = serve({ lots: [] });
  renderWithProviders(<ReceiverDuty pharmacyId={pharmacyId} outletId={outletId} />);
  await chooseOption(user, await screen.findByRole("combobox", { name: "Product" }), /TEST Brand/);
  await user.type(await screen.findByLabelText("Lot number"), "test-lot");
  await user.type(screen.getByLabelText("Expiry date"), "2030-12-31");
  await user.type(screen.getByLabelText(/MRP printed on the pack/), "125");
  await user.click(screen.getByRole("button", { name: "Record lot" }));
  expect(await screen.findByText(/Lot TEST-LOT recorded/)).toBeInTheDocument();
  const [write] = server.puts();
  expect(write?.path).toMatch(/^\/batches\//);
  expect(write?.body).toEqual({
    product_id: product.id,
    lot_number: "TEST-LOT",
    expires_on: "2030-12-31",
    mrp_minor: 12500,
  });
});

it("recovers a lost receipt answer by reading the same ID, never writing twice", async () => {
  const user = userEvent.setup();
  const server = serve({ put: () => Response.json({ error: "internal" }, { status: 502 }) });
  renderWithProviders(<ReceiverDuty pharmacyId={pharmacyId} outletId={outletId} />);
  await fillReceipt(user);
  await user.click(screen.getByRole("button", { name: "Record receipt" }));
  expect(await screen.findByText(/receipt outcome could not be confirmed/)).toBeInTheDocument();
  expect(screen.getByLabelText(/Units counted/)).toBeDisabled();
  expect(screen.getByRole("combobox", { name: "Product" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Recover receipt" }));
  expect(await screen.findByText("Receipt recorded in quarantine.")).toBeInTheDocument();
  const [write] = server.puts();
  expect(server.puts()).toHaveLength(1);
  expect(
    server.writes.filter((call) => call.path === write?.path).map((call) => call.method),
  ).toEqual(["PUT", "GET"]);
});

it("retries a lost receipt under the same ID", async () => {
  const user = userEvent.setup();
  let answers = 0;
  const server = serve({
    put: (write) =>
      answers++ === 0
        ? Response.json({ error: "internal" }, { status: 502 })
        : Response.json({
            ...write.body,
            id: write.path.split("/")[2],
            outlet_id: outletId,
            actor_subject: subject,
            created_at: created,
          }),
  });
  renderWithProviders(<ReceiverDuty pharmacyId={pharmacyId} outletId={outletId} />);
  await fillReceipt(user);
  await user.click(screen.getByRole("button", { name: "Record receipt" }));
  await user.click(await screen.findByRole("button", { name: "Retry same receipt" }));
  expect(await screen.findByText("Receipt recorded in quarantine.")).toBeInTheDocument();
  const [first, second] = server.puts();
  expect(second?.path).toBe(first?.path);
  expect(second?.body).toEqual(first?.body);
});

it.each([
  [409, "conflict", /cannot change/],
  [400, "invalid_request", /refused as invalid/],
])(
  "releases the ID after a definitive %i refusal so corrected details get a new one",
  async (status, error, text) => {
    const user = userEvent.setup();
    let answers = 0;
    const server = serve({
      put: (write) =>
        answers++ === 0
          ? Response.json({ error, message: "TEST refusal" }, { status })
          : Response.json({
              ...write.body,
              id: write.path.split("/")[2],
              outlet_id: outletId,
              actor_subject: subject,
              created_at: created,
            }),
    });
    renderWithProviders(<ReceiverDuty pharmacyId={pharmacyId} outletId={outletId} />);
    await fillReceipt(user);
    await user.click(screen.getByRole("button", { name: "Record receipt" }));
    expect(await screen.findByText(text)).toBeInTheDocument();
    const units = screen.getByLabelText(/Units counted/);
    expect(units).toBeEnabled();
    await user.clear(units);
    await user.type(units, "39");
    await user.click(screen.getByRole("button", { name: "Record receipt" }));
    expect(await screen.findByText("Receipt recorded in quarantine.")).toBeInTheDocument();
    const [first, second] = server.puts();
    expect(second?.path).not.toBe(first?.path);
    expect(second?.body).toEqual({ ...receiptBody, quantity_units: 39 });
  },
);

it("ends the panel when the duty is withdrawn mid-write", async () => {
  const user = userEvent.setup();
  serve({ put: () => Response.json({ error: "not_found", message: "gone" }, { status: 404 }) });
  renderWithProviders(<ReceiverDuty pharmacyId={pharmacyId} outletId={outletId} />);
  await fillReceipt(user);
  await user.click(screen.getByRole("button", { name: "Record receipt" }));
  await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("not available to you"));
  expect(screen.queryByRole("button", { name: /receipt/ })).not.toBeInTheDocument();
});
