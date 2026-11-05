import { afterEach, expect, it, vi } from "vitest";
import {
  getPharmacyStaffBinding,
  listPharmacyStaffBindings,
  listPharmacyStaffBindingHistory,
} from "./pharmacy-staff-bindings";
import { beginTenantTransition, cancelTenantTransition } from "./tenant-transition";

const id = (n: number) => "019a0000-0000-7000-8000-" + String(n).padStart(12, "0");
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-binding-reviewer",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  pharmacyId: id(1),
  outletId: id(2),
};
const binding = {
  id: id(3),
  pharmacy_id: scope.pharmacyId,
  outlet_id: scope.outletId,
  hospital_id: scope.hospitalId,
  department_id: id(4),
  department_revision_id: id(5),
  replaces_binding_id: null,
  replaces_binding_version: null,
  reason: "Synthetic relationship preparation",
  status: "pending",
  version: 1,
  proposed_by_subject: "synthetic-preparer",
  proposer_staff_id: id(6),
  created_at: "2026-11-05T00:00:00Z",
  decided_by_subject: null,
  reviewer_staff_id: null,
  decision_reason: null,
  decided_at: null,
  ended_at: null,
};
const view = { binding, source_state: "not_applicable" };
afterEach(() => {
  vi.unstubAllGlobals();
  cancelTenantTransition();
});

it("reads the exact relationship through Global API with account intent and no cache", async () => {
  const fetch = vi.fn().mockResolvedValue(Response.json(view));
  vi.stubGlobal("fetch", fetch);
  expect(await getPharmacyStaffBinding(scope, binding.id)).toEqual(view);
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
  expect(options.cache).toBe("no-store");
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(new Headers(options.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

it.each([
  { pharmacy_id: id(8) },
  { outlet_id: id(8) },
  { hospital_id: id(8) },
  { id: id(8) },
  { id: scope.hospitalId },
  { status: "approved", version: 1 },
  { decided_by_subject: scope.subject },
])("withholds a mismatched or incoherent relationship: %j", async (change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(Response.json({ ...view, binding: { ...binding, ...change } })),
  );
  await expect(getPharmacyStaffBinding(scope, binding.id)).rejects.toThrow();
});

it("does not return data after cancellation even if the transport ignores its signal", async () => {
  const abort = new AbortController();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      abort.abort();
      return Response.json(view);
    }),
  );
  await expect(getPharmacyStaffBinding(scope, binding.id, abort.signal)).rejects.toThrow();
});

it("withholds late data across a workspace transition", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      beginTenantTransition();
      cancelTenantTransition();
      return Response.json(view);
    }),
  );
  await expect(getPharmacyStaffBinding(scope, binding.id)).rejects.toThrow();
});

it("accepts an existing canonical Staff reference without requiring a new Staff identifier", async () => {
  const existing = { ...view, binding: { ...binding, proposer_staff_id: scope.hospitalId } };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(existing)));
  expect(await getPharmacyStaffBinding(scope, binding.id)).toEqual(existing);
});

const page = (data: unknown[], next: string | null = null) => ({
  data,
  page: { has_more: next !== null, next_cursor: next },
});
const authorizations = ["pharmacy.staff_binding.prepare", "workforce.staff_actor.use"].map(
  (permission) => ({
    permission,
    assignment_id: id(7),
    assignment_version: 4,
    scope_epoch: 2,
    membership_epoch: 1,
  }),
);
const history = {
  id: id(8),
  binding_id: binding.id,
  outlet_id: scope.outletId,
  hospital_id: scope.hospitalId,
  actor_subject: binding.proposed_by_subject,
  action: "propose",
  command_id: binding.id,
  recorded_at: binding.created_at,
  recorded_version: 1,
  source_state: null,
  related_binding_id: null,
  related_binding_version: null,
  authorizations,
};

it("pages recorded relationships using an opaque cursor and verifies every row", async () => {
  const response = page([binding], "next+/=");
  const fetch = vi.fn().mockResolvedValue(Response.json(response));
  vi.stubGlobal("fetch", fetch);
  expect(await listPharmacyStaffBindings(scope, "previous+/=")).toEqual(response);
  const url = new URL(fetch.mock.calls[0]![0], "https://example.invalid");
  expect(url.searchParams.get("limit")).toBe("50");
  expect(url.searchParams.get("cursor")).toBe("previous+/=");
});
it("reads exact relationship history with historical authorization observations", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(page([history]))));
  expect(await listPharmacyStaffBindingHistory(scope, binding.id)).toEqual(page([history]));
});

it.each([
  page([binding, binding]),
  { data: [], page: { has_more: true, next_cursor: null } },
  { data: [], page: { has_more: false, next_cursor: "unexpected" } },
  page([binding], "same-cursor"),
])("withholds ambiguous relationship pagination: %j", async (response) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(response)));
  await expect(listPharmacyStaffBindings(scope, "same-cursor")).rejects.toThrow();
});
it.each([
  { binding_id: id(9) },
  { outlet_id: id(9) },
  { hospital_id: id(9) },
  { related_binding_id: id(9) },
  { command_id: null },
  { recorded_version: 2 },
  { authorizations: [authorizations[0], authorizations[0]] },
  {
    authorizations: authorizations.map((value) => ({ ...value, assignment_id: scope.hospitalId })),
  },
  {
    authorizations: authorizations.map((value) => ({
      ...value,
      permission: "pharmacy.catalogue.read",
    })),
  },
])("withholds mismatched history or authorization observations: %j", async (change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(Response.json(page([{ ...history, ...change }]))),
  );
  await expect(listPharmacyStaffBindingHistory(scope, binding.id)).rejects.toThrow();
});
it("rejects duplicate history rows", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(page([history, history]))));
  await expect(listPharmacyStaffBindingHistory(scope, binding.id)).rejects.toThrow();
});
it.each([
  { tenantId: "invalid" },
  { subject: " changed-account " },
  { subject: "account\nheader" },
  { hospitalId: "../private" },
  { pharmacyId: scope.hospitalId },
  { outletId: scope.hospitalId },
])("rejects invalid request scope before sending: %j", async (change) => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(getPharmacyStaffBinding({ ...scope, ...change }, binding.id)).rejects.toThrow();
  expect(fetch).not.toHaveBeenCalled();
});
it("withholds mismatched list scope", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(Response.json(page([{ ...binding, outlet_id: id(9) }]))),
  );
  await expect(listPharmacyStaffBindings(scope)).rejects.toThrow();
});

it.each(["approved", "rejected", "withdrawn", "ended"] as const)(
  "accepts a coherent %s relationship without treating it as an operating grant",
  async (status) => {
    const decided = {
      ...binding,
      status,
      version: status === "ended" ? 3 : 2,
      decided_by_subject: status === "withdrawn" ? binding.proposed_by_subject : scope.subject,
      reviewer_staff_id: status === "withdrawn" ? binding.proposer_staff_id : id(9),
      decision_reason: "Synthetic independent decision",
      decided_at: "2026-11-05T01:00:00Z",
      ended_at: status === "ended" ? "2026-11-05T02:00:00Z" : null,
    };
    const response = {
      binding: decided,
      source_state: status === "approved" ? "current" : "not_applicable",
    };
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(response)));
    expect(await getPharmacyStaffBinding(scope, binding.id)).toEqual(response);
  },
);
it.each([
  { status: "approved", version: 2 },
  { proposed_by_subject: "proposer\nheader" },
  { replaces_binding_id: id(9) },
])("withholds malformed relationship decision or actor evidence: %j", async (change) => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(Response.json({ ...view, binding: { ...binding, ...change } })),
  );
  await expect(getPharmacyStaffBinding(scope, binding.id)).rejects.toThrow();
});
it.each(["current", "changed"])(
  "does not present pending evidence as %s source authority",
  async (source_state) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ ...view, source_state })));
    await expect(getPharmacyStaffBinding(scope, binding.id)).rejects.toThrow();
  },
);
it("detects duplicate identifiers with different casing", async () => {
  vi.stubGlobal(
    "fetch",
    vi
      .fn()
      .mockResolvedValue(
        Response.json(page([binding, { ...binding, id: binding.id.toUpperCase() }])),
      ),
  );
  await expect(listPharmacyStaffBindings(scope)).rejects.toThrow();
});
it("normalizes the requested identifier before validating exact proposal history", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json(page([history]))));
  expect(await listPharmacyStaffBindingHistory(scope, binding.id.toUpperCase())).toEqual(
    page([history]),
  );
});
