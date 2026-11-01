import { act, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { NavigationGuardProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import { cancelTenantTransition } from "@helivanta/api";
import EmploymentReviews from "./page";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
  subject: "synthetic-employment-preparer",
  hrIntake: true as const,
};
const id = "019a0000-0000-7000-8000-000000000001";
const command = "019a0000-0000-7000-8000-000000000002";
const period = "019a0000-0000-7000-8000-000000000003";
const revision = "019a0000-0000-7000-8000-000000000004";
const person = "019a0000-0000-7000-8000-000000000005";
const personRevision = "019a0000-0000-7000-8000-000000000006";
const bindingId = "019a0000-0000-7000-8000-000000000007";
const bindingRevision = "019a0000-0000-7000-8000-000000000008";
const file = "019a0000-0000-7000-8000-000000000009";
const at = "2026-10-31T00:00:00Z";
const input = {
  command_id: command,
  staff_version: 1,
  employment_epoch: 1,
  identity_hash: "a".repeat(64),
  period_id: period,
  period_revision_id: revision,
  period_version: 1,
  person_review_id: person,
  person_review_revision_id: personRevision,
  person_review_version: 2,
  evidence: [{ id: bindingId, revision_id: bindingRevision, version: 1 }],
  policy_reference: "SYN-APPOINTMENT-POLICY",
  reason: "Synthetic frozen appointment",
  terms_examined: true,
  known_associations_disclosed: true,
};
const terms = {
  id: period,
  revision_id: revision,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  version: 1,
  status: "draft",
  staff_version: 1,
  employment_epoch: 1,
  engagement_kind: "employee",
  duration: "ongoing",
  starts_at: at,
  contracting_party: "",
  sponsor_id: "",
  reason: "Synthetic appointment terms",
  evidence_reference: "SYN-APPOINTMENT",
  proposed_by_subject: scope.subject,
  actor_subject: scope.subject,
  created_at: at,
  updated_at: at,
};
const binding = {
  id: bindingId,
  revision_id: bindingRevision,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  period_id: period,
  period_revision_id: revision,
  period_version: 1,
  version: 1,
  status: "attached",
  staff_version: 1,
  employment_epoch: 1,
  document_id: file,
  document_record_version: 1,
  filename: "synthetic-appointment.png",
  content_type: "image/png",
  size: 88,
  sha256: "b".repeat(64),
  source_generation: "10",
  released_generation: "11",
  scan_attempt: 1,
  scanned_at: at,
  proposed_by_subject: scope.subject,
  actor_subject: scope.subject,
  reason: "Synthetic selected evidence",
  created_at: at,
  updated_at: at,
};
const { command_id, ...source } = input;
const row = {
  ...source,
  id,
  revision_id: command_id,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  version: 1,
  status: "pending",
  access_intent: "non_digital",
  declared_subject: "",
  proposed_by_subject: scope.subject,
  actor_subject: scope.subject,
  decision_reason: "",
  reviewer_terms_examined: false,
  independent_person_confirmed: false,
  terms,
  evidence: [{ binding, classification_version: 0, classification_revision_id: "" }],
  created_at: at,
  updated_at: at,
};

const summary = {
  id: row.id,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  employee_code: "SYN-TERMS",
  display_name: "Synthetic Joiner",
  status: "pending",
  version: 1,
  created_at: at,
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => {
  act(() => cancelTenantTransition());
  vi.unstubAllGlobals();
});
function fixture({ denial = 0, uncertain = false, canApprove = true } = {}) {
  const writes: { url: string; body: Record<string, unknown> }[] = [];
  const reads: string[] = [];
  let done = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      reads.push(url);
      if (url.includes("/iam/me/permissions"))
        return Response.json({
          tenant_id: scope.tenantId,
          subject: "synthetic-employment-reviewer",
          data: [],
        });
      if (url.includes("/employment-review-scopes"))
        return Response.json(
          page([
            {
              hospital_id: scope.hospitalId,
              name: "Synthetic Hospital",
              can_submit: false,
              can_review: true,
            },
          ]),
        );
      if (url.includes("/employment-review-inbox"))
        return Response.json(page(done ? [] : [summary]));
      if (url.includes("/decision")) {
        const body = JSON.parse(String(init?.body));
        writes.push({ url, body });
        if (uncertain && writes.length === 1)
          return Response.json(
            { error: "unavailable", message: "Synthetic uncertain result" },
            { status: 503 },
          );
        done = true;
        return Response.json({
          ...row,
          revision_id: body.command_id,
          version: 2,
          status: body.decision,
          actor_subject: "synthetic-employment-reviewer",
          decision_reason: body.reason,
          reviewer_terms_examined: body.terms_examined,
          independent_person_confirmed: body.independent_person_confirmed,
        });
      }
      if (url.includes("/employment-reviews/")) {
        if (denial)
          return Response.json(
            { error: "forbidden", message: "Synthetic review cutoff" },
            { status: denial },
          );
        return Response.json({
          review: row,
          current_state: "pending",
          can_approve: canApprove,
          can_reject: canApprove,
          can_withdraw: false,
          can_revoke: false,
        });
      }
      throw new Error(`Unexpected request: ${url}`);
    }),
  );
  return { writes, reads };
}
async function open() {
  const result = renderWithProviders(
    <NavigationGuardProvider>
      <EmploymentReviews />
    </NavigationGuardProvider>,
  );
  await result.user.click(
    await screen.findByRole("button", { name: "Open review for Synthetic Joiner" }),
  );
  return result;
}
it("opens a scoped employment inbox and records an independently attested approval", async () => {
  const { writes, reads } = fixture();
  const { user } = await open();
  expect(await screen.findByText("Synthetic appointment terms")).toBeVisible();
  expect(screen.getByText(/does not activate employment/)).toBeVisible();
  await user.type(
    screen.getByLabelText("Decision reason"),
    "Synthetic independent appointment approval",
  );
  await user.click(
    screen.getByLabelText(
      "I independently examined the employment terms and selected evidence under this policy.",
    ),
  );
  await user.click(
    screen.getByLabelText("I am independent of the applicant and all contributors."),
  );
  await user.click(screen.getByRole("button", { name: "Approve terms" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0].body).toMatchObject({
    decision: "approve",
    expected_version: 1,
    terms_examined: true,
    independent_person_confirmed: true,
  });
  expect(writes[0].url).toContain("employment_review=v1");
  expect(reads.some((url) => url.includes("/employment-source"))).toBe(false);
});
it.each([401, 403, 404, 409])(
  "clears private review detail after access cutoff %s",
  async (denial) => {
    const { writes } = fixture({ denial });
    await open();
    expect(await screen.findByRole("alert")).toHaveTextContent(/access|workspace/i);
    expect(screen.queryByText("Synthetic appointment terms")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Approve terms" })).not.toBeInTheDocument();
    expect(writes).toHaveLength(0);
  },
);
it("requires the reason and both human attestations", async () => {
  const { writes } = fixture();
  const { user } = await open();
  await user.click(await screen.findByRole("button", { name: "Approve terms" }));
  expect(writes).toHaveLength(0);
  expect(await screen.findByText("Enter a reason for this decision.")).toBeVisible();
  await user.type(screen.getByLabelText("Decision reason"), "Synthetic review");
  await user.click(screen.getByRole("button", { name: "Approve terms" }));
  expect(
    await screen.findByText("Confirm your independence before recording a decision."),
  ).toBeVisible();
  await user.click(
    screen.getByLabelText("I am independent of the applicant and all contributors."),
  );
  await user.click(screen.getByRole("button", { name: "Approve terms" }));
  expect(await screen.findByText("Confirm your examination before approving.")).toBeVisible();
  expect(writes).toHaveLength(0);
});
it("honors the owner's current decision capabilities", async () => {
  fixture({ canApprove: false });
  await open();
  expect(await screen.findByText("Synthetic appointment terms")).toBeVisible();
  expect(screen.queryByRole("button", { name: "Approve terms" })).not.toBeInTheDocument();
});
it("freezes an uncertain command and retries its exact original bytes", async () => {
  const { writes } = fixture({ uncertain: true });
  const { user } = await open();
  await user.type(
    await screen.findByLabelText("Decision reason"),
    "Synthetic independent appointment approval",
  );
  await user.click(
    screen.getByLabelText(
      "I independently examined the employment terms and selected evidence under this policy.",
    ),
  );
  await user.click(
    screen.getByLabelText("I am independent of the applicant and all contributors."),
  );
  await user.click(screen.getByRole("button", { name: "Approve terms" }));
  await user.click(await screen.findByRole("button", { name: "Retry same decision" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]).toEqual(writes[0]);
});
