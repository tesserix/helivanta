import { useState } from "react";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { OperatingDutyManagement } from "./operating-duty-management";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/workforce/operating-access",
  useSearchParams: () => new URLSearchParams(),
}));
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-owner",
  employerKind: "supplier" as const,
  employerId: "019a55c0-1111-7111-8111-111111111112",
  administration: "governance" as const,
};
const row = {
  owner_tenant_id: scope.tenantId,
  id: "019a55c0-1111-7111-8111-111111111114",
  input: {
    scope: {
      employer_kind: "supplier",
      employer_id: scope.employerId,
      location_id: "019a55c0-1111-7111-8111-111111111116",
      expected_employer_version: 1,
      expected_location_version: 1,
    },
    identity: {
      staff_id: "019a55c0-1111-7111-8111-111111111113",
      staff_version: 1,
      link_id: "019a55c0-1111-7111-8111-111111111117",
      link_version: 2,
    },
    subject: "TEST-inactive-account",
    template_key: "supply.inventory_receiver.v1",
    reason: "TEST retained ineligible duty",
    valid_from: "2026-11-07T00:00:00Z",
    expires_at: "2027-01-01T00:00:00Z",
  },
  status: "active",
  version: 3,
  prepared_by: "TEST-maker",
  created_at: "2026-11-07T00:00:00Z",
  updated_at: "2026-11-07T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());

it("manages retained duties independently of active Staff choices and offers no new approval", async () => {
  const requests: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      requests.push(url);
      if (url.endsWith("permissions"))
        return Response.json({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: ["iam.governance.operating.manage"],
        });
      if (url.includes("/duty-inbox?"))
        return Response.json({ data: [row], page: { has_more: false, next_cursor: null } });
      throw new Error(`Unexpected owner query: ${url}`);
    }),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <OperatingDutyManagement
        scope={scope}
        busy={false}
        setBusy={vi.fn()}
        onBack={vi.fn()}
        onClose={vi.fn()}
      />
    </NavigationGuardProvider>,
  );
  await screen.findByText(row.input.reason);
  expect(screen.getByRole("button", { name: "Revoke duty" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /^Approve/ })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Assign operating duty" })).not.toBeInTheDocument();
  expect(
    requests.some((url) => url.includes("staff-choices") || url.includes("account-link")),
  ).toBe(false);
});

it("hides retained personnel details when the current management source is lost", async () => {
  let cut = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("permissions"))
        return Response.json({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: ["iam.governance.operating.manage"],
        });
      if (url.includes("/duty-inbox?"))
        return cut
          ? Response.json(
              {
                error: {
                  code: "not_found",
                  message: "Current management source is no longer available",
                },
              },
              { status: 404 },
            )
          : Response.json({ data: [row], page: { has_more: false, next_cursor: null } });
      throw new Error(`Unexpected owner query: ${url}`);
    }),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <OperatingDutyManagement
        scope={scope}
        busy={false}
        setBusy={vi.fn()}
        onBack={vi.fn()}
        onClose={vi.fn()}
      />
    </NavigationGuardProvider>,
  );
  await screen.findByText(row.input.reason);
  cut = true;
  await userEvent.click(screen.getByRole("button", { name: "Refresh existing duties" }));
  await waitFor(() => expect(screen.queryByText(row.input.reason)).not.toBeInTheDocument());
  expect(screen.queryByText(row.input.subject)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Revoke duty" })).not.toBeInTheDocument();
  expect(screen.getByRole("alert")).toBeInTheDocument();
});

it("freezes retained management after a lost committed revocation and recovers by GET without repeating PUT", async () => {
  let head = row;
  let receipt: unknown;
  let savedPath = "";
  let writes = 0;
  let sent: { path: string; headers: Headers; input: unknown } | undefined;
  const onBack = vi.fn(),
    onClose = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return Response.json({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: ["iam.governance.operating.manage"],
        });
      if (init?.method === "PUT") {
        writes++;
        savedPath = url;
        const input = JSON.parse(String(init.body));
        sent = { path: url, headers: new Headers(init.headers), input };
        head = { ...row, status: "revoked", version: 4 };
        receipt = {
          id: url.split("/").at(-1),
          duty_id: row.id,
          input,
          result: head,
          actor_subject: scope.subject,
          created_at: row.created_at,
        };
        throw new TypeError("TEST committed response lost");
      }
      if (url === savedPath) return Response.json(receipt);
      if (url.includes("/duty-inbox?"))
        return Response.json({ data: [head], page: { has_more: false, next_cursor: null } });
      throw new Error(`Unexpected retained management query: ${url}`);
    }),
  );
  function Controlled() {
    const [busy, setBusy] = useState(false);
    return (
      <OperatingDutyManagement
        scope={scope}
        busy={busy}
        setBusy={setBusy}
        onBack={onBack}
        onClose={onClose}
      />
    );
  }
  renderWithProviders(
    <NavigationGuardProvider>
      <Controlled />
    </NavigationGuardProvider>,
  );
  await screen.findByText(row.input.reason);
  const user = userEvent.setup();
  await user.type(screen.getByLabelText("Decision reason"), "End the retained ineligible duty");
  await user.click(screen.getByRole("button", { name: "Revoke duty" }));
  const recover = await screen.findByRole("button", { name: "Recover duty decision" });
  expect(screen.getByRole("button", { name: "Choose reviewed Staff" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Change employer" })).toBeDisabled();
  await user.click(recover);
  await screen.findByText("Status: revoked", { exact: true });
  expect(writes).toBe(1);
  expect(sent?.path).toContain(
    `/iam/operating-governance-employers/supplier/${scope.employerId}/duties/${row.id}/commands/`,
  );
  expect(sent?.headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(sent?.headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  expect(sent?.input).toMatchObject({ decision: "revoke", expected_version: 3 });
  expect(onBack).not.toHaveBeenCalled();
  expect(onClose).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: "Choose reviewed Staff" })).toBeEnabled();
});
