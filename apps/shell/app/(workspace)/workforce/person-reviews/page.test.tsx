import { chooseOption } from "@/test/select";
import { createHash } from "node:crypto";
import { NavigationGuardProvider } from "@helivanta/ui";
import { act, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { beginTenantTransition, cancelTenantTransition } from "@helivanta/api";
import PersonReviews from "./page";

const tenant = "11111111-1111-4111-8111-111111111111";
const hospital = "22222222-2222-4222-8222-222222222222";
const staff = "33333333-3333-4333-8333-333333333333";
const reviewId = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const imageBytes = Uint8Array.from(
  Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==",
    "base64",
  ),
);
const row = {
  id: reviewId,
  hospital_id: hospital,
  staff_id: staff,
  revision_id: "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb",
  version: 1,
  status: "pending",
  staff_version: 1,
  employment_epoch: 1,
  identity_hash: "a".repeat(64),
  policy_reference: "SYN-IDENTITY-POLICY",
  finding_reference: "Synthetic private human finding",
  observed_on: "2026-10-31",
  valid_until: "2030-01-01T00:00:00Z",
  access_intent: "non_digital",
  declared_subject: "",
  identity_examined: true,
  known_associations_disclosed: true,
  proposed_by_subject: "synthetic-manager",
  actor_subject: "synthetic-manager",
  decision_reason: "",
  reviewer_identity_examined: false,
  independent_person_confirmed: false,
  created_at: "2026-10-31T00:00:00Z",
  updated_at: "2026-10-31T00:00:00Z",
  evidence: [
    {
      document_id: "019ccccc-cccc-7ccc-8ccc-cccccccccccc",
      record_version: 1,
      filename: "synthetic-identity.png",
      content_type: "image/png",
      size: imageBytes.byteLength,
      sha256: createHash("sha256").update(imageBytes).digest("hex"),
      source_generation: 1,
      released_generation: 2,
      scan_attempt: 1,
      scanned_at: "2026-10-31T00:00:00Z",
      engine_version: "1.5.1",
      definition_version: 28000,
      definitions_updated_at: "2026-10-30T00:00:00Z",
      classification_version: 0,
      classification_revision_id: "",
    },
  ],
};
const summary = {
  id: row.id,
  hospital_id: hospital,
  staff_id: staff,
  employee_code: "SYN-JOINER",
  display_name: "Synthetic Joiner",
  status: "pending",
  version: 1,
  valid_until: row.valid_until,
  created_at: row.created_at,
};
const page = (data: unknown[], cursor: string | null = null) => ({
  data,
  page: { has_more: cursor !== null, next_cursor: cursor },
});
afterEach(() => {
  act(() => cancelTenantTransition());
  vi.unstubAllGlobals();
});

function fixture({
  uncertain = false,
  denial = 0,
  emptyFirst = false,
  self = false,
  holdEvidence = false,
  manager = false,
  recorded = "pending",
  changed = false,
  uncertainSubmission = false,
  selectionDenial = 0,
  emptyEvidence = false,
  newerHistory = false,
  historyDenial = 0,
} = {}) {
  const writes: { url: string; body: Record<string, unknown>; headers: Headers }[] = [];
  const reads: string[] = [];
  let done = false;
  let releaseEvidence!: () => void;
  const evidenceGate = new Promise<void>((resolve) => {
    releaseEvidence = resolve;
  });
  const subject = manager ? "synthetic-manager" : "synthetic-reviewer";
  const currentRow =
    recorded === "approved"
      ? {
          ...row,
          status: recorded,
          version: 2,
          actor_subject: "synthetic-reviewer",
          reviewer_identity_examined: true,
          independent_person_confirmed: true,
          decision_reason: "Synthetic prior independent review",
        }
      : row;
  const currentSummary = { ...summary, status: recorded, version: currentRow.version };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      reads.push(url);
      if (url.includes("/iam/me/permissions"))
        return Response.json({ tenant_id: tenant, subject, data: [] });
      if (url.includes("/person-review-scopes"))
        return Response.json(
          emptyFirst && !url.includes("cursor=")
            ? page([], "next-hospitals")
            : page([
                {
                  hospital_id: hospital,
                  name: "Synthetic Hospital",
                  can_submit: manager,
                  can_review: !manager,
                },
              ]),
        );
      if (url.includes("/person-review-targets/search"))
        return Response.json(
          page([
            {
              id: staff,
              hospital_id: hospital,
              employee_code: summary.employee_code,
              display_name: summary.display_name,
              employment_status: "inactive",
              staff_version: 1,
              created_at: row.created_at,
            },
          ]),
        );
      if (url.includes("/person-review-evidence/search") && selectionDenial)
        return Response.json(
          { error: "person_review_conflict", message: "Synthetic stale evidence basis" },
          { status: selectionDenial },
        );
      if (url.includes("/person-review-evidence/search") && emptyEvidence)
        return Response.json(page([]));
      if (url.includes("/person-review-evidence/search"))
        return Response.json(
          page([
            {
              id: row.evidence[0].document_id,
              hospital_id: hospital,
              staff_id: staff,
              record_version: 1,
              filename: row.evidence[0].filename,
              content_type: "image/png",
              size: imageBytes.byteLength,
              created_at: row.created_at,
            },
          ]),
        );
      if (url.includes(`/identity-staff/${staff}?`))
        return Response.json({
          hospital_id: hospital,
          staff_id: staff,
          employee_code: summary.employee_code,
          display_name: summary.display_name,
          given_name: "Synthetic",
          family_name: "Joiner",
          date_of_birth: "1990-01-01",
          employment_status: "inactive",
          staff_version: 1,
          employment_epoch: 1,
          identity_hash: row.identity_hash,
          manage_checks_allowed: true,
        });
      if (init?.method === "PUT" && url.includes("/person-reviews/")) {
        const body = JSON.parse(String(init.body));
        writes.push({ url, body, headers: new Headers(init.headers) });
        if (uncertainSubmission && writes.length === 1)
          return Response.json(
            { error: "unavailable", message: "Synthetic uncertain submission" },
            { status: 502 },
          );
        const { command_id, document_ids, ...finding } = body;
        return Response.json({
          ...row,
          ...finding,
          id: url.split("/").at(-1),
          revision_id: command_id,
          proposed_by_subject: subject,
          actor_subject: subject,
          evidence: row.evidence.filter((file) => document_ids.includes(file.document_id)),
        });
      }
      if (url.includes("/person-review-inbox")) {
        const status =
          new URL(url, "https://synthetic.example.invalid").searchParams.get("status") ?? "pending";
        return Response.json(page(done || status !== recorded ? [] : [currentSummary]));
      }
      if (url.includes(`/person-reviews/${reviewId}/history?`)) {
        if (historyDenial)
          return Response.json(
            { error: "person_review_conflict", message: "Synthetic history access changed" },
            { status: historyDenial },
          );
        if (newerHistory && !url.includes("cursor="))
          return Response.json(
            page(
              [
                {
                  ...row,
                  status: "approved",
                  version: 2,
                  revision_id: "019ddddd-dddd-7ddd-8ddd-dddddddddddd",
                  actor_subject: "synthetic-reviewer",
                  reviewer_identity_examined: true,
                  independent_person_confirmed: true,
                  decision_reason: "Synthetic newer receipt",
                  updated_at: "2026-10-31T01:00:00Z",
                },
              ],
              "older-history",
            ),
          );
        return Response.json(page([currentRow]));
      }
      if (url.endsWith("/content")) {
        if (holdEvidence) await evidenceGate;
        return new Response(imageBytes, {
          headers: { "Content-Type": "image/png", "Content-Length": String(imageBytes.byteLength) },
        });
      }
      if (/\/(decision|withdrawal|revocation)$/.test(url)) {
        const body = JSON.parse(String(init?.body));
        writes.push({ url, body, headers: new Headers(init?.headers) });
        if (denial || (uncertain && writes.length === 1))
          return Response.json(
            { error: "synthetic_failure", message: "Synthetic decision unavailable" },
            { status: denial || 502 },
          );
        done = true;
        return Response.json({
          ...currentRow,
          status: url.endsWith("/withdrawal")
            ? "withdrawn"
            : url.endsWith("/revocation")
              ? "revoked"
              : body.decision === "approve"
                ? "approved"
                : "rejected",
          version: body.expected_version + 1,
          revision_id: body.command_id,
          actor_subject: subject,
          decision_reason: body.reason,
          reviewer_identity_examined:
            body.identity_examined ?? currentRow.reviewer_identity_examined,
          independent_person_confirmed:
            body.independent_person_confirmed ?? currentRow.independent_person_confirmed,
        });
      }
      if (url.endsWith(`/person-reviews/${reviewId}`))
        return Response.json({
          review: { ...currentRow, ...(self ? { proposed_by_subject: subject } : {}) },
          current_state: changed ? "source_changed" : recorded,
        });
      throw new Error(`Unexpected request ${url}`);
    }),
  );
  return { writes, reads, releaseEvidence };
}

it("requires an independent human finding before approving through the scoped reviewer inbox", async () => {
  const { writes, reads } = fixture();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open review for Synthetic Joiner" }));
  const detail = await screen.findByRole("region", { name: "Person review details" });
  expect(await within(detail).findByText(row.finding_reference)).toBeInTheDocument();
  expect(within(detail).getByText("synthetic-identity.png")).toBeInTheDocument();
  await user.click(within(detail).getByRole("button", { name: "Approve finding" }));
  expect(writes).toHaveLength(0);
  await user.type(
    within(detail).getByLabelText("Decision reason"),
    "Synthetic independent examination",
  );
  await user.click(
    within(detail).getByLabelText(
      "I independently examined the identity evidence under this policy.",
    ),
  );
  await user.click(
    within(detail).getByLabelText("I am independent of the applicant and all contributors."),
  );
  await user.click(within(detail).getByRole("button", { name: "Approve finding" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0].body).toMatchObject({
    expected_version: 1,
    decision: "approve",
    reason: "Synthetic independent examination",
    identity_examined: true,
    independent_person_confirmed: true,
  });
  expect(writes[0].headers.get("X-Helivanta-Expected-Tenant")).toBe(tenant);
  expect(writes[0].headers.get("X-Helivanta-Expected-Subject")).toBe("synthetic-reviewer");
  expect(await screen.findByText(/Recorded receipt: approved/)).toBeInTheDocument();
  expect(
    reads.some((url) => url.includes("/documents") || url.includes("/person-review-targets")),
  ).toBe(false);
});

it("preserves edits until the user confirms closing the review", async () => {
  fixture();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open review for Synthetic Joiner" }));
  const detail = await screen.findByRole("region", { name: "Person review details" });
  await user.type(within(detail).getByLabelText("Decision reason"), "Synthetic unsaved decision");
  expect(screen.getByRole("button", { name: "Refresh inbox" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Refresh access" })).toBeDisabled();
  await user.click(within(detail).getByRole("button", { name: "Close review" }));
  const confirmation = await screen.findByRole("dialog");
  await user.click(within(confirmation).getByRole("button", { name: "Keep editing" }));
  expect(within(detail).getByLabelText("Decision reason")).toHaveValue(
    "Synthetic unsaved decision",
  );
});

async function completeFinding(user: ReturnType<typeof renderWithProviders>["user"]) {
  await user.click(await screen.findByRole("button", { name: "Open review for Synthetic Joiner" }));
  const detail = await screen.findByRole("region", { name: "Person review details" });
  await user.type(within(detail).getByLabelText("Decision reason"), "Synthetic independent review");
  await user.click(
    within(detail).getByLabelText(
      "I independently examined the identity evidence under this policy.",
    ),
  );
  await user.click(
    within(detail).getByLabelText("I am independent of the applicant and all contributors."),
  );
  return detail;
}

it("retries an uncertain decision with the exact original command while navigation stays locked", async () => {
  const { writes } = fixture({ uncertain: true });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  const detail = await completeFinding(user);
  await user.click(within(detail).getByRole("button", { name: "Approve finding" }));
  const retry = await within(detail).findByRole("button", { name: "Retry same decision" });
  expect(within(detail).getByLabelText("Decision reason")).toBeDisabled();
  expect(within(detail).getByRole("button", { name: "Close review" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Refresh access" })).toBeDisabled();
  await user.click(retry);
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1].body).toEqual(writes[0].body);
  expect(writes[1].headers).toEqual(writes[0].headers);
  expect(await screen.findByText(/Recorded receipt: approved/)).toBeInTheDocument();
});

it.each([401, 403, 404, 409])(
  "clears private findings after a %s decision refusal",
  async (denial) => {
    fixture({ denial });
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <PersonReviews />
      </NavigationGuardProvider>,
    );
    const detail = await completeFinding(user);
    await user.click(within(detail).getByRole("button", { name: "Approve finding" }));
    expect(await screen.findByText(/Your access or workspace changed/)).toBeInTheDocument();
    expect(screen.queryByText(row.finding_reference)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retry same decision" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Refresh access" })).toBeEnabled();
  },
);

it("continues hospital discovery after an empty filtered page", async () => {
  fixture({ emptyFirst: true });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Next hospitals" }));
  expect(
    await screen.findByRole("button", { name: "Open review for Synthetic Joiner" }),
  ).toBeInTheDocument();
});

it("hides old private details as soon as a tenant transition starts", async () => {
  fixture();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open review for Synthetic Joiner" }));
  expect(await screen.findByText(row.finding_reference)).toBeInTheDocument();
  act(() => beginTenantTransition());
  expect(screen.queryByText(row.finding_reference)).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Decision reason")).not.toBeInTheDocument();
});

it("does not offer a self-review decision", async () => {
  fixture({ self: true });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open review for Synthetic Joiner" }));
  expect(
    await screen.findByText("An independent reviewer must decide this finding."),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Approve finding" })).not.toBeInTheDocument();
});

it("allows correction after a definitive invalid-request refusal", async () => {
  const { writes } = fixture({ denial: 400 });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  const detail = await completeFinding(user);
  await user.click(within(detail).getByRole("button", { name: "Approve finding" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  await within(detail).findByRole("alert");
  expect(within(detail).getByLabelText("Decision reason")).toBeEnabled();
  expect(within(detail).getByRole("button", { name: "Close review" })).toBeEnabled();
});

it("downloads verified purpose-bound evidence without a generic document request", async () => {
  const { reads } = fixture();
  const create = vi.fn<(blob: Blob) => string>(() => "blob:synthetic-evidence");
  const revoke = vi.fn();
  vi.stubGlobal(
    "URL",
    Object.assign(class extends URL {}, { createObjectURL: create, revokeObjectURL: revoke }),
  );
  const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
  try {
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <PersonReviews />
      </NavigationGuardProvider>,
    );
    await user.click(
      await screen.findByRole("button", { name: "Open review for Synthetic Joiner" }),
    );
    await user.click(
      await screen.findByRole("button", { name: "Download synthetic-identity.png" }),
    );
    await waitFor(() => expect(create).toHaveBeenCalledTimes(1));
    expect(create.mock.calls[0][0]).toBeInstanceOf(Blob);
    expect(click).toHaveBeenCalledTimes(1);
    await waitFor(() => expect(revoke).toHaveBeenCalledWith("blob:synthetic-evidence"));
    expect(
      reads.some(
        (url) => url.includes(`/person-reviews/${reviewId}/evidence/`) && url.endsWith("/content"),
      ),
    ).toBe(true);
    expect(reads.some((url) => url.includes("/documents/"))).toBe(false);
  } finally {
    click.mockRestore();
  }
});

it("does not publish evidence bytes arriving after a workspace transition", async () => {
  const { reads, releaseEvidence } = fixture({ holdEvidence: true });
  const create = vi.fn(() => "blob:synthetic-stale-evidence");
  vi.stubGlobal(
    "URL",
    Object.assign(class extends URL {}, { createObjectURL: create, revokeObjectURL: vi.fn() }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open review for Synthetic Joiner" }));
  await user.click(await screen.findByRole("button", { name: "Download synthetic-identity.png" }));
  await waitFor(() => expect(reads.some((url) => url.endsWith("/content"))).toBe(true));
  act(() => beginTenantTransition());
  await act(async () => {
    releaseEvidence();
    await Promise.resolve();
  });
  expect(create).not.toHaveBeenCalled();
  expect(screen.queryByText(row.finding_reference)).not.toBeInTheDocument();
});

it("records a rejection with an independent reason without asserting successful examination", async () => {
  const { writes } = fixture();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open review for Synthetic Joiner" }));
  const detail = await screen.findByRole("region", { name: "Person review details" });
  await user.type(
    within(detail).getByLabelText("Decision reason"),
    "Synthetic evidence did not establish identity",
  );
  await user.click(
    within(detail).getByLabelText("I am independent of the applicant and all contributors."),
  );
  await user.click(within(detail).getByRole("button", { name: "Reject finding" }));
  expect(await screen.findByText(/Recorded receipt: rejected/)).toBeInTheDocument();
  expect(writes[0].body).toMatchObject({
    decision: "reject",
    identity_examined: false,
    independent_person_confirmed: true,
  });
});

it("lets the submitting identity manager withdraw their pending finding", async () => {
  const { writes } = fixture({ manager: true });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open review for Synthetic Joiner" }));
  const detail = await screen.findByRole("region", { name: "Person review details" });
  expect(within(detail).queryByRole("button", { name: "Approve finding" })).not.toBeInTheDocument();
  await user.type(
    within(detail).getByLabelText("Decision reason"),
    "Synthetic applicant withdrew request",
  );
  await user.click(within(detail).getByRole("button", { name: "Withdraw submission" }));
  expect(await screen.findByText(/Recorded receipt: withdrawn/)).toBeInTheDocument();
  expect(writes[0].body).toMatchObject({
    expected_version: 1,
    reason: "Synthetic applicant withdrew request",
  });
  expect(writes[0].body).not.toHaveProperty("decision");
});

it("allows revocation after the identity source changes without releasing stale evidence", async () => {
  const { writes } = fixture({ recorded: "approved", changed: true });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await chooseOption(user, await screen.findByLabelText("Recorded status"), "Approved findings");
  await user.click(await screen.findByRole("button", { name: "Open review for Synthetic Joiner" }));
  const detail = await screen.findByRole("region", { name: "Person review details" });
  expect(
    within(detail).getByRole("button", { name: "Download synthetic-identity.png" }),
  ).toBeDisabled();
  await user.type(
    within(detail).getByLabelText("Decision reason"),
    "Synthetic corrected identity invalidated this finding",
  );
  await user.click(within(detail).getByRole("button", { name: "Revoke finding" }));
  expect(await screen.findByText(/Recorded receipt: revoked/)).toBeInTheDocument();
  expect(writes[0].body).toMatchObject({
    expected_version: 2,
    reason: "Synthetic corrected identity invalidated this finding",
  });
});

it("explains missing finding fields without exposing schema terminology", async () => {
  const { writes } = fixture({ manager: true });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await user.click(
    await screen.findByRole("button", { name: "Prepare review for Synthetic Joiner" }),
  );
  const preparation = await screen.findByRole("region", { name: "Prepare person review" });
  await user.click(
    within(preparation).getByRole("button", { name: "Submit for independent review" }),
  );
  expect(await within(preparation).findByText("Enter a hospital policy reference.")).toBeVisible();
  expect(
    within(preparation).getByText("Enter an observation and finding reference."),
  ).toBeVisible();
  expect(within(preparation).getByText("Enter the observation date in UTC.")).toBeVisible();
  expect(
    within(preparation).getByText("Enter a valid finding expiry date and time."),
  ).toBeVisible();
  expect(within(preparation).queryByText(/Too small|Invalid ISO/)).not.toBeInTheDocument();
  expect(writes).toHaveLength(0);
});

it("prepares an inactive applicant with mandatory source-bound evidence and human attestations", async () => {
  const { writes, reads } = fixture({ manager: true });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await user.click(
    await screen.findByRole("button", { name: "Prepare review for Synthetic Joiner" }),
  );
  const preparation = await screen.findByRole("region", { name: "Prepare person review" });
  const submit = await within(preparation).findByRole("button", {
    name: "Submit for independent review",
  });
  await user.click(submit);
  expect(writes).toHaveLength(0);
  await user.type(
    within(preparation).getByLabelText("Hospital policy reference"),
    "SYN-PERSON-POLICY",
  );
  await user.type(
    within(preparation).getByLabelText("Observation and finding reference"),
    "Synthetic human physical identity examination",
  );
  await user.type(within(preparation).getByLabelText("Observation date (UTC)"), "2026-10-30");
  await user.type(
    within(preparation).getByLabelText("Finding valid until (local time)"),
    "2030-01-01T12:00",
  );
  await user.click(within(preparation).getByRole("checkbox", { name: /synthetic-identity\.png/ }));
  await user.click(
    within(preparation).getByLabelText(
      "I examined this person’s identity evidence under the recorded policy.",
    ),
  );
  await user.click(
    within(preparation).getByLabelText(
      "I disclosed all known staff and account associations for this person.",
    ),
  );
  await user.click(submit);
  expect(
    await screen.findByText(/Person finding submitted for independent review/),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(1);
  expect(writes[0].body).toMatchObject({
    staff_version: 1,
    employment_epoch: 1,
    identity_hash: row.identity_hash,
    access_intent: "non_digital",
    declared_subject: "",
    document_ids: [row.evidence[0].document_id],
    identity_examined: true,
    known_associations_disclosed: true,
  });
  expect(reads.some((url) => url.includes("/documents"))).toBe(false);
});

async function fillPreparation(user: ReturnType<typeof renderWithProviders>["user"]) {
  await user.click(
    await screen.findByRole("button", { name: "Prepare review for Synthetic Joiner" }),
  );
  const preparation = await screen.findByRole("region", { name: "Prepare person review" });
  await user.type(
    within(preparation).getByLabelText("Hospital policy reference"),
    "SYN-PERSON-POLICY",
  );
  await user.type(
    within(preparation).getByLabelText("Observation and finding reference"),
    "Synthetic human physical identity examination",
  );
  await user.type(within(preparation).getByLabelText("Observation date (UTC)"), "2026-10-30");
  await user.type(
    within(preparation).getByLabelText("Finding valid until (local time)"),
    "2030-01-01T12:00",
  );
  await user.click(within(preparation).getByRole("checkbox", { name: /synthetic-identity\.png/ }));
  await user.click(
    within(preparation).getByLabelText(
      "I examined this person’s identity evidence under the recorded policy.",
    ),
  );
  await user.click(
    within(preparation).getByLabelText(
      "I disclosed all known staff and account associations for this person.",
    ),
  );

  return preparation;
}

it("retries an uncertain submission with the same finding ID, source and command", async () => {
  const { writes } = fixture({ manager: true, uncertainSubmission: true });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  const preparation = await fillPreparation(user);
  await user.click(
    within(preparation).getByRole("button", { name: "Submit for independent review" }),
  );
  const retry = await within(preparation).findByRole("button", { name: "Retry same submission" });
  expect(within(preparation).getByLabelText("Hospital policy reference")).toBeDisabled();
  expect(within(preparation).getByRole("button", { name: "Close preparation" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Refresh access" })).toBeDisabled();
  await user.click(retry);
  expect(
    await screen.findByText(/Person finding submitted for independent review/),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(2);
  expect(writes[1].body).toEqual(writes[0].body);
  expect(writes[1].headers).toEqual(writes[0].headers);
  expect(writes[1].url).toBe(writes[0].url);
  expect(Date.parse(String(writes[0].body.valid_until))).toBe(
    new Date("2030-01-01T12:00").getTime(),
  );
});

it("clears private identity details after the evidence basis becomes stale", async () => {
  const { writes } = fixture({ manager: true, selectionDenial: 409 });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await user.click(
    await screen.findByRole("button", { name: "Prepare review for Synthetic Joiner" }),
  );
  expect(await screen.findByText(/Your access or workspace changed/)).toBeInTheDocument();
  expect(screen.queryByText("1990-01-01")).not.toBeInTheDocument();
  expect(screen.queryByLabelText("Hospital policy reference")).not.toBeInTheDocument();
  expect(writes).toHaveLength(0);
});

it("does not replace missing released evidence with an unbound manual file ID", async () => {
  const { writes } = fixture({ manager: true, emptyEvidence: true });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await user.click(
    await screen.findByRole("button", { name: "Prepare review for Synthetic Joiner" }),
  );
  const preparation = await screen.findByRole("region", { name: "Prepare person review" });
  expect(
    await within(preparation).findByText(/No released identity evidence is available/),
  ).toBeInTheDocument();
  expect(within(preparation).queryByLabelText(/file ID/i)).not.toBeInTheDocument();
  await user.click(
    within(preparation).getByRole("button", { name: "Submit for independent review" }),
  );
  expect(writes).toHaveLength(0);
});

it("shows protected recorded receipts separately from present effectiveness", async () => {
  const { reads } = fixture();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open review for Synthetic Joiner" }));
  const detail = await screen.findByRole("region", { name: "Person review details" });
  await user.click(within(detail).getByRole("button", { name: "View history" }));
  const history = await screen.findByRole("region", { name: "Recorded person review history" });
  expect(await within(history).findByText("Account synthetic-manager")).toBeInTheDocument();
  expect(
    within(history).getByText(/Historical receipts do not establish present effectiveness/),
  ).toBeInTheDocument();
  expect(reads.some((url) => url.includes(`/person-reviews/${reviewId}/history?`))).toBe(true);
});

it("requires reopening after a newer historical receipt and retains that guard across history pages", async () => {
  fixture({ newerHistory: true });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open review for Synthetic Joiner" }));
  const detail = await screen.findByRole("region", { name: "Person review details" });
  await user.click(within(detail).getByRole("button", { name: "View history" }));
  expect(await within(detail).findByText("Synthetic newer receipt")).toBeInTheDocument();
  expect(within(detail).getByRole("button", { name: "Approve finding" })).toBeDisabled();
  await user.click(within(detail).getByRole("button", { name: "Next history receipts" }));
  expect(await within(detail).findByText("Account synthetic-manager")).toBeInTheDocument();
  expect(within(detail).getByRole("button", { name: "Approve finding" })).toBeDisabled();
  expect(within(detail).getByText(/A newer receipt was recorded/)).toBeInTheDocument();
});

it("clears private detail when history access is refused", async () => {
  fixture({ historyDenial: 403 });
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <PersonReviews />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Open review for Synthetic Joiner" }));
  const detail = await screen.findByRole("region", { name: "Person review details" });
  await user.click(within(detail).getByRole("button", { name: "View history" }));
  expect(await screen.findByText(/Your access or workspace changed/)).toBeInTheDocument();
  expect(screen.queryByText(row.finding_reference)).not.toBeInTheDocument();
});
