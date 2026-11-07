import { afterEach, expect, it, vi } from "vitest";
import {
  prepareSupplierGrant,
  getSupplierGrant,
  decideSupplierGrant,
  recoverSupplierGrantCommand,
  getHospitalSupplierGrant,
} from "./supplier-access";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-source-admin",
  product: "supply" as const,
  organizationId: "019a55c0-1111-7111-8111-111111111111",
  locationId: "019a55c0-1111-7111-8111-111111111112",
};
const id = "019a55c0-1111-7111-8111-111111111113",
  command = "019a55c0-1111-7111-8111-111111111114";
const home = "22222222-2222-4222-8222-222222222222",
  hospital = "33333333-3333-4333-8333-333333333333";
const input = {
  relationship_id: "019a55c0-1111-7111-8111-111111111115",
  terms_sha256: "a".repeat(64),
  recipient_kind: "group" as const,
  recipient_group_id: "019a55c0-1111-7111-8111-111111111116",
  template_key: "partner.catalogue_viewer.v1" as const,
  valid_from: "2026-11-07T00:00:00Z",
  expires_at: "2026-11-08T00:00:00Z",
  reason: "TEST current hospital procurement team",
};
const row = {
  id,
  owner_tenant_id: scope.tenantId,
  scope: {
    source_tenant_id: scope.tenantId,
    recipient_tenant_id: home,
    hospital_id: hospital,
    source_product: scope.product,
    source_organization_id: scope.organizationId,
    source_location_id: scope.locationId,
  },
  input,
  input_sha256: "b".repeat(64),
  side: "source",
  status: "pending_review",
  version: 1,
  authority_epoch: 1,
  remote_epoch: 0,
  prepared_by: scope.subject,
  created_at: input.valid_from,
  updated_at: input.valid_from,
};
afterEach(() => vi.unstubAllGlobals());
it("binds source preparation and reads to the original actor, owned location and Global API", async () => {
  const request = vi.fn<typeof fetch>().mockResolvedValue(Response.json(row));
  vi.stubGlobal("fetch", request);
  expect(await prepareSupplierGrant(scope, id, input)).toEqual(row);
  const [url, options] = request.mock.calls[0]!;
  expect(String(url)).toBe(`/api/v1/iam/supplier-grants/${id}`);
  expect(options?.method).toBe("PUT");
  expect(new Headers(options?.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(options?.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  request.mockResolvedValue(
    Response.json({
      ...row,
      scope: { ...row.scope, source_location_id: input.recipient_group_id },
    }),
  );
  await expect(getSupplierGrant(scope, id)).rejects.toThrow(/source|location|scope/i);
});
it("recovers a specific actor-bound decision with GET and rejects changed review intent", async () => {
  const decision = {
    expected_version: 1,
    operation: "approve" as const,
    reason: "TEST independent source offer",
  };
  const receipt = {
    id: command,
    grant_id: id,
    actor_subject: scope.subject,
    ...decision,
    grant: { ...row, status: "offered", version: 2, authority_epoch: 2, signed_by: scope.subject },
    created_at: input.valid_from,
  };
  const request = vi.fn<typeof fetch>().mockImplementation(async () => Response.json(receipt));
  vi.stubGlobal("fetch", request);
  expect(await decideSupplierGrant(scope, id, command, decision)).toEqual(receipt);
  expect(await recoverSupplierGrantCommand(scope, id, command, decision)).toEqual(receipt);
  expect(request.mock.calls[1]![1]?.method ?? "GET").toBe("GET");
  expect(request.mock.calls[1]![1]?.body).toBeUndefined();
  request.mockImplementation(async () => Response.json({ ...receipt, reason: "another decision" }));
  await expect(recoverSupplierGrantCommand(scope, id, command, decision)).rejects.toThrow(
    /decision|review|intent/i,
  );
});
it("keeps hospital consent and source access eligibility separate and rejects another hospital", async () => {
  const hospitalScope = { tenantId: home, subject: "TEST-recipient", hospitalId: hospital };
  const summary = {
    id,
    scope: row.scope,
    input,
    consent_status: "approved",
    version: 3,
    source_access_state: "not_evaluated",
    created_at: input.valid_from,
    updated_at: input.valid_from,
  };
  const request = vi.fn<typeof fetch>().mockImplementation(async () => Response.json(summary));
  vi.stubGlobal("fetch", request);
  expect(await getHospitalSupplierGrant(hospitalScope, id)).toEqual(summary);
  expect(String(request.mock.calls[0]![0])).toBe(
    `/api/v1/iam/hospitals/${hospital}/supplier-grants/${id}`,
  );
  request.mockImplementation(async () =>
    Response.json({ ...summary, scope: { ...row.scope, hospital_id: scope.tenantId } }),
  );
  await expect(getHospitalSupplierGrant(hospitalScope, id)).rejects.toThrow(/hospital|scope/i);
});
