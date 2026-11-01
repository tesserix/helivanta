import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { NavigationGuardProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import type { EmploymentPeriodDraft, EmploymentEvidenceBinding } from "@helivanta/api";
import { chooseOption } from "@/test/select";
import { EmploymentReviewPreparation } from "./preparation";
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
const terms: EmploymentPeriodDraft = {
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
const binding: EmploymentEvidenceBinding = {
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

afterEach(() => vi.unstubAllGlobals());
it.each([false, true])(
  "submits exact selected terms/evidence and preserves uncertain retry %s",
  async (uncertain) => {
    const writes: Record<string, unknown>[] = [];
    const onSubmitted = vi.fn();
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: RequestInfo | URL, init?: RequestInit) => {
        if (String(url).includes("employment-person-findings"))
          return Response.json({
            data: [
              {
                id: person,
                revision_id: personRevision,
                hospital_id: scope.hospitalId,
                staff_id: scope.staffId,
                version: 2,
                employment_epoch: 1,
                identity_hash: input.identity_hash,
                policy_reference: "SYN-IDENTITY",
                finding_reference: "Synthetic approved person",
                valid_until: "2030-01-01T00:00:00Z",
                access_intent: "non_digital",
                declared_subject: "",
                created_at: at,
              },
            ],
            page: { has_more: false, next_cursor: null },
          });
        const body = JSON.parse(String(init?.body));
        writes.push(body);
        if (uncertain && writes.length === 1)
          return Response.json(
            { error: "unavailable", message: "Synthetic uncertain submission" },
            { status: 503 },
          );
        const response = {
          ...row,
          ...body,
          revision_id: body.command_id,
          id: String(url).split("/").at(-1)?.split("?")[0],
          evidence: row.evidence,
        };
        delete response.command_id;
        return Response.json(response);
      }),
    );
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <EmploymentReviewPreparation
          scope={scope}
          terms={terms}
          evidence={[binding]}
          onClose={() => {}}
          onSubmitted={onSubmitted}
          onAccessLost={() => {}}
        />
      </NavigationGuardProvider>,
    );
    await screen.findByLabelText("Approved person finding");
    await chooseOption(
      user,
      screen.getByLabelText("Approved person finding"),
      "Synthetic approved person · SYN-IDENTITY",
    );
    await user.click(screen.getByLabelText("Select synthetic-appointment.png"));
    await user.type(screen.getByLabelText("Hospital employment policy"), "SYN-APPOINTMENT-POLICY");
    await user.type(screen.getByLabelText("Submission reason"), "Synthetic frozen appointment");
    await user.click(
      screen.getByLabelText("I examined these terms and the selected employment evidence."),
    );
    await user.click(
      screen.getByLabelText("I disclosed known associations with the applicant and contributors."),
    );
    await user.click(
      screen.getByRole("button", { name: "Submit for independent employment review" }),
    );
    await waitFor(() => expect(writes).toHaveLength(1));
    expect(writes[0]).toMatchObject({ ...input, command_id: expect.any(String) });
    if (uncertain) {
      const retry = await screen.findByRole("button", { name: "Retry same submission" });
      expect(screen.getByLabelText("Submission reason")).toBeDisabled();
      expect(screen.getByRole("button", { name: "Close preparation" })).toBeDisabled();
      await user.click(retry);
      await waitFor(() => expect(writes).toHaveLength(2));
      expect(writes[1]).toEqual(writes[0]);
    }
    await waitFor(() => expect(onSubmitted).toHaveBeenCalled());
  },
);
it("shows clear required policy and reason guidance before any write", async () => {
  const fetcher = vi.fn(async () =>
    Response.json({ data: [], page: { has_more: false, next_cursor: null } }),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <EmploymentReviewPreparation
        scope={scope}
        terms={terms}
        evidence={[binding]}
        onClose={() => {}}
        onSubmitted={() => {}}
        onAccessLost={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await user.click(
    await screen.findByRole("button", { name: "Submit for independent employment review" }),
  );
  expect(await screen.findByText("Enter the hospital employment policy reference.")).toBeVisible();
  expect(screen.getByText("Enter a reason for this submission.")).toBeVisible();
  expect(fetcher.mock.calls).toHaveLength(1);
});

it.each([401, 403, 404, 409])(
  "clears preparation after a scoped source cutoff %s",
  async (status) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json(
          { error: "forbidden", message: "Synthetic scoped finding cutoff" },
          { status },
        ),
      ),
    );
    const onAccessLost = vi.fn();
    renderWithProviders(
      <NavigationGuardProvider>
        <EmploymentReviewPreparation
          scope={scope}
          terms={terms}
          evidence={[binding]}
          onClose={() => {}}
          onSubmitted={() => {}}
          onAccessLost={onAccessLost}
        />
      </NavigationGuardProvider>,
    );
    expect(await screen.findByRole("alert")).toHaveTextContent("Review access changed.");
    expect(screen.queryByLabelText("Hospital employment policy")).not.toBeInTheDocument();
    await waitFor(() => expect(onAccessLost).toHaveBeenCalledTimes(1));
  },
);
