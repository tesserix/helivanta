import { z } from "zod";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import {
  SupplierGrantScopeContract,
  PartnerShipmentOrderContract,
  PartnerShipmentContract,
  type SupplierGrantScope,
  type PartnerShipmentOrder,
} from "./supplier-access-contract";
import type { SupplierHospitalScope } from "./supplier-access";
export type PartnerShipmentScope = SupplierHospitalScope & {
  grantId: string;
  relationshipId: string;
  termsSha256: string;
  source: SupplierGrantScope;
};
const record = z.uuid().refine((value) => value[14] === "7" && value === value.toLowerCase());
const cursor = z.string().min(1).max(2048);
const pageInfo = z.object({ has_more: z.boolean(), next_cursor: cursor.nullable() }).strict();
const orderPage = z
  .object({ data: z.array(PartnerShipmentOrderContract).max(50), page: pageInfo })
  .strict();
const shipmentPage = z
  .object({ data: z.array(PartnerShipmentContract).max(50), page: pageInfo })
  .strict();
function base(scope: PartnerShipmentScope) {
  const source = SupplierGrantScopeContract.parse(scope.source);
  PartnerShipmentOrderContract.shape.terms_sha256.parse(scope.termsSha256);
  record.parse(scope.relationshipId);
  record.parse(source.source_organization_id);
  record.parse(source.source_location_id);
  if (
    source.recipient_tenant_id !== scope.tenantId ||
    source.hospital_id !== scope.hospitalId ||
    source.source_tenant_id === scope.tenantId
  )
    throw new Error("Supplier sharing belongs to another hospital or workspace.");
  return `/supply/hospitals/${z.uuid().parse(scope.hospitalId)}/partner-grants/${source.source_product}/${record.parse(scope.grantId)}/terms/${scope.termsSha256}/shipment-orders`;
}
function request(scope: PartnerShipmentScope, signal?: AbortSignal): RequestInit {
  const deadline = AbortSignal.timeout(15000);
  return {
    ...membershipRequestOptions(scope),
    signal: signal ? AbortSignal.any([signal, deadline]) : deadline,
  };
}
function query(after: string | null) {
  const values = new URLSearchParams({ limit: "50" });
  if (after !== null) values.set("cursor", cursor.parse(after));
  return values;
}
function checkScope(
  scope: PartnerShipmentScope,
  rows: Pick<
    PartnerShipmentOrder,
    | "id"
    | "grant_id"
    | "relationship_id"
    | "terms_sha256"
    | "scope"
    | "source_organization_name"
    | "source_location_name"
  >[],
) {
  const first = rows[0];
  for (const row of rows) {
    record.parse(row.id);
    if (
      row.source_organization_name !== first?.source_organization_name ||
      row.source_location_name !== first?.source_location_name
    )
      throw new Error("The shared shipment page contains inconsistent source names.");
    if (
      row.grant_id !== scope.grantId ||
      row.relationship_id !== scope.relationshipId ||
      row.terms_sha256 !== scope.termsSha256 ||
      Object.entries(scope.source).some(
        ([key, value]) => row.scope[key as keyof SupplierGrantScope] !== value,
      )
    )
      throw new Error("The shared shipment evidence does not match this account, grant or source.");
  }
}
function checkContinuation(
  page: { data: { id: string }[]; page: { has_more: boolean; next_cursor: string | null } },
  after: string | null,
) {
  if (
    page.page.has_more !== Boolean(page.page.next_cursor) ||
    (page.page.has_more && page.data.length === 0) ||
    (page.page.next_cursor !== null && page.page.next_cursor === after) ||
    new Set(page.data.map((row) => row.id.toLowerCase())).size !== page.data.length
  )
    throw new Error("The shared shipment page cannot be continued safely.");
}
export async function listPartnerShipmentOrders(
  scope: PartnerShipmentScope,
  after: string | null = null,
  signal?: AbortSignal,
) {
  const page = orderPage.parse(
    await apiFetch(`${base(scope)}?${query(after)}`, request(scope, signal)),
  );
  checkContinuation(page, after);
  checkScope(scope, page.data);
  return page;
}
export async function listPartnerShipments(
  scope: PartnerShipmentScope,
  order: string,
  after: string | null = null,
  signal?: AbortSignal,
) {
  const page = shipmentPage.parse(
    await apiFetch(
      `${base(scope)}/${record.parse(order)}/shipments?${query(after)}`,
      request(scope, signal),
    ),
  );
  checkContinuation(page, after);
  checkScope(scope, page.data);
  for (const row of page.data)
    if (row.order_id !== order)
      throw new Error("The shared shipment evidence belongs to another order.");
  return page;
}
