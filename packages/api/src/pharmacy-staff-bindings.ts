import { z } from "zod";
import { apiFetch } from "./client";
import { enrollmentRequestOptions } from "./enrollment";
import { onTenantTransition, tenantTransitionPending } from "./tenant-transition";
import {
  StaffDepartmentBindingContract,
  StaffDepartmentBindingViewContract,
  StaffDepartmentBindingHistoryEntryContract,
  type StaffDepartmentBinding,
} from "./pharmacy-staff-binding-contract";

export type PharmacyStaffBindingScope = {
  tenantId: string;
  subject: string;
  hospitalId: string;
  pharmacyId: string;
  outletId: string;
};
const identifier = z
  .string()
  .guid()
  .refine((value) => value !== "00000000-0000-0000-0000-000000000000")
  .transform((value) => value.toLowerCase());
const commandId = z.uuidv7().transform((value) => value.toLowerCase());
const subject = z
  .string()
  .min(1)
  .max(200)
  .refine((value) => value === value.trim() && !/\p{Cc}/u.test(value));
const invalid = () => new Error("The Pharmacy relationship response could not be verified.");

function root(scope: PharmacyStaffBindingScope) {
  return (
    "/pharmacy/pharmacies/" +
    commandId.parse(scope.pharmacyId) +
    "/outlets/" +
    commandId.parse(scope.outletId) +
    "/hospitals/" +
    identifier.parse(scope.hospitalId) +
    "/staff-department-bindings"
  );
}
async function read(scope: PharmacyStaffBindingScope, path: string, signal?: AbortSignal) {
  const tenantId = identifier.parse(scope.tenantId);
  subject.parse(scope.subject);
  signal?.throwIfAborted();
  if (tenantTransitionPending()) throw new Error("Wait for your workspace to finish switching.");
  const transition = new AbortController();
  const combined = signal ? AbortSignal.any([transition.signal, signal]) : transition.signal;
  const unsubscribe = onTenantTransition(() => {
    if (tenantTransitionPending()) transition.abort();
  });
  try {
    const raw = await apiFetch(path, {
      ...enrollmentRequestOptions({ tenantId, subject: scope.subject }),
      signal: combined,
    });
    combined.throwIfAborted();
    return raw;
  } finally {
    unsubscribe();
  }
}

function verifyBinding(
  raw: unknown,
  scope: PharmacyStaffBindingScope,
  expectedId?: string,
): StaffDepartmentBinding {
  const parsed = StaffDepartmentBindingContract.safeParse(raw);
  if (!parsed.success) throw invalid();
  const row = parsed.data;
  for (const id of [
    row.id,
    row.pharmacy_id,
    row.outlet_id,
    row.department_id,
    row.department_revision_id,
  ])
    commandId.parse(id);
  identifier.parse(row.proposer_staff_id);
  if (row.reviewer_staff_id !== null) identifier.parse(row.reviewer_staff_id);
  if (row.replaces_binding_id !== null) commandId.parse(row.replaces_binding_id);
  subject.parse(row.proposed_by_subject);
  if (row.decided_by_subject !== null) subject.parse(row.decided_by_subject);
  const pending = row.status === "pending";
  const ended = row.status === "ended";
  const withdrawn = row.status === "withdrawn";
  if (
    identifier.parse(row.pharmacy_id) !== commandId.parse(scope.pharmacyId) ||
    identifier.parse(row.outlet_id) !== commandId.parse(scope.outletId) ||
    identifier.parse(row.hospital_id) !== identifier.parse(scope.hospitalId) ||
    (expectedId !== undefined && commandId.parse(row.id) !== commandId.parse(expectedId)) ||
    (row.replaces_binding_id === null) !== (row.replaces_binding_version === null) ||
    row.replaces_binding_id === row.id ||
    row.version !== (pending ? 1 : ended ? 3 : 2) ||
    (pending
      ? [row.decided_by_subject, row.reviewer_staff_id, row.decision_reason, row.decided_at].some(
          (value) => value !== null,
        )
      : [row.decided_by_subject, row.reviewer_staff_id, row.decision_reason, row.decided_at].some(
          (value) => value === null,
        )) ||
    ended !== (row.ended_at !== null) ||
    (!pending &&
      (withdrawn
        ? row.decided_by_subject !== row.proposed_by_subject ||
          row.reviewer_staff_id !== row.proposer_staff_id
        : row.decided_by_subject === row.proposed_by_subject ||
          row.reviewer_staff_id === row.proposer_staff_id))
  )
    throw invalid();
  return row;
}

export async function getPharmacyStaffBinding(
  scope: PharmacyStaffBindingScope,
  binding: string,
  signal?: AbortSignal,
) {
  const view = StaffDepartmentBindingViewContract.parse(
    await read(scope, root(scope) + "/" + commandId.parse(binding), signal),
  );
  verifyBinding(view.binding, scope, binding);
  if ((view.binding.status === "approved") === (view.source_state === "not_applicable"))
    throw invalid();
  return view;
}

const cursorSchema = z.string().min(1).max(2048);
const pageInfo = z.object({ has_more: z.boolean(), next_cursor: cursorSchema.nullable() }).strict();
const bindingPage = z
  .object({ data: z.array(StaffDepartmentBindingContract).max(50), page: pageInfo })
  .strict();
const historyPage = z
  .object({ data: z.array(StaffDepartmentBindingHistoryEntryContract).max(50), page: pageInfo })
  .strict();

function verifyPage(
  page: { data: { id: string }[]; page: { has_more: boolean; next_cursor: string | null } },
  cursor: string | null,
) {
  if (
    page.page.has_more !== (page.page.next_cursor !== null) ||
    (cursor !== null && page.page.next_cursor === cursor) ||
    new Set(page.data.map((row) => row.id.toLowerCase())).size !== page.data.length
  )
    throw invalid();
}

function query(cursor: string | null) {
  const params = new URLSearchParams({ limit: "50" });
  if (cursor !== null) params.set("cursor", cursorSchema.parse(cursor));
  return "?" + params;
}
export async function listPharmacyStaffBindings(
  scope: PharmacyStaffBindingScope,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const page = bindingPage.parse(await read(scope, root(scope) + query(cursor), signal));
  verifyPage(page, cursor);
  page.data.forEach((row) => verifyBinding(row, scope));
  return page;
}
export async function listPharmacyStaffBindingHistory(
  scope: PharmacyStaffBindingScope,
  binding: string,
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const page = historyPage.parse(
    await read(
      scope,
      root(scope) + "/" + commandId.parse(binding) + "/history" + query(cursor),
      signal,
    ),
  );
  verifyPage(page, cursor);
  for (const row of page.data) {
    commandId.parse(row.id);
    subject.parse(row.actor_subject);
    if (
      commandId.parse(row.binding_id) !== commandId.parse(binding) ||
      commandId.parse(row.outlet_id) !== commandId.parse(scope.outletId) ||
      identifier.parse(row.hospital_id) !== identifier.parse(scope.hospitalId) ||
      (row.related_binding_id === null) !== (row.related_binding_version === null) ||
      row.related_binding_id === row.binding_id ||
      (row.action !== "read" && row.command_id === null) ||
      (row.action === "propose" &&
        (row.command_id === null ||
          commandId.parse(row.command_id) !== commandId.parse(binding) ||
          row.recorded_version !== 1)) ||
      (["approve", "reject", "withdraw"].includes(row.action) && row.recorded_version !== 2) ||
      (row.action === "end" && row.recorded_version !== 3) ||
      row.recorded_version > 3 ||
      (row.action !== "read" && row.source_state !== null)
    )
      throw invalid();
    if (row.command_id !== null) commandId.parse(row.command_id);
    if (row.related_binding_id !== null) commandId.parse(row.related_binding_id);
    const action =
      row.action === "read"
        ? "read"
        : ["propose", "withdraw"].includes(row.action)
          ? "prepare"
          : "review";
    const permissions = new Set(["pharmacy.staff_binding." + action, "workforce.staff_actor.use"]);
    if (row.authorizations.length !== permissions.size) throw invalid();
    for (const observation of row.authorizations) {
      commandId.parse(observation.assignment_id);
      if (!permissions.delete(observation.permission)) throw invalid();
    }
    if (permissions.size !== 0) throw invalid();
  }
  return page;
}
