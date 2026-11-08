import { z } from "zod";
import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import { wholesaleStockKey, type WholesaleStockScope } from "./supply-stock";
import {
  WholesaleQualityDecisionContract,
  WholesaleQualityInputContract,
  WholesaleIdentityInputContract,
  WholesaleIdentityCheckContract,
  WholesaleStockInspectionContract,
  type WholesaleQualityInput,
  type WholesaleIdentityInput,
} from "./supply-stock-contract";

const id = z.uuid().regex(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
const base = (scope: WholesaleStockScope, receipt: string) =>
  `/supply/parties/${id.parse(scope.partyId)}/operating-locations/${id.parse(scope.premiseId)}/stock/receipts/${id.parse(receipt)}`;
function owned(
  scope: WholesaleStockScope,
  receipt: string,
  value: { owner_tenant_id: string; party_id: string; premise_id: string; receipt_id: string },
) {
  if (
    value.owner_tenant_id.toLowerCase() !== scope.tenantId.toLowerCase() ||
    value.party_id !== scope.partyId ||
    value.premise_id !== scope.premiseId ||
    value.receipt_id !== receipt
  )
    throw new Error("The stock evidence belongs to another workspace or physical receipt.");
}
function quality(value: unknown) {
  const row = WholesaleQualityDecisionContract.parse(value);
  if (
    row.version !== row.input.expected_version + 1 ||
    row.status !== (row.input.decision === "release" ? "released" : "held") ||
    (row.status === "held" ? row.released_base_units !== 0 : row.released_base_units < 1)
  )
    throw new Error("The quality decision is inconsistent.");
  return row;
}
function exactQuality(
  scope: WholesaleStockScope,
  receipt: string,
  command: string,
  input: WholesaleQualityInput,
  value: unknown,
) {
  const row = quality(value);
  owned(scope, receipt, row);
  if (
    row.id !== command ||
    row.actor_subject !== scope.subject ||
    JSON.stringify(row.input) !== JSON.stringify(WholesaleQualityInputContract.parse(input))
  )
    throw new Error("The quality receipt differs from the original account or intent.");
  return row;
}
export async function recordWholesaleQuality(
  scope: WholesaleStockScope,
  receipt: string,
  command: string,
  input: WholesaleQualityInput,
) {
  const body = WholesaleQualityInputContract.parse(input);
  id.parse(body.identity_check_id);
  return exactQuality(
    scope,
    receipt,
    command,
    body,
    await apiFetch(`${base(scope, receipt)}/quality-decisions/${id.parse(command)}`, {
      ...membershipRequestOptions(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}
export async function recoverWholesaleQuality(
  scope: WholesaleStockScope,
  receipt: string,
  command: string,
  input: WholesaleQualityInput,
) {
  return exactQuality(
    scope,
    receipt,
    command,
    input,
    await apiFetch(
      `${base(scope, receipt)}/quality-decisions/${id.parse(command)}`,
      membershipRequestOptions(scope),
    ),
  );
}
function identity(value: unknown) {
  const row = WholesaleIdentityCheckContract.parse(value);
  if (
    row.version !== row.input.expected_version + 1 ||
    row.source.version_id !== row.input.product_version_id ||
    row.source.pack_size !== row.input.expected_pack_size ||
    row.quantity_base_units !== row.input.expected_base_units
  )
    throw new Error("The product count evidence is inconsistent.");
  return row;
}
export async function recordWholesaleIdentity(
  scope: WholesaleStockScope,
  receipt: string,
  command: string,
  input: WholesaleIdentityInput,
) {
  const body = WholesaleIdentityInputContract.parse(input);
  const row = identity(
    await apiFetch(`${base(scope, receipt)}/identity-checks/${id.parse(command)}`, {
      ...membershipRequestOptions(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  return exactIdentity(scope, receipt, command, body, row);
}
function exactIdentity(
  scope: WholesaleStockScope,
  receipt: string,
  command: string,
  input: WholesaleIdentityInput,
  row: ReturnType<typeof identity>,
) {
  owned(scope, receipt, row);
  if (
    row.id !== command ||
    row.actor_subject !== scope.subject ||
    JSON.stringify(row.input) !== JSON.stringify(WholesaleIdentityInputContract.parse(input))
  )
    throw new Error("The count differs from the original account or intent.");
  return row;
}
export async function recoverWholesaleIdentity(
  scope: WholesaleStockScope,
  receipt: string,
  command: string,
  input: WholesaleIdentityInput,
) {
  return exactIdentity(
    scope,
    receipt,
    command,
    input,
    identity(
      await apiFetch(
        `${base(scope, receipt)}/identity-checks/${id.parse(command)}`,
        membershipRequestOptions(scope),
      ),
    ),
  );
}
export async function getWholesaleInspection(
  scope: WholesaleStockScope,
  receipt: string,
  signal?: AbortSignal,
) {
  const row = WholesaleStockInspectionContract.parse(
    await apiFetch(`${base(scope, receipt)}/inspection`, {
      ...membershipRequestOptions(scope),
      signal,
    }),
  );
  owned(scope, receipt, row);
  if (row.identity) {
    identity(row.identity);
    owned(scope, receipt, row.identity);
  }
  if (row.quality) {
    quality(row.quality);
    owned(scope, receipt, row.quality);
  }
  return row;
}
export const wholesaleInspectionKey = (scope: WholesaleStockScope, receipt: string) => [
  ...wholesaleStockKey(scope),
  "inspection",
  receipt,
];
export function useWholesaleInspection(
  scope: WholesaleStockScope,
  receipt: string,
  enabled: boolean,
) {
  return useQuery({
    queryKey: wholesaleInspectionKey(scope, receipt),
    enabled,
    gcTime: 0,
    staleTime: 0,
    retry: false,
    refetchOnWindowFocus: true,
    queryFn: ({ signal }) => getWholesaleInspection(scope, receipt, signal),
  });
}
