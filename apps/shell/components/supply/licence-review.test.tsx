import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import userEvent from "@testing-library/user-event";
import { LicenceReview } from "./licence-review";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-maker",
  partyId: "0199e000-0000-7000-8000-000000000001",
  licenseId: "0199e000-0000-7000-8000-000000000002",
};
const licence = { id: scope.licenseId, version: 1, number: "TEST-LICENCE" };
afterEach(() => vi.unstubAllGlobals());
it("shows the effective decision and keeps auditors read-only despite broad global permissions", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify(
            url.endsWith("permissions")
              ? {
                  tenant_id: scope.tenantId,
                  subject: scope.subject,
                  data: [
                    "supply.license.read",
                    "supply.license.submit",
                    "supply.license.review",
                    "supply.evidence.read",
                  ],
                }
              : url.endsWith("/review")
                ? {
                    latest: null,
                    effective_status: "not_submitted",
                    trading_approved: false,
                    party_version: 1,
                    premise_version: 1,
                    license_version: 1,
                    allowed_actions: [],
                  }
                : { data: [], page: { has_more: false, next_cursor: null } },
          ),
        ),
    ),
  );
  HTMLElement.prototype.scrollIntoView = vi.fn();
  renderWithProviders(
    <LicenceReview scope={scope} licence={licence} roles={["auditor"]} onClose={() => {}} />,
  );
  expect(await screen.findByRole("heading", { name: "Licence review" })).toBeInTheDocument();
  expect(await screen.findByText("Not submitted")).toBeInTheDocument();
  expect(screen.getByText(/does not approve trading/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Submit for review" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Verify licence" })).not.toBeInTheDocument();
});

it("records an independent decision with source versions and retries the same command after refusal", async () => {
  const reviewerScope = { ...scope, subject: "TEST-reviewer" };
  const receipt = {
    id: "0199e000-0000-7000-8000-000000000003",
    party_id: scope.partyId,
    license_id: scope.licenseId,
    premise_id: "0199e000-0000-7000-8000-000000000004",
    version: 1,
    party_version: 1,
    premise_version: 1,
    license_version: 1,
    evidence_id: "0199e000-0000-7000-8000-000000000005",
    action: "submit",
    status: "submitted",
    reason: "TEST submission",
    method: "not_applicable",
    reference: "",
    actor_subject: "TEST-maker",
    created_at: "2026-10-16T00:00:00Z",
    updated_at: "2026-10-16T00:00:00Z",
  };
  let current = {
    latest: receipt,
    effective_status: "submitted",
    trading_approved: false,
    party_version: 1,
    premise_version: 1,
    license_version: 1,
    allowed_actions: ["verify", "reject"],
  };
  const writes: { url: string; body: Record<string, unknown> }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: reviewerScope.subject,
            data: [
              "supply.license.read",
              "supply.license.submit",
              "supply.license.review",
              "supply.evidence.read",
            ],
          }),
        );
      if (init?.method === "PUT") {
        const body = JSON.parse(init.body as string) as Record<string, unknown>;
        writes.push({ url, body });
        expect(new Headers(init.headers).get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
        if (writes.length === 1)
          return new Response(
            JSON.stringify({
              error: "conflict",
              message: "TEST review conflict. Refresh and retry.",
            }),
            { status: 409 },
          );
        const saved = {
          ...receipt,
          id: url.split("/").at(-1)!,
          version: 2,
          action: "verify",
          status: "verified",
          reason: String(body.reason),
          method: String(body.method),
          reference: String(body.reference),
          actor_subject: reviewerScope.subject,
        };
        current = {
          ...current,
          latest: saved,
          effective_status: "verified",
          allowed_actions: ["suspend"],
        };
        return new Response(JSON.stringify(saved));
      }
      return new Response(
        JSON.stringify(
          url.endsWith("/review")
            ? current
            : {
                data: url.endsWith("/reviews") ? [current.latest] : [],
                page: { has_more: false, next_cursor: null },
              },
        ),
      );
    }),
  );
  HTMLElement.prototype.scrollIntoView = vi.fn();
  renderWithProviders(
    <LicenceReview
      scope={reviewerScope}
      licence={licence}
      roles={["compliance"]}
      onClose={() => {}}
    />,
  );
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Record verification" }));
  await user.click(screen.getByRole("combobox", { name: "Independent verification method" }));
  await user.click(screen.getByRole("option", { name: "Direct authority contact" }));
  await user.type(screen.getByLabelText("Authority-check reference"), "TEST-REF-001");
  await user.type(screen.getByLabelText("Review reason"), "TEST independent authority check");
  await user.click(screen.getByRole("button", { name: "Verify licence" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("TEST review conflict");
  await user.click(screen.getByRole("button", { name: "Verify licence" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(writes[0].body).toMatchObject({
    expected_version: 1,
    expected_party_version: 1,
    expected_premise_version: 1,
    expected_license_version: 1,
    evidence_id: receipt.evidence_id,
    action: "verify",
  });
  expect(await screen.findByText("Verified")).toBeInTheDocument();
});

it("clears private review controls and offers a workspace refresh when the signed-in user changes", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      async (url: string) =>
        new Response(
          JSON.stringify(
            url.endsWith("permissions")
              ? {
                  tenant_id: scope.tenantId,
                  subject: scope.subject,
                  data: ["supply.license.read", "supply.license.submit"],
                }
              : {
                  error: "session_changed",
                  message: "Your signed-in user changed. Refresh the page before continuing.",
                },
          ),
          { status: url.endsWith("permissions") ? 200 : 409 },
        ),
    ),
  );
  HTMLElement.prototype.scrollIntoView = vi.fn();
  renderWithProviders(
    <LicenceReview scope={scope} licence={licence} roles={["administrator"]} onClose={() => {}} />,
  );
  expect(
    await screen.findByText("Your signed-in user changed. Refresh the page before continuing."),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Refresh workspace" })).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Prepare submission" })).not.toBeInTheDocument();
});
