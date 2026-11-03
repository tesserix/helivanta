import { useState, type ReactElement } from "react";
import { NavigationGuardProvider } from "@helivanta/ui";
import "@/test/select";
import { renderWithProviders } from "@helivanta/api/testing";
import { act, fireEvent, screen } from "@testing-library/react";
import { StaffPostingReviewWorkspace } from "./posting-review-workspace";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-posting-reviewer",
};
const hospital = "22222222-2222-4222-8222-222222222222";
const staff = "33333333-3333-4333-8333-333333333333";
const id = (n: number) => `019a0000-0000-7000-8000-${String(n).padStart(12, "0")}`;
const proposal = {
  id: id(1),
  revision_id: id(2),
  command_id: id(3),
  version: 1,
  status: "pending",
  grants_access: false,
  hospital_id: hospital,
  staff_id: staff,
  draft_id: id(4),
  draft_revision_id: id(5),
  draft_version: 1,
  generation_id: id(6),
  staff_version: 2,
  employment_epoch: 2,
  department_id: id(7),
  department_revision_id: id(8),
  starts_at: "2026-11-04T00:00:00Z",
  ends_at: null,
  eligibility_until: "2026-11-05T00:00:00Z",
  created_at: "2026-11-03T00:00:00Z",
  recorded_at: "2026-11-03T00:00:00Z",
};
const context = {
  purpose: "review",
  proposal,
  draft: {
    id: id(4),
    revision_id: id(5),
    version: 1,
    hospital_id: hospital,
    staff_id: staff,
    staff_version: 2,
    employment_epoch: 2,
    department_id: id(7),
    department_revision_id: id(8),
    starts_at: proposal.starts_at,
    ends_at: null,
    status: "draft",
    grants_access: false,
    created_at: proposal.created_at,
    recorded_at: proposal.recorded_at,
  },
  draft_reason: "Synthetic placement rationale",
  submission_reason: "Synthetic review request",
  decision_reason: null,
};
const source = {
  hospital_id: hospital,
  staff_id: staff,
  staff_version: 2,
  employment_epoch: 2,
  generation_id: proposal.generation_id,
  generation_state: "current",
  starts_at: proposal.starts_at,
  ends_at: null,
  eligibility_until: proposal.eligibility_until,
  checked_at: "2026-11-04T00:00:00Z",
};
function renderReview(ui: ReactElement) {
  return renderWithProviders(<NavigationGuardProvider>{ui}</NavigationGuardProvider>);
}
afterEach(() => vi.unstubAllGlobals());

it("discovers reviewer hospitals and opens only the selected exact-head protected context", async () => {
  const paths: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      paths.push(url);
      if (url.endsWith("posting-review-source")) return Response.json(source);
      if (url.includes("posting-review-scopes"))
        return Response.json({
          data: [
            {
              hospital_id: hospital,
              name: "Synthetic Hospital",
              can_submit: false,
              can_review: true,
            },
          ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.includes("posting-proposal-inbox"))
        return Response.json({
          data: [
            {
              proposal,
              staff: { id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" },
              is_submitter: false,
            },
          ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.includes("/context?")) return Response.json(context);
      throw new Error(`Unexpected request: ${url}`);
    }),
  );
  const { user } = renderReview(
    <StaffPostingReviewWorkspace scope={scope} onContextChanged={() => {}} />,
  );
  await user.click(
    await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" }),
  );
  await screen.findByText("Synthetic Staff");
  expect(screen.queryByText(context.draft_reason)).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Open proposal for Synthetic Staff" }));
  await screen.findByText(context.draft_reason);
  expect(screen.getByText(context.submission_reason)).toBeInTheDocument();
  expect(paths.filter((path) => path.includes("/context?"))).toEqual([
    expect.stringContaining(
      `/posting-proposals/${proposal.id}/context?purpose=review&expected_version=1&expected_revision_id=${proposal.revision_id}`,
    ),
  ]);
  expect(
    paths.some(
      (path) =>
        path.includes("/enrollment") ||
        path.includes("/person-reviews") ||
        path.includes("posting-targets"),
    ),
  ).toBe(false);
  await user.click(screen.getByRole("button", { name: "Close proposal" }));
  expect(screen.queryByText(context.draft_reason)).not.toBeInTheDocument();
});

for (const status of [403, 503]) {
  it(`withholds protected rationale on ${status} and locks access only for current-authority denial`, async () => {
    const changed = vi.fn();
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        if (url.includes("posting-review-scopes"))
          return Response.json({
            data: [
              {
                hospital_id: hospital,
                name: "Synthetic Hospital",
                can_submit: false,
                can_review: true,
              },
            ],
            page: { has_more: false, next_cursor: null },
          });
        if (url.includes("posting-proposal-inbox"))
          return Response.json({
            data: [
              {
                proposal,
                staff: { id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" },
                is_submitter: false,
              },
            ],
            page: { has_more: false, next_cursor: null },
          });
        return Response.json(
          {
            error: status === 403 ? "forbidden" : "unavailable",
            message: "Synthetic protected refusal",
          },
          { status },
        );
      }),
    );
    const { user } = renderReview(
      <StaffPostingReviewWorkspace scope={scope} onContextChanged={changed} />,
    );
    await user.click(
      await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" }),
    );
    await user.click(
      await screen.findByRole("button", { name: "Open proposal for Synthetic Staff" }),
    );
    await screen.findByRole("alert");
    expect(screen.queryByText(context.draft_reason)).not.toBeInTheDocument();
    expect(screen.queryByText(context.submission_reason)).not.toBeInTheDocument();
    if (status === 403) expect(changed).toHaveBeenCalledTimes(1);
    else expect(changed).not.toHaveBeenCalled();
  });
}

it("discards selected rationale when the original account changes", async () => {
  const headers: Headers[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      headers.push(new Headers(init?.headers));
      if (url.endsWith("posting-review-source")) return Response.json(source);
      if (url.includes("posting-review-scopes"))
        return Response.json({
          data:
            new Headers(init?.headers).get("X-Helivanta-Expected-Subject") === scope.subject
              ? [
                  {
                    hospital_id: hospital,
                    name: "Synthetic Hospital",
                    can_submit: false,
                    can_review: true,
                  },
                ]
              : [],
          page: { has_more: false, next_cursor: null },
        });
      if (url.includes("posting-proposal-inbox"))
        return Response.json({
          data: [
            {
              proposal,
              staff: { id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" },
              is_submitter: false,
            },
          ],
          page: { has_more: false, next_cursor: null },
        });
      return Response.json(context);
    }),
  );
  function AccountHarness() {
    const [subject, setSubject] = useState(scope.subject);
    return (
      <>
        <button onClick={() => setSubject("synthetic-new-account")}>Change original account</button>
        <StaffPostingReviewWorkspace scope={{ ...scope, subject }} onContextChanged={() => {}} />
      </>
    );
  }
  const { user } = renderReview(<AccountHarness />);
  await user.click(
    await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" }),
  );
  await user.click(
    await screen.findByRole("button", { name: "Open proposal for Synthetic Staff" }),
  );
  await screen.findByText(context.draft_reason);
  await user.click(screen.getByRole("button", { name: "Change original account" }));
  expect(screen.queryByText(context.draft_reason)).not.toBeInTheDocument();
  await screen.findByText("No approved posting review hospitals on this page.");
  expect(headers.at(-1)?.get("X-Helivanta-Expected-Subject")).toBe("synthetic-new-account");
});

it("rejects the exact selected proposal only after mandatory rationale and source examination", async () => {
  const writes: { url: string; input: Record<string, unknown> }[] = [];
  let saved = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("posting-review-scopes"))
        return Response.json({
          data: [
            {
              hospital_id: hospital,
              name: "Synthetic Hospital",
              can_submit: false,
              can_review: true,
            },
          ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.includes("posting-proposal-inbox"))
        return Response.json({
          data: saved
            ? []
            : [
                {
                  proposal,
                  staff: { id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" },
                  is_submitter: false,
                },
              ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.endsWith("posting-review-source")) return Response.json(source);
      if (url.includes("/context?")) return Response.json(context);
      if (init?.method === "PUT" && url.includes("/decisions/")) {
        const input = JSON.parse(String(init.body));
        writes.push({ url, input });
        saved = true;
        return Response.json({
          ...proposal,
          status: "rejected",
          version: 2,
          revision_id: id(9),
          command_id: url.split("/").at(-1),
          recorded_at: "2026-11-04T01:00:00Z",
        });
      }
      throw new Error(`Unexpected request: ${url}`);
    }),
  );
  const { user } = renderReview(
    <StaffPostingReviewWorkspace scope={scope} onContextChanged={() => {}} />,
  );
  await user.click(
    await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" }),
  );
  await user.click(
    await screen.findByRole("button", { name: "Open proposal for Synthetic Staff" }),
  );
  await screen.findByText("The current employment source matches this submission.");
  await user.click(screen.getByRole("button", { name: "Reject proposal" }));
  expect(writes).toHaveLength(0);
  await user.type(
    screen.getByLabelText("Decision rationale"),
    "Synthetic placement needs correction",
  );
  await user.click(screen.getByRole("button", { name: "Reject proposal" }));
  expect(writes).toHaveLength(0);
  await user.click(
    screen.getByLabelText("I examined the submitted proposal and current employment source."),
  );
  await user.click(screen.getByRole("button", { name: "Reject proposal" }));
  await screen.findByText("Posting proposal rejected.");
  expect(writes).toHaveLength(1);
  expect(writes[0].input).toEqual({
    expected_version: 1,
    expected_revision_id: proposal.revision_id,
    decision: "reject",
    reason: "Synthetic placement needs correction",
    sources_examined: true,
  });
  expect(writes[0].url).toContain(`/posting-proposals/${proposal.id}/decisions/`);
});

it("lets the original submitter withdraw using submission context and preserves the exact command after a lost response", async () => {
  const requests: { url: string; method: string; headers: Headers; body: string | null }[] = [];
  let receipt: Record<string, unknown> | null = null;
  let recover = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      requests.push({
        url,
        method: init?.method ?? "GET",
        headers: new Headers(init?.headers),
        body: init?.body ? String(init.body) : null,
      });
      if (url.includes("posting-review-scopes"))
        return Response.json({
          data: [
            {
              hospital_id: hospital,
              name: "Synthetic Hospital",
              can_submit: true,
              can_review: false,
            },
          ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.includes("posting-proposal-inbox"))
        return Response.json({
          data: recover
            ? []
            : [
                {
                  proposal,
                  staff: { id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" },
                  is_submitter: true,
                },
              ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.endsWith("posting-review-source")) return Response.json(source);
      if (url.includes("/context?")) return Response.json({ ...context, purpose: "submission" });
      if (url.includes("/decisions/")) {
        if (init?.method === "PUT") {
          receipt = {
            ...proposal,
            status: "withdrawn",
            version: 2,
            revision_id: id(9),
            command_id: url.split("/").at(-1),
            recorded_at: "2026-11-04T01:00:00Z",
          };
          throw new TypeError("Synthetic response lost after commit");
        }
        recover = true;
        return Response.json(receipt);
      }
      throw new Error(`Unexpected request: ${url}`);
    }),
  );
  const { user } = renderReview(
    <StaffPostingReviewWorkspace scope={scope} onContextChanged={() => {}} />,
  );
  await user.click(
    await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" }),
  );
  await user.click(
    await screen.findByRole("button", { name: "Open proposal for Synthetic Staff" }),
  );
  await screen.findByText("The current employment source matches this submission.");
  expect(screen.queryByRole("button", { name: "Reject proposal" })).not.toBeInTheDocument();
  expect(requests.some((row) => row.url.includes("/context?purpose=submission&"))).toBe(true);
  await user.type(
    screen.getByLabelText("Decision rationale"),
    "Synthetic submitter withdraws placement",
  );
  await user.click(
    screen.getByLabelText("I examined the submitted proposal and current employment source."),
  );
  await user.click(screen.getByRole("button", { name: "Withdraw proposal" }));
  await screen.findByText(/The decision outcome could not be confirmed/);
  expect(screen.getByLabelText("Decision rationale")).toBeDisabled();
  expect(screen.getByRole("button", { name: "Refresh review access" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Refresh proposals" })).toBeDisabled();
  expect(screen.getByLabelText("Proposal status")).toBeDisabled();
  const originalVisibility = Object.getOwnPropertyDescriptor(document, "visibilityState");
  try {
    Object.defineProperty(document, "visibilityState", { value: "hidden", configurable: true });
    fireEvent(window, new Event("visibilitychange"));
    await act(async () => {
      Object.defineProperty(document, "visibilityState", { value: "visible", configurable: true });
      fireEvent(window, new Event("visibilitychange"));
      await Promise.resolve();
    });
    expect(await screen.findByRole("button", { name: "Recover decision receipt" })).toBeEnabled();
    expect(screen.getByLabelText("Decision rationale")).toHaveValue(
      "Synthetic submitter withdraws placement",
    );
  } finally {
    if (originalVisibility) Object.defineProperty(document, "visibilityState", originalVisibility);
    else Reflect.deleteProperty(document, "visibilityState");
  }
  await user.click(screen.getByRole("button", { name: "Recover decision receipt" }));
  await screen.findByText("Posting proposal withdrawn.");
  const decisions = requests.filter((row) => row.url.includes("/decisions/"));
  expect(decisions.map((row) => row.method)).toEqual(["PUT", "GET"]);
  expect(decisions[0].url).toBe(decisions[1].url);
  expect(JSON.parse(decisions[0].body!)).toEqual({
    expected_version: 1,
    expected_revision_id: proposal.revision_id,
    decision: "withdraw",
    reason: "Synthetic submitter withdraws placement",
    sources_examined: true,
  });
  for (const request of decisions) {
    expect(request.headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
    expect(request.headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  }
});

it("withholds a decision when the current employment-source owner is unavailable", async () => {
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") writes.push(url);
      if (url.includes("posting-review-scopes"))
        return Response.json({
          data: [
            {
              hospital_id: hospital,
              name: "Synthetic Hospital",
              can_submit: false,
              can_review: true,
            },
          ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.includes("posting-proposal-inbox"))
        return Response.json({
          data: [
            {
              proposal,
              staff: { id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" },
              is_submitter: false,
            },
          ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.endsWith("posting-review-source"))
        return Response.json(
          { error: "unavailable", message: "Synthetic unavailable source" },
          { status: 503 },
        );
      return Response.json(context);
    }),
  );
  const { user } = renderReview(
    <StaffPostingReviewWorkspace scope={scope} onContextChanged={() => {}} />,
  );
  await user.click(
    await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" }),
  );
  await user.click(
    await screen.findByRole("button", { name: "Open proposal for Synthetic Staff" }),
  );
  await screen.findByText(/The current employment source could not be verified/);
  expect(screen.getByRole("button", { name: "Reject proposal" })).toBeDisabled();
  expect(screen.getByLabelText("Decision rationale")).toBeDisabled();
  expect(writes).toHaveLength(0);
});

it("keeps an unconfirmed command after a missing receipt and retries only its original intent", async () => {
  const decisions: { url: string; method: string; body: string | null }[] = [];
  let contextReads = 0;
  let saved = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("posting-review-scopes"))
        return Response.json({
          data: [
            {
              hospital_id: hospital,
              name: "Synthetic Hospital",
              can_submit: false,
              can_review: true,
            },
          ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.includes("posting-proposal-inbox"))
        return Response.json({
          data: saved
            ? []
            : [
                {
                  proposal,
                  staff: { id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" },
                  is_submitter: false,
                },
              ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.endsWith("posting-review-source")) return Response.json(source);
      if (url.includes("/context?")) {
        contextReads++;
        return Response.json(context);
      }
      if (url.includes("/decisions/")) {
        decisions.push({
          url,
          method: init?.method ?? "GET",
          body: init?.body ? String(init.body) : null,
        });
        if (decisions.length === 1) throw new TypeError("Synthetic interrupted transport");
        if (init?.method === "GET")
          return Response.json(
            { error: "not_found", message: "Synthetic missing receipt" },
            { status: 404 },
          );
        saved = true;
        return Response.json({
          ...proposal,
          status: "rejected",
          version: 2,
          revision_id: id(9),
          command_id: url.split("/").at(-1),
          recorded_at: "2026-11-04T01:00:00Z",
        });
      }
      throw new Error(`Unexpected request: ${url}`);
    }),
  );
  const { user } = renderReview(
    <StaffPostingReviewWorkspace scope={scope} onContextChanged={() => {}} />,
  );
  await user.click(
    await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" }),
  );
  await user.click(
    await screen.findByRole("button", { name: "Open proposal for Synthetic Staff" }),
  );
  await screen.findByText("The current employment source matches this submission.");
  await user.type(screen.getByLabelText("Decision rationale"), "Synthetic exact retry rationale");
  await user.click(
    screen.getByLabelText("I examined the submitted proposal and current employment source."),
  );
  await user.click(screen.getByRole("button", { name: "Reject proposal" }));
  await user.click(await screen.findByRole("button", { name: "Recover decision receipt" }));
  await user.click(await screen.findByRole("button", { name: "Retry exact decision" }));
  await screen.findByText("Posting proposal rejected.");
  expect(decisions.map((row) => row.method)).toEqual(["PUT", "GET", "PUT"]);
  expect(new Set(decisions.map((row) => row.url)).size).toBe(1);
  expect(decisions[2].body).toBe(decisions[0].body);
  expect(contextReads).toBeGreaterThan(1);
});

it("clears private rationale when a missing-receipt context recheck confirms revoked authority", async () => {
  let contextReads = 0;
  const changed = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("posting-review-scopes"))
        return Response.json({
          data: [
            {
              hospital_id: hospital,
              name: "Synthetic Hospital",
              can_submit: false,
              can_review: true,
            },
          ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.includes("posting-proposal-inbox"))
        return Response.json({
          data: [
            {
              proposal,
              staff: { id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" },
              is_submitter: false,
            },
          ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.endsWith("posting-review-source")) return Response.json(source);
      if (url.includes("/context?")) {
        contextReads++;
        return contextReads === 1
          ? Response.json(context)
          : Response.json(
              { error: "forbidden", message: "Synthetic revoked context" },
              { status: 403 },
            );
      }
      if (url.includes("/decisions/") && init?.method === "PUT")
        throw new TypeError("Synthetic lost response");
      return Response.json(
        { error: "not_found", message: "Synthetic missing receipt" },
        { status: 404 },
      );
    }),
  );
  const { user } = renderReview(
    <StaffPostingReviewWorkspace scope={scope} onContextChanged={changed} />,
  );
  await user.click(
    await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" }),
  );
  await user.click(
    await screen.findByRole("button", { name: "Open proposal for Synthetic Staff" }),
  );
  await screen.findByText("The current employment source matches this submission.");
  await user.type(screen.getByLabelText("Decision rationale"), "Synthetic stale reviewer intent");
  await user.click(
    screen.getByLabelText("I examined the submitted proposal and current employment source."),
  );
  await user.click(screen.getByRole("button", { name: "Reject proposal" }));
  await user.click(await screen.findByRole("button", { name: "Recover decision receipt" }));
  await screen.findByText("Staff access changed. Reopen Staff to verify your current workspace.");
  expect(screen.queryByText(context.draft_reason)).not.toBeInTheDocument();
  expect(screen.queryByText(context.submission_reason)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Retry exact decision" })).not.toBeInTheDocument();
  expect(changed).toHaveBeenCalledTimes(1);
});
