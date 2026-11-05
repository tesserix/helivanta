import { afterEach, expect, it, vi } from "vitest";
import {
  preparePharmacyStaffBinding,
  decidePharmacyStaffBinding,
  recoverPharmacyStaffBindingReceipt,
} from "./pharmacy-staff-bindings";
import type { StaffDepartmentBindingCommandInput } from "./pharmacy-staff-binding-contract";
import { beginTenantTransition, cancelTenantTransition } from "./tenant-transition";

const id = (n: number) => "019a0000-0000-7000-8000-" + String(n).padStart(12, "0");
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-preparer",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  pharmacyId: id(1),
  outletId: id(2),
};
const reviewer = { ...scope, subject: "synthetic-reviewer" };
const input = {
  department_id: id(4),
  department_revision_id: id(5),
  replaces_binding_id: null,
  replaces_binding_version: null,
  reason: "Synthetic relationship proposal",
};
const binding = {
  ...input,
  id: id(3),
  pharmacy_id: scope.pharmacyId,
  outlet_id: scope.outletId,
  hospital_id: scope.hospitalId,
  status: "pending",
  version: 1,
  proposed_by_subject: scope.subject,
  proposer_staff_id: id(6),
  created_at: "2026-11-05T00:00:00.123456Z",
  decided_by_subject: null,
  reviewer_staff_id: null,
  decision_reason: null,
  decided_at: null,
  ended_at: null,
};
const authorizations = (action: string) =>
  ["pharmacy.staff_binding." + action, "workforce.staff_actor.use"].map((permission) => ({
    permission,
    assignment_id: id(8),
    assignment_version: 2,
    scope_epoch: 1,
    membership_epoch: 1,
  }));
const proposal = {
  id: binding.id,
  binding_id: binding.id,
  operation: "propose",
  expected_version: 0,
  reason: input.reason,
  replaced_binding_id: null,
  replaced_binding_version: null,
  actor_subject: scope.subject,
  actor_staff_id: binding.proposer_staff_id,
  recorded_at: binding.created_at,
  binding,
  authorizations: authorizations("prepare"),
};
const command = (
  operation: StaffDepartmentBindingCommandInput["operation"],
): StaffDepartmentBindingCommandInput => ({
  id: id(9),
  expected_version: operation === "end" ? 2 : 1,
  operation,
  reason: "Synthetic independent command",
  attest_department_responsibility: operation === "approve",
});
const receipt = (operation: StaffDepartmentBindingCommandInput["operation"]) => {
  const request = command(operation),
    withdraw = operation === "withdraw",
    end = operation === "end";
  const at = "2026-11-05T01:00:00.123456Z";
  const decided = {
    ...binding,
    status:
      operation === "approve"
        ? "approved"
        : operation === "reject"
          ? "rejected"
          : withdraw
            ? "withdrawn"
            : "ended",
    version: request.expected_version + 1,
    decided_by_subject: withdraw ? scope.subject : reviewer.subject,
    reviewer_staff_id: withdraw ? binding.proposer_staff_id : id(7),
    decision_reason: end ? "Earlier independent approval" : request.reason,
    decided_at: end ? "2026-11-05T00:30:00Z" : at,
    ended_at: end ? at : null,
  };
  return {
    id: request.id,
    binding_id: binding.id,
    operation,
    expected_version: request.expected_version,
    reason: request.reason,
    replaced_binding_id: null,
    replaced_binding_version: null,
    actor_subject: withdraw ? scope.subject : reviewer.subject,
    actor_staff_id: withdraw ? binding.proposer_staff_id : id(7),
    recorded_at: at,
    binding: decided,
    authorizations: authorizations(withdraw ? "prepare" : "review"),
  };
};
afterEach(() => {
  vi.unstubAllGlobals();
  cancelTenantTransition();
});

it("prepares one immutable proposal through Global API with current account intent", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json(proposal));
  vi.stubGlobal("fetch", fetch);
  expect(
    await preparePharmacyStaffBinding(scope, binding.id, {
      ...input,
      reason: " " + input.reason + " ",
    }),
  ).toEqual(proposal);
  const [url, options] = fetch.mock.calls[0]!;
  expect(url).toBe(
    "/api/v1/pharmacy/pharmacies/" +
      scope.pharmacyId +
      "/outlets/" +
      scope.outletId +
      "/hospitals/" +
      scope.hospitalId +
      "/staff-department-bindings/" +
      binding.id,
  );
  expect(options.method).toBe("PUT");
  expect(options.body).toBe(JSON.stringify(input));
  expect(options.cache).toBe("no-store");
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it.each(["approve", "reject", "withdraw", "end"] as const)(
  "verifies an exact %s command receipt",
  async (operation) => {
    const response = receipt(operation),
      request = command(operation);
    const fetch = vi.fn().mockResolvedValue(Response.json(response));
    vi.stubGlobal("fetch", fetch);
    expect(
      await decidePharmacyStaffBinding(
        operation === "withdraw" ? scope : reviewer,
        binding.id,
        request,
      ),
    ).toEqual(response);
    const [url, options] = fetch.mock.calls[0]!;
    expect(url).toContain("/staff-department-bindings/" + binding.id + "/commands");
    expect(options.method).toBe("POST");
    expect(options.body).toBe(JSON.stringify(request));
  },
);

it("recovers its original proposal receipt under the exact attempted intent", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json(proposal));
  vi.stubGlobal("fetch", fetch);
  expect(
    await recoverPharmacyStaffBindingReceipt(scope, {
      kind: "proposal",
      bindingId: binding.id,
      input,
    }),
  ).toEqual(proposal);
  const [url, options] = fetch.mock.calls[0]!;
  expect(url).toContain("/commands/" + binding.id);
  expect(options.method ?? "GET").toBe("GET");
  expect(options.body).toBeUndefined();
});

it("recovers its original independent approval without resubmitting it", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json(receipt("approve")));
  vi.stubGlobal("fetch", fetch);
  expect(
    await recoverPharmacyStaffBindingReceipt(reviewer, {
      kind: "decision",
      bindingId: binding.id,
      input: command("approve"),
    }),
  ).toEqual(receipt("approve"));
  expect(fetch).toHaveBeenCalledOnce();
});

it.each([
  { id: id(10) },
  { binding_id: id(10) },
  { operation: "reject" },
  { expected_version: 0 },
  { reason: "A different private decision" },
  { attest_department_responsibility: false },
  { actor_subject: scope.subject },
  { actor_staff_id: id(10) },
  { recorded_at: "2026-11-05T01:00:00.123457Z" },
  { authorizations: authorizations("prepare") },
  { authorizations: [authorizations("review")[0], authorizations("review")[0]] },
])("withholds a mismatched approval receipt: %j", async (change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(Response.json({ ...receipt("approve"), ...change })),
  );
  await expect(
    decidePharmacyStaffBinding(reviewer, binding.id, command("approve")),
  ).rejects.toThrow();
});
it.each([
  { outlet_id: id(10) },
  { hospital_id: id(10) },
  { pharmacy_id: id(10) },
  { proposed_by_subject: reviewer.subject },
  { proposer_staff_id: id(7) },
  { decision_reason: "A different decision" },
  { version: 1 },
  { decided_at: "2026-11-04T00:00:00Z" },
])("withholds a scope, source or decision mismatch: %j", async (change) => {
  const response = receipt("approve");
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        Response.json({ ...response, binding: { ...response.binding, ...change } }),
      ),
  );
  await expect(
    decidePharmacyStaffBinding(reviewer, binding.id, command("approve")),
  ).rejects.toThrow();
});
it.each([
  { department_id: id(10) },
  { department_revision_id: id(10) },
  { replaces_binding_id: id(10), replaces_binding_version: 2 },
  { reason: "Different preparation" },
])("rejects recovery of a proposal with changed intent: %j", async (change) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(proposal)));
  await expect(
    recoverPharmacyStaffBindingReceipt(scope, {
      kind: "proposal",
      bindingId: binding.id,
      input: { ...input, ...change },
    }),
  ).rejects.toThrow();
});
it.each([
  { expected_version: 2 },
  { operation: "end", expected_version: 1 },
  { id: scope.hospitalId },
  { attest_department_responsibility: false },
  { reason: "" },
  { reason: "Bad\0reason" },
])("refuses invalid command intent before sending: %j", async (change) => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(
    decidePharmacyStaffBinding(reviewer, binding.id, {
      ...command("approve"),
      ...change,
    } as StaffDepartmentBindingCommandInput),
  ).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});
it.each([
  { replaces_binding_id: id(10) },
  { replaces_binding_id: id(10), replaces_binding_version: 1 },
  { replaces_binding_id: binding.id, replaces_binding_version: 2 },
  { department_id: scope.hospitalId },
])("refuses incomplete or invalid proposal intent: %j", async (change) => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(
    preparePharmacyStaffBinding(scope, binding.id, { ...input, ...change }),
  ).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});
it("retains exact replacement intent", async () => {
  const replacement = { ...input, replaces_binding_id: id(10), replaces_binding_version: 2 };
  const response = { ...proposal, binding: { ...binding, ...replacement } };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(response)));
  expect(await preparePharmacyStaffBinding(scope, binding.id, replacement)).toEqual(response);
});
it("does not resubmit a failed mutation automatically", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json({ error: "unavailable" }, { status: 503 }));
  vi.stubGlobal("fetch", fetch);
  await expect(
    decidePharmacyStaffBinding(reviewer, binding.id, command("approve")),
  ).rejects.toThrow();
  expect(fetch).toHaveBeenCalledOnce();
});
it("discards a late command response after caller cancellation", async () => {
  const abort = new AbortController();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      abort.abort();
      return Response.json(receipt("approve"));
    }),
  );
  await expect(
    decidePharmacyStaffBinding(reviewer, binding.id, command("approve"), abort.signal),
  ).rejects.toThrow();
});
it("discards a late recovery response across a workspace transition", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      beginTenantTransition();
      cancelTenantTransition();
      return Response.json(receipt("approve"));
    }),
  );
  await expect(
    recoverPharmacyStaffBindingReceipt(reviewer, {
      kind: "decision",
      bindingId: binding.id,
      input: command("approve"),
    }),
  ).rejects.toThrow();
});
it("pins the original command before an in-flight caller edit", async () => {
  const request = command("approve");
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      request.reason = "Edited during flight";
      const response = receipt("approve");
      return Response.json({
        ...response,
        reason: request.reason,
        binding: { ...response.binding, decision_reason: request.reason },
      });
    }),
  );
  await expect(decidePharmacyStaffBinding(reviewer, binding.id, request)).rejects.toThrow();
});
it("accepts equivalent timezone offsets while preserving microsecond evidence", async () => {
  const response = receipt("approve");
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      Response.json({
        ...response,
        recorded_at: "2026-11-05T12:00:00.123456+11:00",
      }),
    ),
  );
  expect(
    (await decidePharmacyStaffBinding(reviewer, binding.id, command("approve"))).recorded_at,
  ).toBe("2026-11-05T12:00:00.123456+11:00");
});
it("allows a separately authorized third reviewer to end the relationship while retaining original approval", async () => {
  const response = receipt("end");
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        Response.json({ ...response, actor_subject: "third-reviewer", actor_staff_id: id(11) }),
      ),
  );
  const result = await decidePharmacyStaffBinding(
    { ...scope, subject: "third-reviewer" },
    binding.id,
    command("end"),
  );
  expect(result.binding.decided_by_subject).toBe(reviewer.subject);
});

it("rejects reusing the proposal command identifier for its decision", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(
    decidePharmacyStaffBinding(reviewer, binding.id, { ...command("approve"), id: binding.id }),
  ).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});
it("verifies the ended version of a replaced relationship in the approval receipt", async () => {
  const response = receipt("approve");
  const replacement = {
    ...response,
    replaced_binding_id: id(10),
    replaced_binding_version: 3,
    binding: { ...response.binding, replaces_binding_id: id(10), replaces_binding_version: 2 },
  };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(replacement)));
  expect(await decidePharmacyStaffBinding(reviewer, binding.id, command("approve"))).toEqual(
    replacement,
  );
});
it.each([
  { replaced_binding_id: id(10) },
  { replaced_binding_id: id(10), replaced_binding_version: 2 },
  { replaced_binding_id: id(10), replaced_binding_version: 3 },
])("withholds a wrong replacement transition: %j", async (change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(Response.json({ ...receipt("approve"), ...change })),
  );
  await expect(
    decidePharmacyStaffBinding(reviewer, binding.id, command("approve")),
  ).rejects.toThrow();
});
it("accepts retained canonical Staff identifiers in proposal receipts", async () => {
  const response = {
    ...proposal,
    actor_staff_id: scope.hospitalId,
    binding: { ...binding, proposer_staff_id: scope.hospitalId },
  };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(response)));
  expect(await preparePharmacyStaffBinding(scope, binding.id, input)).toEqual(response);
});
it("pins account scope before transport begins", async () => {
  const context = { ...reviewer };
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      context.subject = "changed-account";
      return Response.json({
        ...receipt("approve"),
        actor_subject: context.subject,
        binding: { ...receipt("approve").binding, decided_by_subject: context.subject },
      });
    }),
  );
  await expect(
    decidePharmacyStaffBinding(context, binding.id, command("approve")),
  ).rejects.toThrow();
});
it("does not send a proposal while its workspace is switching", async () => {
  beginTenantTransition();
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(preparePharmacyStaffBinding(scope, binding.id, input)).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});
