import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { CountryPolicyWorkspace } from "./workspace";

const scopeId = "01a14400-0000-7000-8000-000000000002";
const versionId = "01a14400-0000-7000-8000-000000000001";
const tenant = "11111111-1111-1111-1111-111111111111";
afterEach(() => vi.unstubAllGlobals());

function fixture(
  subject: "TEST-author" | "TEST-reviewer",
  accessRole = subject === "TEST-author" ? "owner" : "reviewer",
) {
  HTMLElement.prototype.scrollIntoView = vi.fn();
  let approved = false;
  const posts: Record<string, unknown>[] = [];
  const scope = () => ({
    id: scopeId,
    country: "PK",
    jurisdiction: "TEST-PROVINCE",
    category: "otc",
    operation: "order",
    currency: "PKR",
    owner_subject: "TEST-author",
    access_role: accessRole,
    status: approved ? "active" : "unapproved",
    current_version: approved ? versionId : null,
    revision: approved ? 3 : 2,
    created_at: "2026-10-16T00:00:00Z",
    policy_decision: {
      allowed: approved,
      reason: approved ? "country_policy_approved" : "country_policy_not_approved",
    },
  });
  const summary = () => ({
    id: versionId,
    author: "TEST-author",
    state: approved ? "approved" : "submitted",
    effective_from: "2026-10-16T00:00:00Z",
    effective_until: "2027-10-16T00:00:00Z",
    created_at: "2026-10-16T00:00:00Z",
  });
  const version = () => ({
    ...summary(),
    scope: scope(),
    submitted_by: "TEST-author",
    approved_by: approved ? "TEST-reviewer" : "",
    approved_at: approved ? "2026-10-16T12:00:00Z" : "0001-01-01T00:00:00Z",
    sources: [
      {
        key: "TEST-source",
        title: "TEST ONLY fictional source",
        url: "https://regulator.example.invalid/test",
        reviewed_at: "2026-10-15T00:00:00Z",
      },
    ],
    requirements: [
      "licensing",
      "product_registration",
      "prescription",
      "controlled",
      "cold_chain",
      "tax_invoice",
      "credit_note",
      "retention",
      "privacy",
      "residency",
      "reporting",
    ].map((domain) => ({
      domain,
      applicability: "required",
      reason: "TEST ONLY reviewed requirement",
      source_keys: ["TEST-source"],
    })),
  });
  const page = (data: unknown[]) =>
    new Response(JSON.stringify({ data, page: { has_more: false, next_cursor: null } }));
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/iam/me/permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: tenant,
          subject,
          data: [
            "workspace.country_policy.read",
            "workspace.country_policy.review",
            ...(subject === "TEST-author" ? ["workspace.country_policy.manage"] : []),
          ],
        }),
      );
    if (init?.method === "POST") {
      const body = JSON.parse(String(init.body));
      posts.push(body);
      expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
      approved = body.action === "approved";
      return new Response(
        JSON.stringify({
          id: body.id,
          scope_id: scopeId,
          version_id: versionId,
          actor_subject: subject,
          action: body.action,
          reason: body.reason,
          expected_revision: body.expected_revision,
          result_revision: 3,
          created_at: "2026-10-16T12:00:00Z",
        }),
        { status: 202 },
      );
    }
    if (url.endsWith(`/versions/${versionId}`)) return new Response(JSON.stringify(version()));
    if (url.endsWith("/versions")) return page([summary()]);
    if (url.endsWith("/actions")) return page([]);
    if (url.endsWith(scopeId)) return new Response(JSON.stringify(scope()));
    return page([scope()]);
  });
  vi.stubGlobal("fetch", fetcher);
  return posts;
}

it("shows source evidence and blocks the author from self review", async () => {
  fixture("TEST-author");
  const { user } = renderWithProviders(<CountryPolicyWorkspace />);
  await user.click(await screen.findByRole("button", { name: /Pakistan · TEST-PROVINCE/ }));
  expect(
    await screen.findByText(/An independent reviewer must decide this version/),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Approve version" })).not.toBeInTheDocument();
  expect(
    screen.getByText(`Times shown in ${Intl.DateTimeFormat().resolvedOptions().timeZone}`),
  ).toBeInTheDocument();
  expect(
    screen.getByRole("link", { name: "https://regulator.example.invalid/test" }),
  ).toHaveAttribute("rel", "noreferrer noopener");
});

it("lets an explicitly delegated independent reviewer decide the policy", async () => {
  const posts = fixture("TEST-reviewer");
  const { user } = renderWithProviders(<CountryPolicyWorkspace />);
  await user.click(await screen.findByRole("button", { name: /Pakistan · TEST-PROVINCE/ }));
  await user.click(await screen.findByRole("button", { name: "Approve version" }));
  await user.type(screen.getByLabelText("Review reason"), "TEST ONLY independent review");
  await user.click(screen.getByRole("button", { name: "Approve policy" }));
  await waitFor(() => expect(posts).toHaveLength(1));
  expect(posts[0]).toMatchObject({
    action: "approved",
    version_id: versionId,
    expected_revision: 2,
    reason: "TEST ONLY independent review",
  });
  expect(await screen.findByText("Country policy is current")).toBeInTheDocument();
});

it("does not offer review actions when the scope grants only read access", async () => {
  fixture("TEST-reviewer", "reader");
  const { user } = renderWithProviders(<CountryPolicyWorkspace />);
  await user.click(await screen.findByRole("button", { name: /Pakistan · TEST-PROVINCE/ }));
  expect(
    await screen.findByRole("region", { name: "Reviewed policy version" }),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Approve version" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Manage policy access" })).not.toBeInTheDocument();
});
