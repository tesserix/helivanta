import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { apiFetch } from "./client";
import { beginTenantTransition, cancelTenantTransition } from "./tenant-transition";
import {
  decideGovernanceGrant,
  issueGovernanceGrant,
  readGovernanceGrant,
  listGovernanceNotices,
} from "./staff-governance";

vi.mock("./client", () => ({ apiFetch: vi.fn() }));
const request = vi.mocked(apiFetch);
const tenant = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const grant = "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb";
const command = "019ccccc-cccc-7ccc-8ccc-cccccccccccc";
const foreign = "019ddddd-dddd-7ddd-8ddd-dddddddddddd";
const scope = { tenantId: tenant, subject: "synthetic-owner" };
const input = {
  subject: "synthetic-reviewer",
  expected_membership_epoch: 1,
  template_key: "iam.governance_auditor.v1" as const,
  scope_kind: "tenant" as const,
  reason: "Review nonclinical governance",
  valid_from: "2026-11-13T10:00:00+05:30",
  expires_at: "2026-11-14T10:00:00+05:30",
};
const receipt = () => ({
  id: grant,
  tenant_id: tenant,
  input: { ...input, valid_from: "2026-11-13T04:30:00Z", expires_at: "2026-11-14T04:30:00Z" },
  status: "provisional_active",
  version: 1,
  issued_by: scope.subject,
  owner_count: 1,
  authorization_mode: "single_owner_provisional",
  created_at: "2026-11-13T05:00:00Z",
  updated_at: "2026-11-13T05:00:00Z",
});
const decision = {
  expected_version: 1,
  decision: "approve" as const,
  reason: "Independent review",
};
const decisionReceipt = () => ({
  id: command,
  tenant_id: tenant,
  grant_id: grant,
  input: decision,
  result_version: 2,
  actor_subject: scope.subject,
  created_at: "2026-11-13T05:01:00Z",
});

beforeEach(() => {
  request.mockReset();
  cancelTenantTransition();
});
afterEach(cancelTenantTransition);

describe("governance command response binding", () => {
  it("recovers the original issued receipt with equivalent server UTC instants", async () => {
    request.mockResolvedValue(receipt());
    await expect(issueGovernanceGrant(scope, grant, input)).resolves.toMatchObject({
      id: grant,
      version: 1,
    });
    expect(request).toHaveBeenCalledWith(
      "/iam/governance-grants/" + grant,
      expect.objectContaining({ method: "PUT" }),
    );
  });
  it.each([
    { tenant_id: foreign },
    { id: command },
    { issued_by: "another-owner" },
    { input: { ...input, subject: "another-recipient" } },
    { input: { ...input, expires_at: "2026-11-14T04:30:00.000001Z" } },
    { version: 2, status: "active" },
  ])("rejects an issued receipt outside the original command: %j", async (change) => {
    request.mockResolvedValue({ ...receipt(), ...change });
    await expect(issueGovernanceGrant(scope, grant, input)).rejects.toThrow();
  });
  it("allows an authorized reviewer to read another issuer's current head", async () => {
    request.mockResolvedValue({
      ...receipt(),
      version: 3,
      status: "revoked",
      issued_by: "another-owner",
    });
    await expect(readGovernanceGrant(scope, grant)).resolves.toMatchObject({
      version: 3,
      status: "revoked",
    });
  });
  it.each([{ tenant_id: foreign }, { id: command }])(
    "binds current reads to tenant and grant: %j",
    async (change) => {
      request.mockResolvedValue({ ...receipt(), ...change });
      await expect(readGovernanceGrant(scope, grant)).rejects.toThrow();
    },
  );
  it("retains decision command IDs for lost-response recovery", async () => {
    request.mockResolvedValue(decisionReceipt());
    await expect(decideGovernanceGrant(scope, grant, command, decision)).resolves.toMatchObject({
      id: command,
      result_version: 2,
    });
    expect(request).toHaveBeenCalledWith(
      "/iam/governance-grants/" + grant + "/decisions/" + command,
      expect.objectContaining({ method: "PUT" }),
    );
  });
  it.each([
    { tenant_id: foreign },
    { id: grant },
    { grant_id: foreign },
    { actor_subject: "another-reviewer" },
    { result_version: 3 },
    { input: { ...decision, decision: "reject" } },
    { input: { ...decision, reason: "Changed reason" } },
  ])("rejects an unrelated decision receipt: %j", async (change) => {
    request.mockResolvedValue({ ...decisionReceipt(), ...change });
    await expect(decideGovernanceGrant(scope, grant, command, decision)).rejects.toThrow();
  });
  it("rejects supplied authority fields before network I/O", async () => {
    await expect(
      issueGovernanceGrant(scope, grant, { ...input, status: "active" } as typeof input),
    ).rejects.toThrow();
    await expect(
      decideGovernanceGrant(scope, grant, command, {
        ...decision,
        actor_subject: scope.subject,
      } as typeof decision),
    ).rejects.toThrow();
    expect(request).not.toHaveBeenCalled();
  });
  it("rejects ambiguous path IDs before network I/O", async () => {
    await expect(readGovernanceGrant(scope, grant.toUpperCase())).rejects.toThrow();
    await expect(decideGovernanceGrant(scope, grant, "../commands", decision)).rejects.toThrow();
    expect(request).not.toHaveBeenCalled();
  });
  it("blocks new requests while switching tenant", async () => {
    beginTenantTransition();
    await expect(readGovernanceGrant(scope, grant)).rejects.toThrow();
    expect(request).not.toHaveBeenCalled();
  });
  it("discards an in-flight response after switching tenant", async () => {
    let resolve!: (value: unknown) => void;
    request.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const result = readGovernanceGrant(scope, grant);
    beginTenantTransition();
    resolve(receipt());
    await expect(result).rejects.toThrow();
  });
  it("honors caller cancellation before network I/O", async () => {
    const abort = new AbortController();
    abort.abort();
    await expect(readGovernanceGrant(scope, grant, abort.signal)).rejects.toThrow();
    expect(request).not.toHaveBeenCalled();
  });
});

const notice = () => ({
  id: command,
  tenant_id: tenant,
  grant_id: grant,
  grant_version: 1,
  recipient_subject: scope.subject,
  version: 1,
  created_at: "2026-11-13T05:00:00Z",
});
const noticePage = () => ({ data: [notice()], page: { has_more: false, next_cursor: null } });

describe("owner governance inbox", () => {
  it("binds bounded pages to the current owner and tenant", async () => {
    request.mockResolvedValue(noticePage());
    await expect(listGovernanceNotices(scope)).resolves.toEqual(noticePage());
    expect(request).toHaveBeenCalledWith("/iam/governance-notices?limit=50", expect.anything());
  });
  it.each([{ tenant_id: foreign }, { recipient_subject: "another-owner" }])(
    "refuses another account's private notices: %j",
    async (change) => {
      request.mockResolvedValue({ ...noticePage(), data: [{ ...notice(), ...change }] });
      await expect(listGovernanceNotices(scope)).rejects.toThrow();
    },
  );
  it.each([
    { data: [notice(), notice()] },
    { page: { has_more: true, next_cursor: null } },
    { page: { has_more: false, next_cursor: "next" } },
    { data: Array.from({ length: 51 }, () => notice()) },
  ])("refuses malformed or duplicated pages: %j", async (change) => {
    request.mockResolvedValue({ ...noticePage(), ...change });
    await expect(listGovernanceNotices(scope)).rejects.toThrow();
  });
  it("preserves an opaque cursor and refuses repeated continuation", async () => {
    request.mockResolvedValue(noticePage());
    await listGovernanceNotices(scope, "opaque+/cursor=");
    expect(request).toHaveBeenCalledWith(
      "/iam/governance-notices?limit=50&cursor=opaque%2B%2Fcursor%3D",
      expect.anything(),
    );
    request.mockResolvedValue({
      ...noticePage(),
      page: { has_more: true, next_cursor: "previous" },
    });
    await expect(listGovernanceNotices(scope, "previous")).rejects.toThrow();
  });
  it("does not disclose a response after a workspace switch", async () => {
    let resolve!: (value: unknown) => void;
    request.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const result = listGovernanceNotices(scope);
    beginTenantTransition();
    resolve(noticePage());
    await expect(result).rejects.toThrow();
  });
  it("honors cancellation and rejects oversized cursors before network I/O", async () => {
    const abort = new AbortController();
    abort.abort();
    await expect(listGovernanceNotices(scope, null, abort.signal)).rejects.toThrow();
    await expect(listGovernanceNotices(scope, "x".repeat(2049))).rejects.toThrow();
    expect(request).not.toHaveBeenCalled();
  });
});
