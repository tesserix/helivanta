import { z } from "zod";
import { apiFetch } from "./client";
import { pharmacyWardIndentInputSchema } from "./pharmacy-contract";
import { PharmacyReceivingProductContract } from "./pharmacy-receiving";
import { HospitalWorkScopeChoiceContract } from "./staff-access-contract";

const id = z.string().guid();
const stamp = z.string().datetime({ offset: true });
const code = z.string().regex(/^[A-Z0-9][A-Z0-9-]{0,19}$/);
const page = z
  .object({ has_more: z.boolean(), next_cursor: z.string().min(1).nullable() })
  .strict();

export const wardIndentLineSchema = z
  .object({
    line_number: z.number().int().min(1),
    product_id: id,
    brand_name: z.string(),
    generic_name: z.string(),
    requested_units: z.number().int().min(1),
    issued_units: z.number().int().min(0),
    returned_units: z.number().int().min(0),
  })
  .strict();

export const wardIndentSchema = z
  .object({
    id,
    outlet_id: id,
    hospital_id: id,
    department_id: id,
    kind: z.literal("patient"),
    ward_code: code,
    room_code: code,
    bed_code: code,
    hospital_name: z.string().min(1),
    department_name: z.string().min(1),
    line_count: z.number().int().min(1),
    status: z.enum(["open", "closed", "cancelled"]),
    version: z.number().int().min(1),
    close_reason: z.string().nullable(),
    raised_at: stamp,
    closed_at: stamp.nullable(),
    lines: z.array(wardIndentLineSchema).max(50).optional(),
    occupancy_id: id.optional(),
  })
  .strict();

export const wardIndentOutletChoiceSchema = z
  .object({
    id,
    pharmacy_id: id,
    pharmacy_name: z.string().min(1),
    name: z.string().min(1),
    code: z.string().min(1),
  })
  .strict();

export const wardIndentOccupantSchema = z
  .object({ occupancy_id: id, ward_code: code, room_code: code, bed_code: code })
  .passthrough();

export type WardIndent = z.infer<typeof wardIndentSchema>;
export type WardIndentLine = z.infer<typeof wardIndentLineSchema>;
export type WardIndentOutletChoice = z.infer<typeof wardIndentOutletChoiceSchema>;
export type WardIndentInput = z.infer<typeof pharmacyWardIndentInputSchema>;
export type WardIndentStatus = WardIndent["status"];

export type WardIndentDepartment = { hospitalId: string; departmentId: string };

export const wardIndentsKey = (...parts: unknown[]): unknown[] => [
  "pharmacy",
  "ward-indents",
  ...parts,
];

const departmentRoot = (d: WardIndentDepartment) =>
  `/pharmacy/hospitals/${id.parse(d.hospitalId)}/departments/${id.parse(d.departmentId)}`;

export const wardIndentIssueUrl = (indent: string) =>
  `/pharmacy/counter?indent=${id.parse(indent)}`;

function pageOf<T extends z.ZodTypeAny>(row: T, max: number) {
  return z.object({ data: z.array(row).max(max), page }).strict();
}

async function list<T extends z.ZodTypeAny>(
  path: string,
  row: T,
  max: number,
  cursor: string | null,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ limit: String(max) });
  if (cursor !== null) query.set("cursor", z.string().min(1).max(2048).parse(cursor));
  const result = pageOf(row, max).parse(
    await apiFetch<unknown>(`${path}${path.includes("?") ? "&" : "?"}${query}`, {
      cache: "no-store",
      signal,
    }),
  );
  if (
    result.page.has_more !== (result.page.next_cursor !== null) ||
    (result.page.has_more && result.page.next_cursor === cursor)
  )
    throw new Error("The ward indent list could not be verified.");
  return result;
}

export const fetchWardIndentOutlets = (
  d: WardIndentDepartment,
  cursor: string | null,
  signal?: AbortSignal,
) =>
  list(
    `${departmentRoot(d)}/ward-indent-outlet-choices`,
    wardIndentOutletChoiceSchema,
    20,
    cursor,
    signal,
  );

export const fetchWardIndents = (
  d: WardIndentDepartment,
  outlet: string,
  cursor: string | null,
  signal?: AbortSignal,
) =>
  list(
    `${departmentRoot(d)}/outlets/${id.parse(outlet)}/ward-indents`,
    wardIndentSchema,
    50,
    cursor,
    signal,
  );

export async function fetchWardIndentOccupant(
  d: WardIndentDepartment,
  outlet: string,
  room: string,
  bed: string,
  signal?: AbortSignal,
) {
  const query = new URLSearchParams({ room_code: code.parse(room), bed_code: code.parse(bed) });
  return wardIndentOccupantSchema.parse(
    await apiFetch<unknown>(
      `${departmentRoot(d)}/outlets/${id.parse(outlet)}/ward-indent-occupant?${query}`,
      { cache: "no-store", signal },
    ),
  );
}

export async function putWardIndent(
  d: WardIndentDepartment,
  outlet: string,
  indent: string,
  input: WardIndentInput,
) {
  const body = pharmacyWardIndentInputSchema.parse(input);
  const result = wardIndentSchema.parse(
    await apiFetch<unknown>(
      `${departmentRoot(d)}/outlets/${id.parse(outlet)}/ward-indents/${id.parse(indent)}`,
      {
        method: "PUT",
        body: JSON.stringify(body),
      },
    ),
  );
  if (result.id !== indent || result.outlet_id !== outlet)
    throw new Error("The receipt did not match the raised indent.");
  return result;
}

export async function cancelWardIndent(d: WardIndentDepartment, outlet: string, indent: string) {
  const result = wardIndentSchema.parse(
    await apiFetch<unknown>(
      `${departmentRoot(d)}/outlets/${id.parse(outlet)}/ward-indents/${id.parse(indent)}/cancellation`,
      { method: "PUT", body: "{}" },
    ),
  );
  if (result.id !== indent) throw new Error("The receipt did not match the cancelled indent.");
  return result;
}

export type PharmacyOutletRoot = { pharmacyId: string; outletId: string };
const outletRoot = (s: PharmacyOutletRoot) =>
  `/pharmacy/pharmacies/${id.parse(s.pharmacyId)}/outlets/${id.parse(s.outletId)}`;

export const fetchFulfilmentIndents = (
  s: PharmacyOutletRoot,
  status: WardIndentStatus,
  cursor: string | null,
  signal?: AbortSignal,
) => list(`${outletRoot(s)}/ward-indents?status=${status}`, wardIndentSchema, 50, cursor, signal);

export async function fetchFulfilmentIndent(
  s: PharmacyOutletRoot,
  indent: string,
  signal?: AbortSignal,
) {
  return wardIndentSchema.parse(
    await apiFetch<unknown>(`${outletRoot(s)}/ward-indents/${id.parse(indent)}`, {
      cache: "no-store",
      signal,
    }),
  );
}

export async function closeWardIndent(
  s: PharmacyOutletRoot,
  indent: string,
  reason: string | null,
) {
  const result = wardIndentSchema.parse(
    await apiFetch<unknown>(`${outletRoot(s)}/ward-indents/${id.parse(indent)}/closure`, {
      method: "PUT",
      body: JSON.stringify(reason === null ? {} : { reason }),
    }),
  );
  if (result.id !== indent || result.status !== "closed")
    throw new Error("The receipt did not match the closed indent.");
  return result;
}

export function wardIndentOutstanding(indent: Pick<WardIndent, "lines">) {
  return (indent.lines ?? []).some((line) => line.issued_units < line.requested_units);
}

export const fetchWardIndentScopes = (cursor: string | null, signal?: AbortSignal) =>
  list("/iam/me/hospital-ward-indent-scopes", HospitalWorkScopeChoiceContract, 20, cursor, signal);

export const fetchWardIndentProducts = (
  d: WardIndentDepartment,
  outlet: string,
  q: string,
  cursor: string | null,
  signal?: AbortSignal,
) =>
  list(
    `${departmentRoot(d)}/outlets/${id.parse(outlet)}/ward-indent-products${q ? `?q=${encodeURIComponent(q)}` : ""}`,
    PharmacyReceivingProductContract,
    50,
    cursor,
    signal,
  );
