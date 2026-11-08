import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch, POLL_INTERVAL_MS } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import {
  SupplierGrantContract,
  SupplierGrantInputContract,
  SupplierGrantDecisionInputContract,
  SupplierGrantHospitalDecisionInputContract,
  SupplierGrantCommandReceiptContract,
  SupplierGrantSummaryContract,
  type SupplierGrantInput,
  type SupplierGrantDecisionInput,
  type SupplierGrantHospitalDecisionInput,
} from "./supplier-access-contract";
export * from "./supplier-access-contract";
export { v7 as newSupplierGrantId } from "uuid";
export type SupplierSourceScope = {
  tenantId: string;
  subject: string;
  product: "supply" | "pharmacy";
  organizationId: string;
  locationId: string;
};
export type SupplierHospitalScope = { tenantId: string; subject: string; hospitalId: string };
const recordId = z
  .uuid()
  .regex(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
const hospitalId = z.uuid();
const sourceBase = (id: string) => `/iam/supplier-grants/${recordId.parse(id)}`;
const homeBase = (scope: SupplierHospitalScope, id?: string) =>
  `/iam/hospitals/${hospitalId.parse(scope.hospitalId)}/supplier-grant-offers${id ? `/${recordId.parse(id)}` : ""}`;
function sourceRecord(scope: SupplierSourceScope, value: unknown, id?: string) {
  const row = SupplierGrantContract.parse(value);
  if (
    row.owner_tenant_id !== scope.tenantId ||
    row.side !== "source" ||
    row.scope.source_tenant_id !== scope.tenantId ||
    row.scope.source_product !== scope.product ||
    row.scope.source_organization_id !== scope.organizationId ||
    row.scope.source_location_id !== scope.locationId ||
    (id !== undefined && row.id !== id)
  )
    throw new Error("Supplier grant belongs to another source, location or scope.");
  return row;
}
function homeRecord(scope: SupplierHospitalScope, value: unknown, id?: string) {
  const row = SupplierGrantContract.parse(value);
  if (
    row.owner_tenant_id !== scope.tenantId ||
    row.side !== "hospital" ||
    row.scope.recipient_tenant_id !== scope.tenantId ||
    row.scope.hospital_id !== scope.hospitalId ||
    (id !== undefined && row.id !== id)
  )
    throw new Error("Supplier offer belongs to another hospital or scope.");
  return row;
}
function instant(value: string) {
  const fraction = value.match(/\.(\d+)/)?.[1] ?? "";
  return (
    BigInt(Math.floor(Date.parse(value) / 1000)) * 1000000n +
    BigInt(fraction.slice(0, 6).padEnd(6, "0"))
  );
}
function intent(input: SupplierGrantInput) {
  return JSON.stringify({
    ...input,
    valid_from: String(instant(input.valid_from)),
    expires_at: String(instant(input.expires_at)),
  });
}
function preparation(
  scope: SupplierSourceScope,
  id: string,
  input: SupplierGrantInput,
  value: unknown,
) {
  const result = sourceRecord(scope, value, id);
  if (result.prepared_by !== scope.subject || intent(result.input) !== intent(input))
    throw new Error("The supplier proposal differs from the original actor or intent.");
  return result;
}
export async function prepareSupplierGrant(
  scope: SupplierSourceScope,
  id: string,
  input: SupplierGrantInput,
) {
  const body = SupplierGrantInputContract.parse(input);
  return preparation(
    scope,
    id,
    body,
    await apiFetch(sourceBase(id), {
      ...membershipRequestOptions(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}
export async function recoverSupplierGrantProposal(
  scope: SupplierSourceScope,
  id: string,
  input: SupplierGrantInput,
) {
  const body = SupplierGrantInputContract.parse(input);
  return preparation(
    scope,
    id,
    body,
    await apiFetch(sourceBase(id), membershipRequestOptions(scope)),
  );
}
export async function getSupplierGrant(
  scope: SupplierSourceScope,
  id: string,
  signal?: AbortSignal,
) {
  return sourceRecord(
    scope,
    await apiFetch(sourceBase(id), { ...membershipRequestOptions(scope), signal }),
    id,
  );
}
function commandResult(
  scope: SupplierSourceScope | SupplierHospitalScope,
  id: string,
  command: string,
  input: SupplierGrantDecisionInput | SupplierGrantHospitalDecisionInput,
  value: unknown,
) {
  const result = SupplierGrantCommandReceiptContract.parse(value);
  const home = "hospitalId" in scope;
  const grant = home ? homeRecord(scope, result.grant, id) : sourceRecord(scope, result.grant, id);
  const status =
    input.operation === "approve"
      ? home
        ? "accepted_pending_ack"
        : "offered"
      : input.operation === "reject"
        ? "rejected"
        : "suspended";
  if (
    result.id !== command ||
    result.grant_id !== id ||
    result.actor_subject !== scope.subject ||
    result.expected_version !== input.expected_version ||
    result.operation !== input.operation ||
    result.reason !== input.reason ||
    grant.version !== input.expected_version + 1 ||
    grant.status !== status
  )
    throw new Error("The retained supplier decision differs from this review intent or account.");
  return result;
}
export async function decideSupplierGrant(
  scope: SupplierSourceScope,
  id: string,
  command: string,
  input: SupplierGrantDecisionInput,
) {
  const body = SupplierGrantDecisionInputContract.parse(input);
  return commandResult(
    scope,
    id,
    command,
    body,
    await apiFetch(`${sourceBase(id)}/commands/${recordId.parse(command)}`, {
      ...membershipRequestOptions(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}
export async function recoverSupplierGrantCommand(
  scope: SupplierSourceScope,
  id: string,
  command: string,
  input: SupplierGrantDecisionInput,
) {
  const body = SupplierGrantDecisionInputContract.parse(input);
  return commandResult(
    scope,
    id,
    command,
    body,
    await apiFetch(
      `${sourceBase(id)}/commands/${recordId.parse(command)}`,
      membershipRequestOptions(scope),
    ),
  );
}
export async function getHospitalSupplierOffer(
  scope: SupplierHospitalScope,
  id: string,
  signal?: AbortSignal,
) {
  return homeRecord(
    scope,
    await apiFetch(homeBase(scope, id), { ...membershipRequestOptions(scope), signal }),
    id,
  );
}
export async function decideHospitalSupplierOffer(
  scope: SupplierHospitalScope,
  id: string,
  command: string,
  input: SupplierGrantHospitalDecisionInput,
) {
  const body = SupplierGrantHospitalDecisionInputContract.parse(input);
  return commandResult(
    scope,
    id,
    command,
    body,
    await apiFetch(`${homeBase(scope, id)}/commands/${recordId.parse(command)}`, {
      ...membershipRequestOptions(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}
export async function recoverHospitalSupplierOfferCommand(
  scope: SupplierHospitalScope,
  id: string,
  command: string,
  input: SupplierGrantHospitalDecisionInput,
) {
  const body = SupplierGrantHospitalDecisionInputContract.parse(input);
  return commandResult(
    scope,
    id,
    command,
    body,
    await apiFetch(
      `${homeBase(scope, id)}/commands/${recordId.parse(command)}`,
      membershipRequestOptions(scope),
    ),
  );
}
export async function getHospitalSupplierGrant(
  scope: SupplierHospitalScope,
  id: string,
  signal?: AbortSignal,
) {
  const row = SupplierGrantSummaryContract.parse(
    await apiFetch(
      `/iam/hospitals/${hospitalId.parse(scope.hospitalId)}/supplier-grants/${recordId.parse(id)}`,
      { ...membershipRequestOptions(scope), signal },
    ),
  );
  if (
    row.id !== id ||
    row.scope.recipient_tenant_id !== scope.tenantId ||
    row.scope.hospital_id !== scope.hospitalId
  )
    throw new Error("Supplier grant belongs to another hospital scope.");
  return row;
}
const pageContract = z.strictObject({
  data: z.array(SupplierGrantContract).max(50),
  page: z.strictObject({
    has_more: z.boolean(),
    next_cursor: z.string().min(1).max(4096).nullable(),
  }),
});
const params = (cursor: string | null) =>
  new URLSearchParams({ limit: "25", ...(cursor ? { cursor } : {}) });
export async function listSourceSupplierGrants(
  scope: SupplierSourceScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const url = `/iam/supplier-sources/${scope.product}/${recordId.parse(scope.organizationId)}/locations/${recordId.parse(scope.locationId)}/grants?${params(cursor)}`;
  const page = pageContract.parse(
    await apiFetch(url, { ...membershipRequestOptions(scope), signal }),
  );
  page.data.forEach((row) => sourceRecord(scope, row));
  return page;
}
export async function listHospitalSupplierOffers(
  scope: SupplierHospitalScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const page = pageContract.parse(
    await apiFetch(`${homeBase(scope)}?${params(cursor)}`, {
      ...membershipRequestOptions(scope),
      signal,
    }),
  );
  page.data.forEach((row) => homeRecord(scope, row));
  return page;
}
export const supplierGrantsKey = (scope: SupplierSourceScope) => [
  "iam",
  "supplier-grants",
  scope.tenantId,
  scope.subject,
  scope.product,
  scope.organizationId,
  scope.locationId,
];
export const supplierOffersKey = (scope: SupplierHospitalScope) => [
  "iam",
  "supplier-offers",
  scope.tenantId,
  scope.subject,
  scope.hospitalId,
];
export function useSourceSupplierGrants(
  scope: SupplierSourceScope | null,
  cursor: string | null = null,
) {
  return useQuery({
    queryKey: scope ? [...supplierGrantsKey(scope), cursor] : ["iam", "supplier-grants", "none"],
    queryFn: ({ signal }) => listSourceSupplierGrants(scope!, cursor, signal),
    enabled: !!scope,
    staleTime: 0,
    refetchInterval: POLL_INTERVAL_MS,
  });
}
export function useHospitalSupplierOffers(
  scope: SupplierHospitalScope | null,
  cursor: string | null = null,
) {
  return useQuery({
    queryKey: scope ? [...supplierOffersKey(scope), cursor] : ["iam", "supplier-offers", "none"],
    queryFn: ({ signal }) => listHospitalSupplierOffers(scope!, cursor, signal),
    enabled: !!scope,
    staleTime: 0,
    refetchInterval: POLL_INTERVAL_MS,
  });
}
