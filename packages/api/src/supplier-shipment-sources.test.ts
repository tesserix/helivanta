import { afterEach, expect, it, vi } from "vitest";
import * as sdk from "./supplier-shipment-sources";
const id = (n: number) => `0199e000-0000-7000-8000-${n.toString().padStart(12, "0")}`;
const scope = { tenantId: id(1), subject: "TEST-receiver", hospitalId: id(2) };
const grant = {
  id: id(3),
  scope: {
    source_tenant_id: id(4),
    recipient_tenant_id: scope.tenantId,
    hospital_id: scope.hospitalId,
    source_product: "pharmacy",
    source_organization_id: id(5),
    source_location_id: id(6),
  },
  relationship_id: id(7),
  terms_sha256: "a".repeat(64),
  recipient_kind: "subject",
  created_at: "2026-11-22T00:00:00Z",
};
const source = {
  scope: grant.scope,
  grant_id: grant.id,
  relationship_id: grant.relationship_id,
  terms_sha256: grant.terms_sha256,
  source_organization_name: "TEST Independent Pharmacy",
  source_location_name: "TEST Nagpur Branch",
};
const inbox = (rows: unknown[]) => ({ data: rows, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
it("discovers shipment sources through the read-only directory and current source guard", async () => {
  const request = vi
    .fn<typeof fetch>()
    .mockResolvedValueOnce(Response.json(inbox([grant])))
    .mockResolvedValueOnce(Response.json(source));
  vi.stubGlobal("fetch", request);
  const page = await sdk.listPartnerShipmentSourceChoices(scope);
  expect(page.data).toEqual([{ source, recipientKind: "subject" }]);
  expect(request).toHaveBeenCalledTimes(2);
  expect(String(request.mock.calls[0][0])).toContain("/supplier-shipment-grants");
  expect(String(request.mock.calls[1][0])).toContain("/shipment-source");
});

it("keeps a source refused by current grant authority undisclosed", async () => {
  const request = vi
    .fn<typeof fetch>()
    .mockResolvedValueOnce(Response.json(inbox([grant])))
    .mockResolvedValueOnce(
      Response.json({ error: "not_found", message: "shared shipments not found" }, { status: 404 }),
    );
  vi.stubGlobal("fetch", request);
  expect((await sdk.listPartnerShipmentSourceChoices(scope)).data).toEqual([]);
  expect(request).toHaveBeenCalledTimes(2);
});

it("limits independent source checks to five concurrent reads", async () => {
  const controller = new AbortController();
  let entered!: () => void;
  const first = new Promise<void>((resolve) => {
    entered = resolve;
  });
  let active = 0;
  const request = vi.fn<typeof fetch>((url, options) => {
    if (String(url).includes("/supplier-shipment-grants"))
      return Promise.resolve(
        Response.json(inbox(Array.from({ length: 6 }, (_, i) => ({ ...grant, id: id(20 + i) })))),
      );
    active++;
    entered();
    return new Promise<Response>((_resolve, reject) =>
      options?.signal?.addEventListener("abort", () => reject(options.signal?.reason), {
        once: true,
      }),
    );
  });
  vi.stubGlobal("fetch", request);
  const task = sdk
    .listPartnerShipmentSourceChoices(scope, null, controller.signal)
    .catch(() => undefined);
  await first;
  try {
    expect(active).toBe(5);
  } finally {
    controller.abort();
    await task;
  }
});

it.each([401, 409, 503])(
  "does not turn account or dependency failure %i into an empty source list",
  async (status) => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn<typeof fetch>()
        .mockResolvedValueOnce(Response.json(inbox([grant])))
        .mockResolvedValueOnce(
          Response.json({ error: "source_unavailable", message: "TEST refusal" }, { status }),
        ),
    );
    await expect(sdk.listPartnerShipmentSourceChoices(scope)).rejects.toMatchObject({ status });
  },
);
it("does not disguise a missing deployment route as an absent source", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(Response.json(inbox([grant])))
      .mockResolvedValueOnce(new Response("not found", { status: 404 })),
  );
  await expect(sdk.listPartnerShipmentSourceChoices(scope)).rejects.toMatchObject({
    status: 404,
    code: "internal",
  });
});
it("refuses duplicate inbox grants before asking any source for data", async () => {
  const request = vi
    .fn<typeof fetch>()
    .mockResolvedValueOnce(Response.json(inbox([grant, grant])))
    .mockImplementation(async () => Response.json(source));
  vi.stubGlobal("fetch", request);
  await expect(sdk.listPartnerShipmentSourceChoices(scope)).rejects.toThrow();
  expect(request).toHaveBeenCalledTimes(1);
});

it.each([
  { has_more: true, next_cursor: null },
  { has_more: false, next_cursor: "TEST-next" },
  { has_more: true, next_cursor: "TEST-current" },
])("refuses inconsistent or repeating directory continuation %j", async (page) => {
  const request = vi.fn<typeof fetch>().mockResolvedValue(Response.json({ data: [grant], page }));
  vi.stubGlobal("fetch", request);
  await expect(sdk.listPartnerShipmentSourceChoices(scope, "TEST-current")).rejects.toThrow();
  expect(request).toHaveBeenCalledTimes(1);
});
it.each(["recipient_tenant_id", "hospital_id"])(
  "refuses another account or hospital's %s before any source read",
  async (field) => {
    const request = vi
      .fn<typeof fetch>()
      .mockResolvedValue(
        Response.json(inbox([{ ...grant, scope: { ...grant.scope, [field]: id(99) } }])),
      );
    vi.stubGlobal("fetch", request);
    await expect(sdk.listPartnerShipmentSourceChoices(scope)).rejects.toThrow();
    expect(request).toHaveBeenCalledTimes(1);
  },
);
