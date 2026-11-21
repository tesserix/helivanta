import { z } from "zod";
import { apiFetch, ApiError } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import type { SupplierHospitalScope } from "./supplier-access";
import { getPartnerShipmentSource, type PartnerShipmentScope } from "./supplier-partner-shipments";
import {
  SupplierShipmentGrantCandidateContract,
  type PartnerShipmentSource,
  type SupplierShipmentGrantCandidate,
} from "./supplier-access-contract";

export type PartnerShipmentSourceChoice = {
  source: PartnerShipmentSource;
  recipientKind: SupplierShipmentGrantCandidate["recipient_kind"];
};

export async function listPartnerShipmentSourceChoices(
  scope: SupplierHospitalScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ limit: "25", ...(cursor ? { cursor } : {}) });
  const inbox = z
    .strictObject({
      data: z.array(SupplierShipmentGrantCandidateContract).max(50),
      page: z.strictObject({
        has_more: z.boolean(),
        next_cursor: z.string().min(1).max(4096).nullable(),
      }),
    })
    .parse(
      await apiFetch(
        `/iam/hospitals/${z.uuid().parse(scope.hospitalId)}/supplier-shipment-grants?${query}`,
        {
          ...membershipRequestOptions(scope),
          signal,
        },
      ),
    );
  for (const grant of inbox.data) {
    if (
      grant.scope.recipient_tenant_id !== scope.tenantId ||
      grant.scope.hospital_id !== scope.hospitalId
    )
      throw new Error("The supplier directory belongs to another hospital or account.");
  }
  if (
    inbox.page.has_more !== (inbox.page.next_cursor !== null) ||
    (cursor !== null && inbox.page.next_cursor === cursor)
  )
    throw new Error("The supplier directory continuation is invalid.");
  if (new Set(inbox.data.map((grant) => grant.id.toLowerCase())).size !== inbox.data.length)
    throw new Error("The hospital supplier inbox contains duplicate records.");
  const data: PartnerShipmentSourceChoice[] = [];
  const grants = inbox.data;
  for (let offset = 0; offset < grants.length; offset += 5) {
    signal?.throwIfAborted();
    const checked = await Promise.all(
      grants.slice(offset, offset + 5).map(async (grant) => {
        const selected: PartnerShipmentScope = {
          ...scope,
          grantId: grant.id,
          relationshipId: grant.relationship_id,
          termsSha256: grant.terms_sha256,
          source: grant.scope,
        };
        try {
          const source = await getPartnerShipmentSource(selected, signal);
          return { source, recipientKind: grant.recipient_kind };
        } catch (error) {
          if (error instanceof ApiError && error.status === 404 && error.code === "not_found")
            return null;
          throw error;
        }
      }),
    );
    for (const choice of checked) if (choice) data.push(choice);
  }
  return { data, page: inbox.page };
}
