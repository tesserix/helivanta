import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { IndependentStaffLinks } from "./independent-staff-links";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-owner",
  employerKind: "supplier" as const,
  employerId: "019a55c0-1111-7111-8111-111111111111",
  staffId: "019a55c0-1111-7111-8111-111111111112",
};
const wireScope = {
  owner_tenant_id: scope.tenantId,
  employer_kind: scope.employerKind,
  employer_id: scope.employerId,
  staff_id: scope.staffId,
};
const permissions = {
  tenant_id: scope.tenantId,
  subject: scope.subject,
  data: ["workforce.independent_login.manage", "workforce.independent_login.review"],
};
afterEach(() => vi.unstubAllGlobals());
it("recovers a lost proposal response with one write and frozen account details", async () => {
  let saved: Record<string, unknown> | null = null;
  let writes = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions")) return Response.json(permissions);
      if (init?.method === "PUT") {
        writes++;
        saved = {
          id: url.split("/").at(-1),
          scope: wireScope,
          input: JSON.parse(String(init.body)),
          prepared_by: scope.subject,
          created_at: "2026-11-07T00:00:00Z",
        };
        throw new TypeError("TEST lost committed response");
      }
      if (url.endsWith("proposal-receipt")) return Response.json(saved);
      return Response.json({
        data: saved
          ? [
              {
                scope: wireScope,
                staff_version: 1,
                link: {
                  id: saved.id,
                  staff_id: scope.staffId,
                  subject: "TEST-target",
                  status: "pending",
                  version: 1,
                  proposed_by_subject: scope.subject,
                  proposal_reason: "TEST checked identity",
                  actor_subject: scope.subject,
                  reason: "TEST checked identity",
                  created_at: saved.created_at,
                  updated_at: saved.created_at,
                },
              },
            ]
          : [],
        page: { has_more: false, next_cursor: null },
      });
    }),
  );
  renderWithProviders(<IndependentStaffLinks scope={scope} staffVersion={1} canManage canReview />);
  const user = userEvent.setup();
  await user.type(await screen.findByLabelText("Account ID"), "TEST-target");
  await user.type(screen.getByLabelText("Account link reason"), "TEST checked identity");
  await user.click(screen.getByRole("button", { name: "Propose account link" }));
  await screen.findByRole("button", { name: "Recover account proposal" });
  expect(screen.getByLabelText("Account ID")).toBeDisabled();
  await screen.findByText("TEST-target", {}, { timeout: 5000 });
  expect(screen.getByRole("button", { name: "Recover account proposal" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Revoke account link" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Recover account proposal" }));
  await screen.findByText("Account proposal recorded. Independent review is required.");
  await waitFor(() => expect(writes).toBe(1));
});
it("recovers an independently approved link without submitting a second decision", async () => {
  const linkId = "019a55c0-1111-7111-8111-111111111113";
  const row = {
    scope: wireScope,
    staff_version: 1,
    link: {
      id: linkId,
      staff_id: scope.staffId,
      subject: "TEST-target",
      status: "pending",
      version: 1,
      proposed_by_subject: "TEST-maker",
      proposal_reason: "TEST checked identity",
      actor_subject: "TEST-maker",
      reason: "TEST checked identity",
      created_at: "2026-11-07T00:00:00Z",
      updated_at: "2026-11-07T00:00:00Z",
    },
  };
  let saved: Record<string, unknown> | null = null;
  let writes = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions")) return Response.json(permissions);
      if (init?.method === "PUT") {
        writes++;
        const input = JSON.parse(String(init.body));
        saved = {
          id: url.split("/").at(-1),
          link_id: linkId,
          scope: wireScope,
          input,
          actor_subject: scope.subject,
          created_at: "2026-11-07T00:01:00Z",
          result: {
            ...row,
            link: {
              ...row.link,
              status: "active",
              version: 2,
              actor_subject: scope.subject,
              reason: input.reason,
              updated_at: "2026-11-07T00:01:00Z",
            },
          },
        };
        throw new TypeError("TEST lost committed review");
      }
      if (url.includes("/commands/")) return Response.json(saved);
      return Response.json({ data: [row], page: { has_more: false, next_cursor: null } });
    }),
  );
  renderWithProviders(<IndependentStaffLinks scope={scope} staffVersion={1} canManage canReview />);
  const user = userEvent.setup();
  await user.type(
    await screen.findByLabelText("Decision reason for TEST-target"),
    "TEST independent review",
  );
  await user.click(screen.getByRole("button", { name: "Approve account link" }));
  await user.click(await screen.findByRole("button", { name: "Recover account decision" }));
  await screen.findByText("Account decision recorded.");
  expect(writes).toBe(1);
});
