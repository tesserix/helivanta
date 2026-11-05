import { z } from "zod";
import { staffPostingInstantMicros as instant } from "./staff-postings-contract";
import { accountScopedRequest as request } from "./account-scoped-request";
import {
  StaffDepartmentBindingContract,
  StaffDepartmentBindingViewContract,
  StaffDepartmentBindingHistoryEntryContract,
  StaffBindingOutletChoiceContract,
  StaffDepartmentBindingProposalInputContract,
  StaffDepartmentBindingCommandInputContract,
  StaffDepartmentBindingCommandReceiptContract,
  type StaffDepartmentBindingProposalInput,
  type StaffDepartmentBindingCommandInput,
  type StaffDepartmentBindingCommandReceipt,
  type StaffBindingAuthorizationObservation,
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
const invalidIntent = () =>
  new Error("Choose the exact relationship, command and current version.");
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
    await request(scope, root(scope) + "/" + commandId.parse(binding), signal),
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
  page: { page: { has_more: boolean; next_cursor: string | null } },
  cursor: string | null,
  identifiers: string[],
) {
  if (
    page.page.has_more !== (page.page.next_cursor !== null) ||
    (cursor !== null && page.page.next_cursor === cursor) ||
    new Set(identifiers.map((id) => id.toLowerCase())).size !== identifiers.length
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
  const page = bindingPage.parse(await request(scope, root(scope) + query(cursor), signal));
  verifyPage(
    page,
    cursor,
    page.data.map((row) => row.id),
  );
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
    await request(
      scope,
      root(scope) + "/" + commandId.parse(binding) + "/history" + query(cursor),
      signal,
    ),
  );
  verifyPage(
    page,
    cursor,
    page.data.map((row) => row.id),
  );
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
    const disclosurePermission = row.authorizations.find(
      (observation) => observation.permission !== "workforce.staff_actor.use",
    )?.permission;
    const action =
      row.action === "read"
        ? disclosurePermission === "pharmacy.staff_binding.prepare"
          ? "prepare"
          : disclosurePermission === "pharmacy.staff_binding.review"
            ? "review"
            : "read"
        : ["propose", "withdraw"].includes(row.action)
          ? "prepare"
          : "review";
    verifyAuthorizations(row.authorizations, action);
  }
  return page;
}

function verifyAuthorizations(
  observations: StaffBindingAuthorizationObservation[],
  action: "read" | "prepare" | "review",
) {
  const permissions = new Set(["pharmacy.staff_binding." + action, "workforce.staff_actor.use"]);
  if (observations.length !== permissions.size) throw invalid();
  for (const observation of observations) {
    commandId.parse(observation.assignment_id);
    if (!permissions.delete(observation.permission)) throw invalid();
  }
  if (permissions.size !== 0) throw invalid();
}

export type PharmacyStaffBindingAttempt =
  | { kind: "proposal"; bindingId: string; input: StaffDepartmentBindingProposalInput }
  | { kind: "decision"; bindingId: string; input: StaffDepartmentBindingCommandInput };

function normalizeAttempt(attempt: PharmacyStaffBindingAttempt): PharmacyStaffBindingAttempt {
  const bindingId = commandId.parse(attempt.bindingId);
  if (attempt.kind === "proposal") {
    const input = StaffDepartmentBindingProposalInputContract.parse(attempt.input);
    input.department_id = commandId.parse(input.department_id);
    input.department_revision_id = commandId.parse(input.department_revision_id);
    if (input.replaces_binding_id !== null) {
      input.replaces_binding_id = commandId.parse(input.replaces_binding_id);
      if (input.replaces_binding_id === bindingId || input.replaces_binding_version !== 2)
        throw invalidIntent();
    }
    return { kind: "proposal", bindingId, input };
  }
  if (attempt.kind !== "decision") throw invalidIntent();
  const input = StaffDepartmentBindingCommandInputContract.parse(attempt.input);
  input.id = commandId.parse(input.id);
  if (input.id === bindingId || input.expected_version !== (input.operation === "end" ? 2 : 1))
    throw invalidIntent();
  return { kind: "decision", bindingId, input };
}

function verifyReceipt(
  raw: unknown,
  scope: PharmacyStaffBindingScope,
  attempt: PharmacyStaffBindingAttempt,
): StaffDepartmentBindingCommandReceipt {
  const receipt = StaffDepartmentBindingCommandReceiptContract.parse(raw);
  const row = verifyBinding(receipt.binding, scope, attempt.bindingId);
  const proposal = attempt.kind === "proposal";
  const operation = proposal ? "propose" : attempt.input.operation;
  const expectedVersion = proposal ? 0 : attempt.input.expected_version;
  const expectedCommand = proposal ? attempt.bindingId : attempt.input.id;
  const expectedStatus =
    operation === "propose"
      ? "pending"
      : operation === "approve"
        ? "approved"
        : operation === "reject"
          ? "rejected"
          : operation === "withdraw"
            ? "withdrawn"
            : "ended";
  const actorStaff = identifier.parse(receipt.actor_staff_id);
  const replacedId = operation === "approve" ? row.replaces_binding_id : null;
  const replacedVersion = replacedId === null ? null : row.replaces_binding_version! + 1;
  const recorded = instant(receipt.recorded_at),
    created = instant(row.created_at);
  const effectTime =
    operation === "propose"
      ? created
      : operation === "end"
        ? instant(row.ended_at!)
        : instant(row.decided_at!);
  if (
    commandId.parse(receipt.id) !== expectedCommand ||
    commandId.parse(receipt.binding_id) !== attempt.bindingId ||
    receipt.operation !== operation ||
    receipt.expected_version !== expectedVersion ||
    receipt.reason !== attempt.input.reason ||
    receipt.actor_subject !== scope.subject ||
    (receipt.replaced_binding_id === null ? null : commandId.parse(receipt.replaced_binding_id)) !==
      (replacedId === null ? null : commandId.parse(replacedId)) ||
    receipt.replaced_binding_version !== replacedVersion ||
    row.status !== expectedStatus ||
    row.version !== expectedVersion + 1 ||
    recorded === null ||
    created === null ||
    recorded < created ||
    effectTime !== recorded ||
    (proposal &&
      (row.proposed_by_subject !== scope.subject ||
        identifier.parse(row.proposer_staff_id) !== actorStaff)) ||
    (!proposal &&
      operation !== "end" &&
      (row.decided_by_subject !== scope.subject ||
        identifier.parse(row.reviewer_staff_id!) !== actorStaff)) ||
    (!proposal && operation !== "end" && row.decision_reason !== attempt.input.reason)
  )
    throw invalid();
  if (
    proposal &&
    (commandId.parse(row.department_id) !== attempt.input.department_id ||
      commandId.parse(row.department_revision_id) !== attempt.input.department_revision_id ||
      (row.replaces_binding_id === null ? null : commandId.parse(row.replaces_binding_id)) !==
        attempt.input.replaces_binding_id ||
      row.replaces_binding_version !== attempt.input.replaces_binding_version ||
      row.reason !== attempt.input.reason)
  )
    throw invalid();
  if (row.decided_at !== null) {
    const decided = instant(row.decided_at);
    if (decided === null || decided < created || decided > recorded) throw invalid();
  }
  verifyAuthorizations(
    receipt.authorizations,
    ["propose", "withdraw"].includes(operation) ? "prepare" : "review",
  );
  return receipt;
}

export async function preparePharmacyStaffBinding(
  scope: PharmacyStaffBindingScope,
  binding: string,
  input: StaffDepartmentBindingProposalInput,
  signal?: AbortSignal,
) {
  scope = { ...scope };
  const attempt = normalizeAttempt({ kind: "proposal", bindingId: binding, input });
  const raw = await request(scope, root(scope) + "/" + attempt.bindingId, signal, {
    method: "PUT",
    body: attempt.input,
  });
  return verifyReceipt(raw, scope, attempt);
}
export async function decidePharmacyStaffBinding(
  scope: PharmacyStaffBindingScope,
  binding: string,
  input: StaffDepartmentBindingCommandInput,
  signal?: AbortSignal,
) {
  scope = { ...scope };
  const attempt = normalizeAttempt({ kind: "decision", bindingId: binding, input });
  const raw = await request(scope, root(scope) + "/" + attempt.bindingId + "/commands", signal, {
    method: "POST",
    body: attempt.input,
  });
  return verifyReceipt(raw, scope, attempt);
}
export async function recoverPharmacyStaffBindingReceipt(
  scope: PharmacyStaffBindingScope,
  original: PharmacyStaffBindingAttempt,
  signal?: AbortSignal,
) {
  scope = { ...scope };
  const attempt = normalizeAttempt(original);
  const id = attempt.kind === "proposal" ? attempt.bindingId : attempt.input.id;
  const raw = await request(
    scope,
    root(scope) + "/" + attempt.bindingId + "/commands/" + id,
    signal,
  );
  return verifyReceipt(raw, scope, attempt);
}

export type PharmacyStaffBindingHospitalScope = Pick<
  PharmacyStaffBindingScope,
  "tenantId" | "subject" | "hospitalId"
>;
const outletChoicePage = z
  .object({ data: z.array(StaffBindingOutletChoiceContract).max(10), page: pageInfo })
  .strict();
export async function listPharmacyStaffBindingOutletChoices(
  scope: PharmacyStaffBindingHospitalScope,
  purpose: "read" | "prepare" | "review",
  cursor: string | null = null,
  signal?: AbortSignal,
) {
  const hospital = identifier.parse(scope.hospitalId);
  const query = new URLSearchParams({
    purpose: z.enum(["read", "prepare", "review"]).parse(purpose),
    limit: "10",
  });
  if (cursor !== null) query.set("cursor", cursorSchema.parse(cursor));
  const page = outletChoicePage.parse(
    await request(
      scope,
      "/pharmacy/hospitals/" + hospital + "/staff-binding-outlet-choices?" + query,
      signal,
    ),
  );
  verifyPage(
    page,
    cursor,
    page.data.map((row) => row.outlet_id),
  );
  for (const row of page.data) {
    commandId.parse(row.outlet_id);
    commandId.parse(row.pharmacy_id);
  }
  return page;
}
