import { z } from "zod";
import { apiFetch } from "./client";
import { membershipRequestOptions } from "./membership-commands";
import {
  SupplierOrderContract,
  SupplierOrderInputContract,
  SupplierOrderDecisionInputContract,
  SupplierOrderCommandReceiptContract,
  type SupplierOrder,
  type SupplierOrderInput,
  type SupplierOrderDecisionInput,
} from "./supplier-orders-contract";

export type SupplierBuyerOrderScope = {
  tenantId: string;
  subject: string;
  hospitalId: string;
  departmentId: string;
  outletId: string;
};
export type SupplierSourceOrderScope = {
  tenantId: string;
  subject: string;
  product: "supply" | "pharmacy";
  organizationId: string;
  locationId: string;
};
const id = z.uuid().refine((value) => value !== "00000000-0000-0000-0000-000000000000");
const record = id.refine(
  (value) => /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(value),
  "Choose an exact record.",
);
function buyerBase(scope: SupplierBuyerOrderScope, order: string) {
  return `/supply/hospitals/${id.parse(scope.hospitalId)}/departments/${record.parse(scope.departmentId)}/outlets/${record.parse(scope.outletId)}/purchase-orders/${record.parse(order)}`;
}
function buyerOwned(scope: SupplierBuyerOrderScope, orderId: string, value: unknown) {
  const row = SupplierOrderContract.parse(value);
  if (
    row.id !== orderId ||
    row.owner_tenant_id !== scope.tenantId ||
    row.side !== "buyer" ||
    row.buyer_department_id !== scope.departmentId ||
    row.terms.buyer.tenant_id !== scope.tenantId ||
    row.terms.buyer.hospital_id !== scope.hospitalId ||
    row.terms.buyer.location_id !== scope.outletId
  )
    throw new Error("The order belongs to another workspace, department or outlet.");
  return row;
}
function sourceOwned(scope: SupplierSourceOrderScope, orderId: string, value: unknown) {
  const row = SupplierOrderContract.parse(value);
  if (
    row.id !== orderId ||
    row.owner_tenant_id !== scope.tenantId ||
    row.side !== "seller" ||
    row.buyer_department_id !== undefined ||
    row.terms.seller.tenant_id !== scope.tenantId ||
    row.terms.seller.product !== scope.product ||
    row.terms.seller.organization_id !== scope.organizationId ||
    row.terms.seller.location_id !== scope.locationId
  )
    throw new Error("The order belongs to another supplier or stock location.");
  return row;
}
function exactPreparation(
  scope: SupplierBuyerOrderScope,
  order: string,
  input: SupplierOrderInput,
  value: unknown,
) {
  const row = buyerOwned(scope, order, value);
  const intent = SupplierOrderInputContract.parse(input);
  if (
    row.status !== "pending_review" ||
    row.version !== 1 ||
    row.prepared_by !== scope.subject ||
    JSON.stringify(row.input) !== JSON.stringify(intent)
  )
    throw new Error("The preparation receipt differs from the original account or frozen intent.");
  return row;
}
export async function prepareSupplierOrder(
  scope: SupplierBuyerOrderScope,
  order: string,
  input: SupplierOrderInput,
) {
  const body = SupplierOrderInputContract.parse(input);
  return exactPreparation(
    scope,
    order,
    body,
    await apiFetch(buyerBase(scope, order), {
      ...membershipRequestOptions(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
}
export async function recoverSupplierOrderProposal(
  scope: SupplierBuyerOrderScope,
  order: string,
  input: SupplierOrderInput,
) {
  return exactPreparation(
    scope,
    order,
    input,
    await apiFetch(`${buyerBase(scope, order)}/proposal-receipt`, membershipRequestOptions(scope)),
  );
}
export async function getSupplierPurchaseOrder(
  scope: SupplierBuyerOrderScope,
  order: string,
  signal?: AbortSignal,
) {
  return buyerOwned(
    scope,
    order,
    await apiFetch(buyerBase(scope, order), { ...membershipRequestOptions(scope), signal }),
  );
}
export async function getSourceSupplierOrder(
  scope: SupplierSourceOrderScope,
  order: string,
  signal?: AbortSignal,
) {
  return sourceOwned(
    scope,
    order,
    await apiFetch(`/supply/supplier-orders/${record.parse(order)}`, {
      ...membershipRequestOptions(scope),
      signal,
    }),
  );
}
function exactCommand(
  scope: { subject: string },
  order: SupplierOrder,
  command: string,
  input: SupplierOrderDecisionInput,
  value: unknown,
) {
  const receipt = SupplierOrderCommandReceiptContract.parse(value);
  if (
    receipt.command_id !== command ||
    receipt.order.id !== order.id ||
    receipt.order.version !== receipt.input.expected_version + 1 ||
    receipt.actor_subject !== scope.subject ||
    JSON.stringify(receipt.input) !==
      JSON.stringify(SupplierOrderDecisionInputContract.parse(input))
  )
    throw new Error("The command receipt differs from the original account, version or decision.");
  return { ...receipt, order };
}
export async function decideSupplierOrder(
  scope: SupplierBuyerOrderScope,
  order: string,
  command: string,
  input: SupplierOrderDecisionInput,
) {
  const body = SupplierOrderDecisionInputContract.parse(input);
  const raw = SupplierOrderCommandReceiptContract.parse(
    await apiFetch(`${buyerBase(scope, order)}/commands/${record.parse(command)}`, {
      ...membershipRequestOptions(scope),
      method: "PUT",
      body: JSON.stringify(body),
    }),
  );
  return exactCommand(scope, buyerOwned(scope, order, raw.order), command, body, raw);
}
export async function recoverSupplierOrderCommand(
  scope: SupplierBuyerOrderScope,
  order: string,
  command: string,
  input: SupplierOrderDecisionInput,
) {
  const raw = SupplierOrderCommandReceiptContract.parse(
    await apiFetch(
      `${buyerBase(scope, order)}/commands/${record.parse(command)}`,
      membershipRequestOptions(scope),
    ),
  );
  return exactCommand(scope, buyerOwned(scope, order, raw.order), command, input, raw);
}
export async function decideSourceSupplierOrder(
  scope: SupplierSourceOrderScope,
  order: string,
  command: string,
  input: SupplierOrderDecisionInput,
) {
  const body = SupplierOrderDecisionInputContract.parse(input);
  if (body.decision === "cancel")
    throw new Error("Supplier cancellation needs stock-owner reconciliation.");
  const raw = SupplierOrderCommandReceiptContract.parse(
    await apiFetch(
      `/supply/supplier-orders/${record.parse(order)}/commands/${record.parse(command)}`,
      { ...membershipRequestOptions(scope), method: "PUT", body: JSON.stringify(body) },
    ),
  );
  return exactCommand(scope, sourceOwned(scope, order, raw.order), command, body, raw);
}
export async function recoverSourceSupplierOrderCommand(
  scope: SupplierSourceOrderScope,
  order: string,
  command: string,
  input: SupplierOrderDecisionInput,
) {
  const raw = SupplierOrderCommandReceiptContract.parse(
    await apiFetch(
      `/supply/supplier-orders/${record.parse(order)}/commands/${record.parse(command)}`,
      membershipRequestOptions(scope),
    ),
  );
  return exactCommand(scope, sourceOwned(scope, order, raw.order), command, input, raw);
}
export const supplierBuyerOrderKey = (scope: SupplierBuyerOrderScope, order: string) => [
  "supplier-purchase-order",
  scope.tenantId,
  scope.subject,
  scope.hospitalId,
  scope.departmentId,
  scope.outletId,
  order,
];
export const supplierSourceOrderKey = (scope: SupplierSourceOrderScope, order: string) => [
  "supplier-source-order",
  scope.tenantId,
  scope.subject,
  scope.product,
  scope.organizationId,
  scope.locationId,
  order,
];
