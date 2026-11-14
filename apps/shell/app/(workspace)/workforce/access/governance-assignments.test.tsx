import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { staffAccessTemplates } from "@helivanta/api";
import { renderWithProviders } from "@helivanta/api/testing";
import { GovernanceAssignments } from "./governance-assignments";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-reviewer" };
const hospitalId = "22222222-2222-4222-8222-222222222222";
const id = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const proposal = {
  id,
  input: {
    hospital_id: hospitalId,
    subject: "synthetic-recipient",
    template_key: "workforce.leave_reviewer.v1",
    reason: "Synthetic proposed review duty",
    valid_from: "2026-11-01T00:00:00Z",
    expires_at: "2099-11-01T00:00:00Z",
  },
  scope_kind: "hospital",
  proposed_by: "synthetic-proposer",
  status: "pending_review",
  version: 2,
  created_at: "2026-11-01T00:00:00Z",
  updated_at: "2026-11-01T00:00:00Z",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
const hospital = {
  hospital_id: hospitalId,
  name: "Synthetic hospital",
  can_manage: true,
  can_review: true,
};
afterEach(() => vi.unstubAllGlobals());

it("selects a named hospital and retains the exact reviewed decision after an uncertain response", async () => {
  const writes: { url: string; body: string; tenant: string | null; subject: string | null }[] = [];
  const paths: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      paths.push(url);
      if (url.includes("assignment-templates")) return Response.json(staffAccessTemplates);
      if (url.endsWith("/account"))
        return Response.json({
          proposal_id: id,
          proposal_version: 2,
          subject: proposal.input.subject,
          available: true,
          name: "Synthetic Staff",
          email: "synthetic@example.invalid",
        });
      if (init?.method === "POST") {
        writes.push({
          url,
          body: String(init.body),
          tenant: new Headers(init.headers).get("X-Helivanta-Expected-Tenant"),
          subject: new Headers(init.headers).get("X-Helivanta-Expected-Subject"),
        });
        if (writes.length === 1)
          return Response.json(
            { error: { code: "unavailable", message: "Synthetic uncertain response" } },
            { status: 503 },
          );
        return Response.json({ ...proposal, status: "pending_decision_scope", version: 3 });
      }
      if (url.includes("assignment-templates")) return Response.json(staffAccessTemplates);
      if (url.endsWith("/account"))
        return Response.json({
          proposal_id: id,
          proposal_version: 2,
          subject: proposal.input.subject,
          available: true,
          name: "Synthetic Staff",
          email: "synthetic@example.invalid",
        });
      if (url.includes("assignment-authority-scopes")) return Response.json(page([hospital]));
      if (url.endsWith(`/assignment-proposals/${id}`)) return Response.json(proposal);
      if (url.includes("/history"))
        return Response.json(
          page([{ id, version: 2, status: "pending_review", created_at: proposal.created_at }]),
        );
      if (url.includes("/decisions")) return Response.json(page([]));
      return Response.json(page([proposal]));
    }),
  );
  const { user } = renderWithProviders(<GovernanceAssignments scope={scope} />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic hospital" }));
  await user.click(await screen.findByRole("button", { name: "Review assignment" }));
  await user.click(await screen.findByRole("button", { name: "Approve" }));
  await user.type(
    screen.getByLabelText("Decision reason"),
    "Independently reviewed the current assignment",
  );
  await user.click(screen.getByRole("button", { name: "Confirm approve" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("may have been recorded");
  expect(screen.getByLabelText("Decision reason")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry same request" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(JSON.parse(writes[0]!.body)).toMatchObject({ expected_version: 2, decision: "approve" });
  expect(writes[0]!.tenant).toBe(scope.tenantId);
  expect(writes[0]!.subject).toBe(scope.subject);
  expect(
    paths.every(
      (path) =>
        path.includes("assignment-authority-scopes") ||
        path.includes(`/iam/hospitals/${hospitalId}/assignment-proposals`) ||
        path.includes(`/iam/hospitals/${hospitalId}/assignment-templates`),
    ),
  ).toBe(true);
});

it("removes disclosed assignment data when current review access is refused", async () => {
  let revoked = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("assignment-templates")) return Response.json(staffAccessTemplates);
      if (url.endsWith("/account"))
        return Response.json({
          proposal_id: id,
          proposal_version: 2,
          subject: proposal.input.subject,
          available: true,
          name: "Synthetic Staff",
          email: "synthetic@example.invalid",
        });
      if (url.includes("assignment-authority-scopes")) return Response.json(page([hospital]));
      if (revoked)
        return Response.json(
          { error: { code: "not_found", message: "Not found" } },
          { status: 404 },
        );
      if (url.endsWith(`/assignment-proposals/${id}`)) return Response.json(proposal);
      return Response.json(page([proposal]));
    }),
  );
  const { user } = renderWithProviders(<GovernanceAssignments scope={scope} />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic hospital" }));
  await user.click(await screen.findByRole("button", { name: "Review assignment" }));
  expect(await screen.findByText(proposal.input.reason)).toBeInTheDocument();
  revoked = true;
  await user.click(screen.getByRole("button", { name: "Refresh selected assignment" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Staff access changed");
  expect(screen.queryByText(proposal.input.reason)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
});

it("does not offer self approval when the selected account proposed the assignment", async () => {
  const own = { ...proposal, proposed_by: scope.subject };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("assignment-templates")) return Response.json(staffAccessTemplates);
      if (url.endsWith("/account"))
        return Response.json({
          proposal_id: id,
          proposal_version: 2,
          subject: proposal.input.subject,
          available: true,
          name: "Synthetic Staff",
          email: "synthetic@example.invalid",
        });
      if (url.includes("assignment-authority-scopes")) return Response.json(page([hospital]));
      if (url.endsWith(`/assignment-proposals/${id}`)) return Response.json(own);
      return Response.json(page([own]));
    }),
  );
  const { user } = renderWithProviders(<GovernanceAssignments scope={scope} />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic hospital" }));
  await user.click(await screen.findByRole("button", { name: "Review assignment" }));
  expect(await screen.findByRole("button", { name: "Withdraw" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Reject" })).not.toBeInTheDocument();
});

it("loads bounded history and decisions only when requested and hides both after a refused history page", async () => {
  let nextPage = false;
  const paths: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      paths.push(url);
      if (url.includes("assignment-templates")) return Response.json(staffAccessTemplates);
      if (url.endsWith("/account"))
        return Response.json({
          proposal_id: id,
          proposal_version: 2,
          subject: proposal.input.subject,
          available: true,
          name: "Synthetic Staff",
          email: "synthetic@example.invalid",
        });
      if (url.includes("assignment-templates")) return Response.json(staffAccessTemplates);
      if (url.endsWith("/account"))
        return Response.json({
          proposal_id: id,
          proposal_version: 2,
          subject: proposal.input.subject,
          available: true,
          name: "Synthetic Staff",
          email: "synthetic@example.invalid",
        });
      if (url.includes("assignment-authority-scopes")) return Response.json(page([hospital]));
      if (url.includes("/history")) {
        if (nextPage)
          return Response.json(
            { error: { code: "forbidden", message: "Access changed" } },
            { status: 403 },
          );
        return Response.json({
          data: [{ id, version: 2, status: "pending_review", created_at: proposal.created_at }],
          page: { has_more: true, next_cursor: "opaque-history" },
        });
      }
      if (url.includes("/decisions")) return Response.json(page([]));
      if (url.endsWith(`/assignment-proposals/${id}`)) return Response.json(proposal);
      return Response.json(page([proposal]));
    }),
  );
  const { user } = renderWithProviders(<GovernanceAssignments scope={scope} />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic hospital" }));
  await user.click(await screen.findByRole("button", { name: "Review assignment" }));
  await screen.findByText(proposal.input.reason);
  expect(paths.some((url) => url.includes("/history") || url.includes("/decisions"))).toBe(false);
  await user.click(screen.getByRole("button", { name: "History" }));
  await screen.findByRole("region", { name: "Assignment history" });
  const historyPath = paths.find((url) => url.includes("/history"))!;
  expect(new URL(historyPath, "https://example.invalid").searchParams.get("limit")).toBe("50");
  nextPage = true;
  await user.click(screen.getByRole("button", { name: "More history" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Staff access changed");
  expect(screen.queryByRole("region", { name: "Assignment history" })).not.toBeInTheDocument();
  expect(screen.queryByText(proposal.input.reason)).not.toBeInTheDocument();
  expect(paths.some((url) => url.includes("cursor=opaque-history"))).toBe(true);
});

it("withholds assignment actions when the server canonical catalog changes", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("assignment-authority-scopes")) return Response.json(page([hospital]));
      if (url.includes("assignment-templates")) return Response.json(staffAccessTemplates.slice(1));
      return Response.json(page([proposal]));
    }),
  );
  const { user } = renderWithProviders(<GovernanceAssignments scope={scope} />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic hospital" }));
  expect(await screen.findByText("Could not verify assignment templates.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Review assignment" })).not.toBeInTheDocument();
});

it("requires current proposal-qualified identity before offering approval", async () => {
  let release: ((response: Response) => void) | undefined;
  const held = new Promise<Response>((resolve) => {
    release = resolve;
  });
  const paths: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      paths.push(url);
      if (url.includes("assignment-authority-scopes")) return Response.json(page([hospital]));
      if (url.includes("assignment-templates")) return Response.json(staffAccessTemplates);
      if (url.endsWith("/account")) return held;
      if (url.endsWith(`/assignment-proposals/${id}`)) return Response.json(proposal);
      return Response.json(page([proposal]));
    }),
  );
  const { user } = renderWithProviders(<GovernanceAssignments scope={scope} />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic hospital" }));
  await user.click(await screen.findByRole("button", { name: "Review assignment" }));
  await screen.findByText(proposal.input.reason);
  expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  await waitFor(() =>
    expect(paths.some((url) => url.endsWith(`/assignment-proposals/${id}/account`))).toBe(true),
  );
  release!(
    Response.json({
      proposal_id: id,
      proposal_version: 2,
      subject: proposal.input.subject,
      available: true,
      name: "Verified synthetic staff",
      email: "verified-staff@example.invalid",
    }),
  );
  expect(await screen.findByText(/Verified synthetic staff/)).toBeInTheDocument();
  expect(await screen.findByText(/verified-staff@example.invalid/)).toBeInTheDocument();
  expect(await screen.findByRole("button", { name: "Approve" })).toBeInTheDocument();
});

it("retains withdrawal and history when the target identity is known unavailable", async () => {
  const own = { ...proposal, proposed_by: scope.subject };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("assignment-authority-scopes")) return Response.json(page([hospital]));
      if (url.includes("assignment-templates")) return Response.json(staffAccessTemplates);
      if (url.endsWith("/account"))
        return Response.json({
          proposal_id: id,
          proposal_version: 2,
          subject: proposal.input.subject,
          available: false,
        });
      if (url.endsWith(`/assignment-proposals/${id}`)) return Response.json(own);
      return Response.json(page([own]));
    }),
  );
  const { user } = renderWithProviders(<GovernanceAssignments scope={scope} />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic hospital" }));
  await user.click(await screen.findByRole("button", { name: "Review assignment" }));
  expect(await screen.findByText("Current account identity is unavailable.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Withdraw" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "History" })).toBeInTheDocument();
});

it("clears disclosed data after an expected-tenant mismatch on the account read", async () => {
  let mismatch = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("assignment-authority-scopes")) return Response.json(page([hospital]));
      if (url.includes("assignment-templates")) return Response.json(staffAccessTemplates);
      if (url.endsWith("/account"))
        return mismatch
          ? Response.json({ error: "tenant_changed", message: "Account changed" }, { status: 409 })
          : Response.json({
              proposal_id: id,
              proposal_version: 2,
              subject: proposal.input.subject,
              available: true,
              name: "Verified synthetic staff",
              email: "verified-staff@example.invalid",
            });
      if (url.endsWith(`/assignment-proposals/${id}`)) return Response.json(proposal);
      return Response.json(page([proposal]));
    }),
  );
  const { user } = renderWithProviders(<GovernanceAssignments scope={scope} />);
  await user.click(await screen.findByRole("button", { name: "Open Synthetic hospital" }));
  await user.click(await screen.findByRole("button", { name: "Review assignment" }));
  await screen.findByText(/Verified synthetic staff/);
  mismatch = true;
  await user.click(screen.getByRole("button", { name: "Refresh selected assignment" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Staff access changed");
  expect(screen.queryByText(proposal.input.reason)).not.toBeInTheDocument();
  expect(screen.queryByText(/Verified synthetic staff/)).not.toBeInTheDocument();
});
