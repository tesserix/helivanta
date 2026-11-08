import { z } from "zod";
import { apiFetch } from "./client";
import {
  SupplierOrderContract,
  SupplierOrderLineContract,
  SupplierOrderProductContract,
  type SupplierOrderLine,
} from "./supplier-orders-contract";
import { membershipRequestOptions } from "./membership-commands";
import {
  supplierSourceOrdersRoot,
  type SupplierBuyerOrderScope,
  type SupplierSourceOrderScope,
} from "./supplier-orders";
import {
  ReceivingReleaseContract,
  ReceivingReleaseInputContract,
  type ReceivingReleaseInput,
  ReceivingProductChoiceContract,
  ReceivingLocationChoiceContract,
  ReceivingResponseInputContract,
  ReceivingResponseRecordContract,
  ReceivingSharedResponseContract,
  type ReceivingResponseInput,
  type Shipment,
  SupplierStockChoiceContract,
  SupplierStockLocationChoiceContract,
  AllocationContract,
  AllocationViewContract,
  AllocationCommandReceiptContract,
  ReserveInputContract,
  AllocationDecisionInputContract,
  ShipmentContract,
  ReceivingReceiptContract,
  ReceivingInputContract,
  ReceivingSummaryContract,
  ReceivingCorrectionContract,
  ReceivingCorrectionInputContract,
  ReceivingClosureContract,
  ReceivingClosureInputContract,
  ReceivingClearanceInputContract,
  ReceivingClearanceContract,
  type Allocation,
  type AllocationView,
  type ReserveInput,
  type AllocationDecisionInput,
  type ReceivingInput,
  type ReceivingReceipt,
  type ReceivingCorrectionInput,
  type ReceivingClosureInput,
  type ReceivingClearanceInput,
} from "./supplier-fulfilment-contract";

export type SupplierStockScope = SupplierSourceOrderScope;
export type HospitalSupplierReceivingScope = SupplierBuyerOrderScope & { pharmacyId: string };
export type SupplierPageRequest = { limit?: number; cursor?: string | null };
const identifier = AllocationContract.shape.plan.shape.id;
const commandIdentifier = identifier.refine(
  (value) => value[14] === "7" && value === value.toLowerCase(),
);
const same = (a: string, b: string) => a.toLowerCase() === b.toLowerCase();
const fail = (): never => {
  throw new Error("Supplier evidence does not match the selected workspace or frozen command.");
};
const pageMetadata = z
  .object({ has_more: z.boolean(), next_cursor: z.string().min(1).max(4096).nullable() })
  .strict();
const pageRequest = z
  .object({
    limit: z.number().int().min(1).max(50).default(50),
    cursor: z.string().min(1).max(4096).nullish(),
  })
  .strict();
function query(input: SupplierPageRequest) {
  const v = pageRequest.parse(input),
    q = new URLSearchParams({ limit: String(v.limit) });
  if (v.cursor) q.set("cursor", v.cursor);
  return q;
}
function hospitalBase(scope: SupplierBuyerOrderScope) {
  return `/hospitals/${identifier.parse(scope.hospitalId)}/departments/${identifier.parse(scope.departmentId)}/outlets/${identifier.parse(scope.outletId)}`;
}
function stockBase(scope: SupplierStockScope) {
  identifier.parse(scope.tenantId);
  const product = z.enum(["supply", "pharmacy"]).parse(scope.product);
  const organization = identifier.parse(scope.organizationId),
    location = identifier.parse(scope.locationId);
  if (scope.hospitalId !== undefined || scope.departmentId !== undefined) {
    if (product !== "pharmacy") fail();
    return `/pharmacy/hospitals/${identifier.parse(scope.hospitalId)}/departments/${identifier.parse(scope.departmentId)}/pharmacies/${organization}/outlets/${location}`;
  }
  return product === "supply"
    ? `/supply/parties/${organization}/operating-locations/${location}`
    : `/pharmacy/pharmacies/${organization}/outlets/${location}`;
}
function allocationBase(scope: SupplierStockScope, order: string) {
  return `${stockBase(scope)}/supplier-orders/${identifier.parse(order)}/allocations`;
}
function receivingBase(scope: HospitalSupplierReceivingScope, shipment: string) {
  identifier.parse(scope.pharmacyId);
  return `/pharmacy${hospitalBase(scope)}/supplier-shipments/${identifier.parse(shipment)}`;
}
function checkAllocation(scope: SupplierStockScope, order: string, id: string, value: Allocation) {
  const p = value.plan;
  if (
    !same(p.id, id) ||
    !same(p.owner_tenant_id, scope.tenantId) ||
    !same(p.order_id, order) ||
    p.product.owner !== scope.product ||
    !same(p.source_organization_id, scope.organizationId) ||
    !same(p.source_location_id, scope.locationId) ||
    p.input.quantity_units > p.line_quantity_units ||
    value.reserved_units + value.dispatched_units + value.released_units !== p.input.quantity_units
  )
    fail();
  return value;
}
function checkView(scope: SupplierStockScope, order: string, id: string, value: AllocationView) {
  const a = checkAllocation(scope, order, id, value.allocation);
  const expired =
    a.status === "reserved" && Date.parse(value.observed_at) >= Date.parse(a.plan.input.expires_at);
  if (
    value.effective_status !== (expired ? "expired" : a.status) ||
    value.effective_reserved_units !== (expired ? 0 : a.reserved_units)
  )
    fail();
  return value;
}
function sameIntent(schema: z.ZodType, left: unknown, right: unknown) {
  if (JSON.stringify(schema.parse(left)) !== JSON.stringify(schema.parse(right))) fail();
}
function checkReceipt(
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  id: string,
  value: ReceivingReceipt,
) {
  if (
    !same(value.id, id) ||
    !same(value.owner_tenant_id, scope.tenantId) ||
    !same(value.hospital_id, scope.hospitalId) ||
    !same(value.pharmacy_id, scope.pharmacyId) ||
    !same(value.outlet_id, scope.outletId) ||
    !same(value.shipment_id, shipment) ||
    value.input.accepted_units + value.input.rejected_units !== value.input.counted_units ||
    value.cumulative_accepted_units + value.cumulative_rejected_units !==
      value.cumulative_counted_units
  )
    fail();
  return value;
}
async function page<S extends z.ZodType>(
  path: string,
  scope: SupplierBuyerOrderScope | SupplierStockScope,
  contract: S,
  check: (value: z.output<S>) => void,
  request: SupplierPageRequest,
  signal?: AbortSignal,
) {
  const q = query(request);
  const value = z
    .object({ data: z.array(contract).max(Number(q.get("limit"))), page: pageMetadata })
    .strict()
    .parse(await apiFetch(`${path}?${q}`, { ...membershipRequestOptions(scope), signal }));
  if (
    value.page.has_more !== Boolean(value.page.next_cursor) ||
    (value.page.has_more && value.data.length === 0)
  )
    fail();
  for (const row of value.data) check(row);
  const ids = value.data.map((row) =>
    "id" in Object(row)
      ? String((row as { id: string }).id).toLowerCase()
      : String((row as AllocationView).allocation.plan.id).toLowerCase(),
  );
  if (new Set(ids).size !== ids.length) fail();
  return value;
}
export async function listSupplierAllocationsPage(
  scope: SupplierStockScope,
  order: string,
  request: SupplierPageRequest = {},
  signal?: AbortSignal,
) {
  return page(
    allocationBase(scope, order),
    scope,
    AllocationViewContract,
    (v) => {
      checkView(scope, order, v.allocation.plan.id, v);
    },
    request,
    signal,
  );
}
export async function readSupplierAllocation(
  scope: SupplierStockScope,
  order: string,
  id: string,
  signal?: AbortSignal,
) {
  return checkView(
    scope,
    order,
    id,
    AllocationViewContract.parse(
      await apiFetch(`${allocationBase(scope, order)}/${identifier.parse(id)}`, {
        ...membershipRequestOptions(scope),
        signal,
      }),
    ),
  );
}
export async function reserveSupplierAllocation(
  scope: SupplierStockScope,
  order: string,
  id: string,
  input: ReserveInput,
) {
  const intent = reserveIntent(input);
  const value = checkAllocation(
    scope,
    order,
    id,
    AllocationContract.parse(
      await apiFetch(`${allocationBase(scope, order)}/${commandIdentifier.parse(id)}`, {
        ...membershipRequestOptions(scope),
        method: "PUT",
        body: JSON.stringify(intent),
      }),
    ),
  );
  if (
    value.version !== 1 ||
    value.status !== "reserved" ||
    value.dispatched_units !== 0 ||
    value.released_units !== 0 ||
    value.reserved_units !== value.plan.input.quantity_units
  )
    fail();
  if (value.plan.actor_subject !== scope.subject) fail();
  sameReserveIntent(value.plan.input, intent);
  return value;
}
function reserveIntent(input: ReserveInput) {
  const value = ReserveInputContract.parse(input);
  return { ...value, expires_at: value.expires_at.replace(/\.(\d{6})\d+(?=Z|[+-])/, ".$1") };
}
function sameReserveIntent(left: ReserveInput, right: ReserveInput) {
  const instant = (s: string) =>
    [Math.floor(Date.parse(s) / 1000), (/\.(\d+)/.exec(s)?.[1] ?? "").padEnd(9, "0")].join(":");
  if (instant(left.expires_at) !== instant(right.expires_at)) fail();
  sameIntent(ReserveInputContract, { ...left, expires_at: right.expires_at }, right);
}
export async function recoverSupplierAllocation(
  scope: SupplierStockScope,
  order: string,
  id: string,
  input: ReserveInput,
) {
  const intent = reserveIntent(input);
  const value = checkAllocation(
    scope,
    order,
    id,
    AllocationContract.parse(
      await apiFetch(
        `${allocationBase(scope, order)}/${identifier.parse(id)}/creation-receipt`,
        membershipRequestOptions(scope),
      ),
    ),
  );
  if (
    value.version !== 1 ||
    value.status !== "reserved" ||
    value.dispatched_units !== 0 ||
    value.released_units !== 0 ||
    value.reserved_units !== value.plan.input.quantity_units
  )
    fail();
  if (value.plan.actor_subject !== scope.subject) fail();
  sameReserveIntent(value.plan.input, intent);
  return value;
}
async function allocationCommand(
  scope: SupplierStockScope,
  order: string,
  allocation: string,
  command: string,
  input: AllocationDecisionInput,
  submit: boolean,
) {
  const intent = AllocationDecisionInputContract.parse(input);
  const value = AllocationCommandReceiptContract.parse(
    await apiFetch(
      `${allocationBase(scope, order)}/${identifier.parse(allocation)}/commands/${commandIdentifier.parse(command)}`,
      {
        ...membershipRequestOptions(scope),
        ...(submit ? { method: "PUT", body: JSON.stringify(intent) } : {}),
      },
    ),
  );
  checkAllocation(scope, order, allocation, value.allocation);
  if (value.allocation.version !== intent.expected_version + 1) fail();
  if (!same(value.command_id, command) || value.actor_subject !== scope.subject) fail();
  sameIntent(AllocationDecisionInputContract, value.input, intent);
  return value;
}
export const decideSupplierAllocation = (
  scope: SupplierStockScope,
  order: string,
  allocation: string,
  command: string,
  input: AllocationDecisionInput,
) => allocationCommand(scope, order, allocation, command, input, true);
export const recoverSupplierAllocationCommand = (
  scope: SupplierStockScope,
  order: string,
  allocation: string,
  command: string,
  input: AllocationDecisionInput,
) => allocationCommand(scope, order, allocation, command, input, false);
export async function readHospitalSupplierReceipt(
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  id: string,
  signal?: AbortSignal,
) {
  return checkReceipt(
    scope,
    shipment,
    id,
    ReceivingReceiptContract.parse(
      await apiFetch(`${receivingBase(scope, shipment)}/receipts/${identifier.parse(id)}`, {
        ...membershipRequestOptions(scope),
        signal,
      }),
    ),
  );
}
export async function recordHospitalSupplierReceipt(
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  id: string,
  input: ReceivingInput,
) {
  const intent = ReceivingInputContract.parse(input);
  if (intent.accepted_units + intent.rejected_units !== intent.counted_units) fail();
  const value = checkReceipt(
    scope,
    shipment,
    id,
    ReceivingReceiptContract.parse(
      await apiFetch(`${receivingBase(scope, shipment)}/receipts/${commandIdentifier.parse(id)}`, {
        ...membershipRequestOptions(scope),
        method: "PUT",
        body: JSON.stringify(intent),
      }),
    ),
  );
  if (value.actor_subject !== scope.subject) fail();
  sameIntent(ReceivingInputContract, value.input, intent);
  return value;
}
export async function recoverHospitalSupplierReceipt(
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  id: string,
  input: ReceivingInput,
) {
  const value = checkReceipt(
    scope,
    shipment,
    id,
    ReceivingReceiptContract.parse(
      await apiFetch(
        `${receivingBase(scope, shipment)}/receipts/${identifier.parse(id)}`,
        membershipRequestOptions(scope),
      ),
    ),
  );
  if (value.actor_subject !== scope.subject) fail();
  sameIntent(ReceivingInputContract, value.input, input);
  return value;
}
export const listHospitalSupplierReceiptsPage = (
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  request: SupplierPageRequest = {},
  signal?: AbortSignal,
) =>
  page(
    `${receivingBase(scope, shipment)}/receipts`,
    scope,
    ReceivingReceiptContract,
    (v) => {
      checkReceipt(scope, shipment, v.id, v);
    },
    request,
    signal,
  );
export async function readHospitalSupplierReceiving(
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  signal?: AbortSignal,
) {
  const value = ReceivingSummaryContract.parse(
    await apiFetch(`${receivingBase(scope, shipment)}/receiving`, {
      ...membershipRequestOptions(scope),
      signal,
    }),
  );
  if (
    !same(value.owner_tenant_id, scope.tenantId) ||
    !same(value.shipment_id, shipment) ||
    value.counted_units !== value.accepted_units + value.rejected_units
  )
    fail();
  return value;
}
export async function correctHospitalSupplierReceipt(
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  receipt: string,
  command: string,
  input: ReceivingCorrectionInput,
) {
  const intent = ReceivingCorrectionInputContract.parse(input);
  if (intent.accepted_units + intent.rejected_units !== intent.counted_units) fail();
  const value = ReceivingCorrectionContract.parse(
    await apiFetch(
      `${receivingBase(scope, shipment)}/receipts/${identifier.parse(receipt)}/corrections/${commandIdentifier.parse(command)}`,
      { ...membershipRequestOptions(scope), method: "PUT", body: JSON.stringify(intent) },
    ),
  );
  checkCorrection(scope, shipment, receipt, value);
  if (!same(value.id, command) || value.actor_subject !== scope.subject) fail();
  sameIntent(ReceivingCorrectionInputContract, value.input, intent);
  return value;
}
function checkCorrection(
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  receipt: string,
  value: z.output<typeof ReceivingCorrectionContract>,
) {
  if (
    !same(value.owner_tenant_id, scope.tenantId) ||
    !same(value.shipment_id, shipment) ||
    !same(value.receipt_id, receipt) ||
    !same(value.outlet_id, scope.outletId)
  )
    fail();
}
export const listHospitalSupplierCorrectionsPage = (
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  receipt: string,
  request: SupplierPageRequest = {},
  signal?: AbortSignal,
) =>
  page(
    `${receivingBase(scope, shipment)}/receipts/${identifier.parse(receipt)}/corrections`,
    scope,
    ReceivingCorrectionContract,
    (v) => checkCorrection(scope, shipment, receipt, v),
    request,
    signal,
  );
export async function closeHospitalSupplierReceiving(
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  command: string,
  input: ReceivingClosureInput,
) {
  const intent = ReceivingClosureInputContract.parse(input);
  const value = ReceivingClosureContract.parse(
    await apiFetch(
      `${receivingBase(scope, shipment)}/closures/${commandIdentifier.parse(command)}`,
      { ...membershipRequestOptions(scope), method: "PUT", body: JSON.stringify(intent) },
    ),
  );
  if (
    !same(value.id, command) ||
    !same(value.owner_tenant_id, scope.tenantId) ||
    !same(value.shipment_id, shipment) ||
    value.counted_units !== intent.expected_counted_units ||
    value.reason !== intent.reason ||
    value.actor_subject !== scope.subject
  )
    fail();
  return value;
}
export async function recordHospitalSupplierClearance(
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  receipt: string,
  command: string,
  input: ReceivingClearanceInput,
) {
  const intent = ReceivingClearanceInputContract.parse(input);
  const value = ReceivingClearanceContract.parse(
    await apiFetch(
      `${receivingBase(scope, shipment)}/receipts/${identifier.parse(receipt)}/clearances/${commandIdentifier.parse(command)}`,
      { ...membershipRequestOptions(scope), method: "PUT", body: JSON.stringify(intent) },
    ),
  );
  if (
    !same(value.id, command) ||
    !same(value.receipt_id, receipt) ||
    value.summary_version !== intent.expected_summary_version
  )
    fail();
  return value;
}
export const listHospitalSupplierShipmentsPage = (
  scope: HospitalSupplierReceivingScope,
  request: SupplierPageRequest = {},
  signal?: AbortSignal,
) =>
  page(
    `/supply${hospitalBase(scope)}/supplier-shipments`,
    scope,
    ShipmentContract,
    (v) => {
      if (
        !same(v.buyer_tenant_id, scope.tenantId) ||
        !same(v.hospital_id, scope.hospitalId) ||
        !same(v.buyer_organization_id, scope.pharmacyId) ||
        !same(v.buyer_location_id, scope.outletId)
      )
        fail();
    },
    request,
    signal,
  );

function sourceOrderBase(scope: SupplierStockScope) {
  return supplierSourceOrdersRoot(scope);
}

export const listHospitalSupplierOrdersPage = (
  scope: HospitalSupplierReceivingScope,
  request: SupplierPageRequest = {},
  signal?: AbortSignal,
) =>
  page(
    `/supply${hospitalBase(scope)}/purchase-orders`,
    scope,
    SupplierOrderContract,
    (v) => {
      const b = v.terms.buyer;
      if (
        v.side !== "buyer" ||
        !same(v.owner_tenant_id, scope.tenantId) ||
        !same(b.tenant_id, scope.tenantId) ||
        !b.hospital_id ||
        !same(b.hospital_id, scope.hospitalId) ||
        !same(b.organization_id, scope.pharmacyId) ||
        !same(b.location_id, scope.outletId)
      )
        fail();
    },
    request,
    signal,
  );
export const listSourceSupplierOrdersPage = (
  scope: SupplierStockScope,
  request: SupplierPageRequest = {},
  signal?: AbortSignal,
) =>
  page(
    sourceOrderBase(scope),
    scope,
    SupplierOrderContract,
    (v) => {
      const seller = v.terms.seller;
      if (
        v.side !== "seller" ||
        !same(v.owner_tenant_id, scope.tenantId) ||
        !same(seller.tenant_id, scope.tenantId) ||
        seller.product !== scope.product ||
        !same(seller.organization_id, scope.organizationId) ||
        !same(seller.location_id, scope.locationId)
      )
        fail();
      if (
        scope.hospitalId
          ? !seller.hospital_id || !same(seller.hospital_id, scope.hospitalId)
          : seller.hospital_id !== undefined
      )
        fail();
    },
    request,
    signal,
  );
export const listSourceSupplierShipmentsPage = (
  scope: SupplierStockScope,
  order: string,
  request: SupplierPageRequest = {},
  signal?: AbortSignal,
) =>
  page(
    `${sourceOrderBase(scope)}/${identifier.parse(order)}/shipments`,
    scope,
    ShipmentContract,
    (v) => {
      if (
        !same(v.source_tenant_id, scope.tenantId) ||
        !same(v.order_id, order) ||
        v.product.owner !== scope.product ||
        !same(v.source_organization_id, scope.organizationId) ||
        !same(v.source_location_id, scope.locationId)
      )
        fail();
    },
    request,
    signal,
  );

export function supplierStockWorkflowKey(scope: SupplierStockScope, ...parts: string[]) {
  stockBase(scope);
  membershipRequestOptions(scope);
  return [
    "supplier-fulfilment",
    scope.tenantId,
    scope.subject,
    scope.product,
    scope.organizationId,
    scope.locationId,
    scope.hospitalId ?? "",
    scope.departmentId ?? "",
    ...parts,
  ] as const;
}
export function hospitalSupplierReceivingKey(
  scope: HospitalSupplierReceivingScope,
  ...parts: string[]
) {
  hospitalBase(scope);
  identifier.parse(scope.pharmacyId);
  membershipRequestOptions(scope);
  return [
    "supplier-receiving",
    scope.tenantId,
    scope.subject,
    scope.hospitalId,
    scope.departmentId,
    scope.pharmacyId,
    scope.outletId,
    ...parts,
  ] as const;
}

function stockChoicesBase(scope: SupplierStockScope, order: string, line: SupplierOrderLine) {
  const current = SupplierOrderLineContract.parse(line);
  if (current.product.owner !== scope.product) fail();
  return `${stockBase(scope)}/supplier-orders/${identifier.parse(order)}/lines/${identifier.parse(current.id)}/stock-choices`;
}
function checkChoice(
  scope: SupplierStockScope,
  order: string,
  line: SupplierOrderLine,
  value: {
    owner_tenant_id: string;
    order_id: string;
    line_id: string;
    organization_id: string;
    location_id: string;
    available_units: number;
  },
) {
  if (
    !same(value.owner_tenant_id, scope.tenantId) ||
    !same(value.order_id, order) ||
    !same(value.line_id, line.id) ||
    !same(value.organization_id, scope.organizationId) ||
    !same(value.location_id, scope.locationId) ||
    value.available_units < 1
  )
    fail();
}
export const listSupplierStockChoicesPage = async (
  scope: SupplierStockScope,
  order: string,
  line: SupplierOrderLine,
  request: SupplierPageRequest = {},
  signal?: AbortSignal,
) =>
  page(
    stockChoicesBase(scope, order, line),
    scope,
    SupplierStockChoiceContract,
    (value) => {
      checkChoice(scope, order, line, value);
      sameIntent(SupplierOrderProductContract, value.product, line.product);
      if (value.unit_code !== line.unit_code) fail();
    },
    request,
    signal,
  );
export const listSupplierStockLocationsPage = async (
  scope: SupplierStockScope,
  order: string,
  line: SupplierOrderLine,
  stock: string,
  request: SupplierPageRequest = {},
  signal?: AbortSignal,
) => {
  if (scope.product !== "pharmacy") fail();
  return page(
    `${stockChoicesBase(scope, order, line)}/${identifier.parse(stock)}/locations`,
    scope,
    SupplierStockLocationChoiceContract,
    (value) => {
      checkChoice(scope, order, line, value);
      if (!same(value.stock_id, stock)) fail();
    },
    request,
    signal,
  );
};

export { v7 as newSupplierWorkflowId } from "uuid";

function sourceShipment(scope: SupplierStockScope, shipment: Shipment) {
  const value = ShipmentContract.parse(shipment);
  if (
    !same(value.source_tenant_id, scope.tenantId) ||
    value.product.owner !== scope.product ||
    !same(value.source_organization_id, scope.organizationId) ||
    !same(value.source_location_id, scope.locationId)
  )
    fail();
  return value;
}
function buyerShipment(scope: HospitalSupplierReceivingScope, shipment: Shipment) {
  const value = ShipmentContract.parse(shipment);
  if (
    !same(value.buyer_tenant_id, scope.tenantId) ||
    !same(value.hospital_id, scope.hospitalId) ||
    !same(value.buyer_organization_id, scope.pharmacyId) ||
    !same(value.buyer_location_id, scope.outletId)
  )
    fail();
  return value;
}
function checkSupplierResponse(
  shipment: Shipment,
  value: z.output<typeof ReceivingSharedResponseContract>,
) {
  if (
    !same(value.owner_tenant_id, shipment.source_tenant_id) ||
    !same(value.buyer_tenant_id, shipment.buyer_tenant_id) ||
    !same(value.order_id, shipment.order_id) ||
    !same(value.shipment_id, shipment.id) ||
    value.source_product !== shipment.product.owner ||
    !same(value.source_organization_id, shipment.source_organization_id) ||
    !same(value.source_location_id, shipment.source_location_id)
  )
    fail();
}
export async function listHospitalSupplierResponsesPage(
  scope: HospitalSupplierReceivingScope,
  shipment: Shipment,
  request: SupplierPageRequest = {},
  signal?: AbortSignal,
) {
  buyerShipment(scope, shipment);
  return page(
    `/supply${hospitalBase(scope)}/purchase-orders/${shipment.order_id}/shipments/${shipment.id}/receiving-responses`,
    scope,
    ReceivingSharedResponseContract,
    (value) => checkSupplierResponse(shipment, value),
    request,
    signal,
  );
}
export async function listSourceSupplierResponsesPage(
  scope: SupplierStockScope,
  shipment: Shipment,
  request: SupplierPageRequest = {},
  signal?: AbortSignal,
) {
  sourceShipment(scope, shipment);
  return page(
    `${sourceOrderBase(scope)}/${shipment.order_id}/shipments/${shipment.id}/receiving/responses`,
    scope,
    ReceivingResponseRecordContract,
    (value) => checkSupplierResponse(shipment, value),
    request,
    signal,
  );
}
export async function recoverSourceSupplierResponse(
  scope: SupplierStockScope,
  shipment: Shipment,
  id: string,
  input: ReceivingResponseInput,
) {
  sourceShipment(scope, shipment);
  const intent = ReceivingResponseInputContract.parse(input);
  const value = ReceivingResponseRecordContract.parse(
    await apiFetch(
      `${sourceOrderBase(scope)}/${shipment.order_id}/shipments/${shipment.id}/receiving/responses/${identifier.parse(id)}`,
      membershipRequestOptions(scope),
    ),
  );
  checkSupplierResponse(shipment, value);
  if (!same(value.id, id) || value.actor_subject !== scope.subject) fail();
  sameIntent(
    ReceivingResponseInputContract,
    {
      expected_summary_version: value.summary_version,
      response: value.response,
      reason: value.reason,
    },
    intent,
  );
  return value;
}
const supplierResponseAck = z
  .object({
    id: identifier,
    shipment_id: identifier,
    summary_version: z.number().int().min(1).max(Number.MAX_SAFE_INTEGER),
    response: ReceivingResponseInputContract.shape.response,
    financial_effect: z.literal("none"),
  })
  .strict();
export async function recordSourceSupplierResponse(
  scope: SupplierStockScope,
  shipment: Shipment,
  id: string,
  input: ReceivingResponseInput,
) {
  sourceShipment(scope, shipment);
  const intent = ReceivingResponseInputContract.parse(input);
  const value = supplierResponseAck.parse(
    await apiFetch(
      `${sourceOrderBase(scope)}/${shipment.order_id}/shipments/${shipment.id}/receiving/responses/${commandIdentifier.parse(id)}`,
      { ...membershipRequestOptions(scope), method: "PUT", body: JSON.stringify(intent) },
    ),
  );
  if (
    !same(value.id, id) ||
    !same(value.shipment_id, shipment.id) ||
    value.summary_version !== intent.expected_summary_version ||
    value.response !== intent.response
  )
    fail();
  return value;
}
export async function readSourceSupplierReceiving(
  scope: SupplierStockScope,
  shipment: Shipment,
  signal?: AbortSignal,
) {
  sourceShipment(scope, shipment);
  const value = ReceivingSummaryContract.parse(
    await apiFetch(
      `${sourceOrderBase(scope)}/${shipment.order_id}/shipments/${shipment.id}/receiving`,
      { ...membershipRequestOptions(scope), signal },
    ),
  );
  if (
    !same(value.owner_tenant_id, shipment.buyer_tenant_id) ||
    !same(value.order_id, shipment.order_id) ||
    !same(value.shipment_id, shipment.id) ||
    value.counted_units !== value.accepted_units + value.rejected_units ||
    value.counted_units > shipment.quantity_units ||
    (value.closed
      ? value.short_units !== shipment.quantity_units - value.counted_units
      : value.short_units !== 0)
  )
    fail();
  return value;
}

function checkReceivingChoice(
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  value: { owner_tenant_id: string; shipment_id: string; pharmacy_id: string; outlet_id: string },
) {
  if (
    !same(value.owner_tenant_id, scope.tenantId) ||
    !same(value.shipment_id, shipment) ||
    !same(value.pharmacy_id, scope.pharmacyId) ||
    !same(value.outlet_id, scope.outletId)
  )
    fail();
}
export function listHospitalReceivingProductChoicesPage(
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  request: SupplierPageRequest = {},
  signal?: AbortSignal,
) {
  return page(
    `${receivingBase(scope, shipment)}/receiving/product-choices`,
    scope,
    ReceivingProductChoiceContract,
    (v) => checkReceivingChoice(scope, shipment, v),
    request,
    signal,
  );
}
export function listHospitalReceivingLocationsPage(
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  status: "quarantine" | "sellable",
  request: SupplierPageRequest = {},
  signal?: AbortSignal,
) {
  const kind = z.enum(["quarantine", "sellable"]).parse(status);
  return page(
    `${receivingBase(scope, shipment)}/receiving/${kind}-locations`,
    scope,
    ReceivingLocationChoiceContract,
    (v) => {
      checkReceivingChoice(scope, shipment, v);
      if (v.stock_status !== kind) fail();
    },
    request,
    signal,
  );
}

function checkHospitalRelease(
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  receipt: string,
  id: string,
  input: ReceivingReleaseInput,
  raw: unknown,
) {
  const value = ReceivingReleaseContract.parse(raw);
  checkReceivingChoice(scope, shipment, value);
  if (
    !same(value.id, id) ||
    !same(value.receipt_id, receipt) ||
    value.actor_subject !== scope.subject
  )
    fail();
  sameIntent(ReceivingReleaseInputContract, value.input, input);
  return value;
}
export async function releaseHospitalSupplierReceipt(
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  receipt: string,
  id: string,
  input: ReceivingReleaseInput,
) {
  const intent = ReceivingReleaseInputContract.parse(input);
  const value = await apiFetch(
    `${receivingBase(scope, shipment)}/receipts/${identifier.parse(receipt)}/releases/${commandIdentifier.parse(id)}`,
    { ...membershipRequestOptions(scope), method: "PUT", body: JSON.stringify(intent) },
  );
  return checkHospitalRelease(scope, shipment, receipt, id, intent, value);
}
export async function recoverHospitalSupplierRelease(
  scope: HospitalSupplierReceivingScope,
  shipment: string,
  receipt: string,
  id: string,
  input: ReceivingReleaseInput,
) {
  const intent = ReceivingReleaseInputContract.parse(input);
  const value = await apiFetch(
    `${receivingBase(scope, shipment)}/receipts/${identifier.parse(receipt)}/releases/${identifier.parse(id)}`,
    membershipRequestOptions(scope),
  );
  return checkHospitalRelease(scope, shipment, receipt, id, intent, value);
}
