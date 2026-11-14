import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { GovernanceInbox } from "./governance-inbox";

const tenant = "11111111-1111-4111-8111-111111111111";
const grant = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const notice = "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb";
const scope = { tenantId: tenant, subject: "synthetic-owner" };
const timestamp = "2026-11-13T05:00:00Z";
const row = {
  id: notice,
  tenant_id: tenant,
  grant_id: grant,
  grant_version: 1,
  recipient_subject: scope.subject,
  version: 1,
  created_at: timestamp,
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());

it("opens the current grant separately from its historical owner notice", async () => {
  const requests: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      requests.push(url);
      expect(new Headers(init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
      expect(new Headers(init?.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
      if (url.includes("/governance-notices?")) return Response.json(page([row]));
      return Response.json({
        id: grant,
        tenant_id: tenant,
        input: {
          subject: "synthetic-reviewer",
          expected_membership_epoch: 1,
          template_key: "iam.governance_auditor.v1",
          scope_kind: "tenant",
          reason: "Review governance decisions",
          valid_from: timestamp,
          expires_at: "2026-11-14T05:00:00Z",
        },
        status: "revoked",
        version: 3,
        issued_by: scope.subject,
        owner_count: 1,
        authorization_mode: "single_owner_provisional",
        created_at: timestamp,
        updated_at: timestamp,
      });
    }),
  );
  const { user } = renderWithProviders(<GovernanceInbox scope={scope} />);
  await user.click(await screen.findByRole("button", { name: "View current grant" }));
  expect(await screen.findByText("Revoked")).toBeInTheDocument();
  expect(screen.getByText("synthetic-reviewer")).toBeInTheDocument();
  expect(screen.getByText(/Notice recorded for grant version 1/)).toBeInTheDocument();
  expect(screen.getByText(/Current version 3/)).toBeInTheDocument();
  expect(requests.some((url) => url.endsWith("/iam/governance-grants/" + grant))).toBe(true);
});

it("discloses no notice from another owner", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(page([{ ...row, recipient_subject: "another-owner" }]))),
  );
  renderWithProviders(<GovernanceInbox scope={scope} />);
  expect(await screen.findByRole("alert")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "View current grant" })).not.toBeInTheDocument();
});

it("shows an empty inbox without claiming grants are effective", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json(page([]))),
  );
  renderWithProviders(<GovernanceInbox scope={scope} />);
  expect(await screen.findByText("No governance notices.")).toBeInTheDocument();
});

it("uses the returned cursor for the next owner page", async () => {
  const requests: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      requests.push(url);
      return Response.json(
        url.includes("cursor=")
          ? page([])
          : { data: [row], page: { has_more: true, next_cursor: "opaque+/cursor=" } },
      );
    }),
  );
  const { user } = renderWithProviders(<GovernanceInbox scope={scope} />);
  await user.click(await screen.findByRole("button", { name: "Next notices" }));
  await waitFor(() =>
    expect(requests.some((url) => url.includes("cursor=opaque%2B%2Fcursor%3D"))).toBe(true),
  );
  expect(await screen.findByText("No governance notices.")).toBeInTheDocument();
});

it("clears the prior owner notices when the signed-in account changes", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: string, init?: RequestInit) => {
      const subject = new Headers(init?.headers).get("X-Helivanta-Expected-Subject");
      return Response.json(subject === scope.subject ? page([row]) : page([]));
    }),
  );
  function SwitchingAccount() {
    const [subject, setSubject] = useState(scope.subject);
    return (
      <>
        <button onClick={() => setSubject("another-owner")}>Change account</button>
        <GovernanceInbox scope={{ ...scope, subject }} />
      </>
    );
  }
  const { user } = renderWithProviders(<SwitchingAccount />);
  await screen.findByRole("button", { name: "View current grant" });
  await user.click(screen.getByRole("button", { name: "Change account" }));
  expect(screen.queryByRole("button", { name: "View current grant" })).not.toBeInTheDocument();
  expect(await screen.findByText("No governance notices.")).toBeInTheDocument();
});
