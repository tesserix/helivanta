import { afterEach, expect, it, vi } from "vitest";
import { preparePartnerGrantDraft, recoverPartnerGrantDraft } from "./partner-grants";
import type { PartnerGrantDraftInput } from "./partner-grant-contract";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-owner" };
const id = "019a55c0-1111-7111-8111-111111111111";
const input: PartnerGrantDraftInput = {
  owner_product: "supply",
  owner_organization_id: id,
  source_location_id: id,
  relationship_id: id,
  relationship_version: 1,
  recipient_kind: "group",
  recipient_tenant_id: "22222222-2222-4222-8222-222222222222",
  recipient_hospital_id: "33333333-3333-4333-8333-333333333333",
  recipient_group_id: id,
  template_key: "partner.shipment_reader.v1",
  source_jurisdiction: "IN-MH",
  destination_jurisdiction: "IN-OD",
  valid_from: "2026-11-07T00:00:00.000001Z",
  expires_at: "2026-11-08T00:00:00.000001Z",
  reason: "Synthetic supplier receiving-team access",
};
const draft = {
  id,
  owner_tenant_id: scope.tenantId,
  input,
  status: "pending_verification",
  operating_access_enabled: false,
  prepared_by: scope.subject,
  created_at: "2026-11-07T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());

it("prepares only through Global API with exact owner/account preconditions", async () => {
  const request = vi.fn<typeof fetch>().mockResolvedValue(Response.json(draft));
  vi.stubGlobal("fetch", request);
  expect(await preparePartnerGrantDraft(scope, id, input)).toEqual(draft);
  const [url, init] = request.mock.calls[0]!;
  expect(url).toBe(`/api/v1/iam/partner-grant-drafts/${id}`);
  const headers = new Headers(init?.headers);
  expect(headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it("recovers a lost save by reading the original identity and intent without another write", async () => {
  const request = vi.fn<typeof fetch>().mockResolvedValue(Response.json(draft));
  vi.stubGlobal("fetch", request);
  expect(await recoverPartnerGrantDraft(scope, id, input)).toEqual(draft);
  expect(request.mock.calls).toHaveLength(1);
  expect(request.mock.calls[0]![1]?.method ?? "GET").toBe("GET");
});

it.each(["recipient_tenant_id", "recipient_hospital_id"] as const)(
  "recovers the same %s UUID when the owner returns its canonical lowercase form",
  async (field) => {
    const canonical = "abcdefab-cdef-4abc-8abc-abcdefabcdef";
    const submitted = { ...input, [field]: canonical.toUpperCase() };
    const retained = { ...draft, input: { ...input, [field]: canonical } };
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json(retained)),
    );
    expect(await preparePartnerGrantDraft(scope, id, submitted)).toEqual(retained);
    expect(await recoverPartnerGrantDraft(scope, id, submitted)).toEqual(retained);
  },
);

it.each([
  { owner_tenant_id: "22222222-2222-4222-8222-222222222222" },
  { operating_access_enabled: true },
  { prepared_by: "another-account" },
  { input: { ...input, recipient_hospital_id: "abcdefab-cdef-4abc-8abc-abcdefabcdef" } },
  { input: { ...input, template_key: "partner.invoice_viewer.v1" } },
  { input: { ...input, expires_at: "2026-11-08T00:00:00.000002Z" } },
])("withholds a mismatched or operating recovery response: %j", async (changed) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ ...draft, ...changed })),
  );
  await expect(recoverPartnerGrantDraft(scope, id, input)).rejects.toThrow();
});
