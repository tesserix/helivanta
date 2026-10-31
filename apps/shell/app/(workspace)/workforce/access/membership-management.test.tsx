import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { MembershipManagement } from "./membership-management";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-admin" };
const candidate = {
  id: "22222222-2222-4222-8222-222222222222",
  subject: "synthetic-target",
  role_key: "staff_member",
  membership_epoch: 1,
  created_at: "2026-10-30T00:00:00Z",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());

it("requires a reason and acknowledgement and freezes the exact removal after an uncertain response", async () => {
  const writes: Array<{ url: string; body: Record<string, unknown>; tenant: string | null }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body));
        writes.push({
          url,
          body,
          tenant: new Headers(init.headers).get("X-Helivanta-Expected-Tenant"),
        });
        if (writes.length === 1)
          return Response.json(
            { error: "unavailable", message: "Synthetic ambiguous response" },
            { status: 502 },
          );
        return Response.json({
          id: url.split("/").at(-1),
          ...body,
          result_membership_id: body.basis_id,
          result_membership_epoch: 2,
          actor_subject: scope.subject,
          created_at: "2026-10-30T00:00:00Z",
        });
      }
      return Response.json(page(url.includes("membership-candidates") ? [candidate] : []));
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <MembershipManagement scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Remove membership" }));
  await user.click(screen.getByRole("button", { name: "Record membership removal" }));
  expect(writes).toHaveLength(0);
  await user.type(screen.getByLabelText("Reason"), "Remove this specific membership after review");
  await user.click(screen.getByLabelText("I reviewed this account, role and consequence."));
  await user.click(screen.getByRole("button", { name: "Record membership removal" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("may have been recorded");
  expect(screen.getByLabelText("Reason")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry same membership command" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(writes[0]?.tenant).toBe(scope.tenantId);
  expect(writes[0]?.body).toMatchObject({
    operation: "remove",
    basis_id: candidate.id,
    expected_membership_epoch: 1,
  });
  expect(await screen.findByText(/Membership change recorded/)).toBeInTheDocument();
});

it("recovers ordinary membership from the retained removal and current epoch", async () => {
  const prior = {
    id: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa",
    operation: "remove",
    basis_id: candidate.id,
    subject: candidate.subject,
    role_key: "doctor",
    expected_membership_epoch: 1,
    reason: "Previous reviewed removal",
    result_membership_id: candidate.id,
    result_membership_epoch: 2,
    actor_subject: "previous-admin",
    created_at: candidate.created_at,
    current_membership_epoch: 4,
    recoverable: true,
  };
  const writes: Array<Record<string, unknown>> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body));
        writes.push(body);
        return Response.json({
          id: url.split("/").at(-1),
          ...body,
          result_membership_id: "019aaaaa-bbbb-7aaa-8aaa-aaaaaaaaaaaa",
          result_membership_epoch: 4,
          actor_subject: scope.subject,
          created_at: candidate.created_at,
        });
      }
      return Response.json(
        page(
          url.includes("membership-candidates")
            ? []
            : [{ ...prior, recoverable: writes.length === 0 }],
        ),
      );
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <MembershipManagement scope={scope} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Recover ordinary membership" }));
  await user.type(
    screen.getByLabelText("Reason"),
    "Recover this verified active identity as an ordinary member",
  );
  await user.click(screen.getByLabelText("I reviewed this account, role and consequence."));
  await user.click(screen.getByRole("button", { name: "Record ordinary membership recovery" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    operation: "recover",
    basis_id: prior.id,
    subject: candidate.subject,
    role_key: "staff_member",
    expected_membership_epoch: 4,
    reason: "Recover this verified active identity as an ordinary member",
  });
  expect(await screen.findByText(/Membership change recorded/)).toBeInTheDocument();
});
