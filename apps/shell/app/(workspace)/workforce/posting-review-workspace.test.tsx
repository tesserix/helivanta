import { useState, type ReactElement } from "react";
import { NavigationGuardProvider } from "@helivanta/ui";
import "@/test/select";
import { renderWithProviders } from "@helivanta/api/testing";
import { act, fireEvent, screen, within, waitFor } from "@testing-library/react";
import { StaffPostingProposalContextContract } from "@helivanta/api";
import { PostingProposalDecision } from "./posting-proposal-decision";
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
const department = {
  id: proposal.department_id,
  revision_id: proposal.department_revision_id,
  hospital_id: hospital,
  version: 1,
  code: "SYN-PHARMA",
  name: "Synthetic Pharmacy",
  kind: "department",
  status: "active",
  created_at: proposal.created_at,
  effective_at: proposal.created_at,
};
function renderReview(ui: ReactElement) {
  return renderWithProviders(<NavigationGuardProvider>{ui}</NavigationGuardProvider>);
}
beforeEach(() => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(new Date("2026-11-04T01:00:00Z"));
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function lifecycleContextResponse(
  value: unknown,
  supervision: unknown,
  chain: { posting: ReturnType<typeof supervisorChoice>; supervision: unknown }[] = [],
) {
  const parsed = StaffPostingProposalContextContract.safeParse(value);
  if (!parsed.success) return Response.json({ context: value, supervision });
  const limits = [
    parsed.data.proposal.eligibility_until,
    parsed.data.proposal.ends_at,
    ...chain.flatMap((node) => [node.posting.eligibility_until, node.posting.ends_at]),
  ].filter((value): value is string => value !== null);
  const effective_until = limits.reduce((earliest, value) =>
    Date.parse(value) < Date.parse(earliest) ? value : earliest,
  );
  const absence = { id: null, version: 0, cutoff_at: null };
  return Response.json({
    context: value,
    supervision,
    cutoff: absence,
    chain: chain.map((node) => ({ ...node, cutoff: absence })),
    checked_at: source.checked_at,
    effective_until,
  });
}

function postingContextResponse(url: string, value: unknown, supervision: unknown = null) {
  const query = new URL(url, "http://localhost").searchParams;
  if (query.has("lifecycle_review")) return lifecycleContextResponse(value, supervision);
  return Response.json(
    query.get("supervision_context") === "v1" ? { context: value, supervision } : value,
  );
}

function submissionResponse(url: string, init?: RequestInit): Response {
  if (init?.method === "PUT")
    return Response.json({
      ...proposal,
      id: url.split("/").at(-3),
      command_id: url.split("/").at(-1),
    });
  if (url.includes("posting-review-scopes"))
    return Response.json({
      data: [
        { hospital_id: hospital, name: "Synthetic Hospital", can_submit: true, can_review: false },
      ],
      page: { has_more: false, next_cursor: null },
    });
  if (url.includes("posting-targets"))
    return Response.json({
      data: [{ id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" }],
      page: { has_more: false, next_cursor: null },
    });
  if (url.endsWith("posting-review-source")) return Response.json(source);
  return Response.json({
    data: url.includes("posting-drafts")
      ? [context.draft]
      : url.includes("posting-department-choices")
        ? [department]
        : [],
    page: { has_more: false, next_cursor: null },
  });
}
function reviewerResponse(url: string, supervision: unknown, visible = true): Response {
  if (url.includes("posting-review-scopes"))
    return Response.json({
      data: [
        { hospital_id: hospital, name: "Synthetic Hospital", can_submit: false, can_review: true },
      ],
      page: { has_more: false, next_cursor: null },
    });
  if (url.includes("posting-proposal-inbox"))
    return Response.json({
      data: visible
        ? [
            {
              proposal,
              staff: { id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" },
              is_submitter: false,
            },
          ]
        : [],
      page: { has_more: false, next_cursor: null },
    });
  if (url.endsWith("posting-review-source")) return Response.json(source);
  if (url.includes("/context?")) return postingContextResponse(url, context, supervision);
  throw new Error(`Unexpected request: ${url}`);
}

async function openReviewer() {
  const rendered = renderReview(
    <StaffPostingReviewWorkspace scope={scope} onContextChanged={() => {}} />,
  );
  await rendered.user.click(
    await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" }),
  );
  await rendered.user.click(
    await screen.findByRole("button", { name: "Open proposal for Synthetic Staff" }),
  );
  return rendered;
}

async function openSubmission(onContextChanged = () => {}, ui?: ReactElement) {
  const rendered = renderReview(
    ui ?? <StaffPostingReviewWorkspace scope={scope} onContextChanged={onContextChanged} />,
  );
  await rendered.user.click(
    await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" }),
  );
  await rendered.user.click(await screen.findByRole("button", { name: "Submit prepared posting" }));
  await rendered.user.click(await screen.findByRole("button", { name: "Choose Synthetic Staff" }));
  await rendered.user.click(await screen.findByRole("button", { name: "Submit draft version 1" }));
  return { ...rendered, form: await screen.findByRole("form", { name: "Posting submission" }) };
}

it("shows the exact canonical department name before asking the submitter to attest", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => submissionResponse(url, init)),
  );
  await openSubmission();
  await screen.findByText("Current department: SYN-PHARMA · Synthetic Pharmacy");
});

for (const state of ["unavailable", "revised"] as const) {
  it(`blocks submission when the canonical department is ${state}`, async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.includes("posting-department-choices"))
          return state === "unavailable"
            ? Response.json(
                { error: "unavailable", message: "Synthetic structure outage" },
                { status: 503 },
              )
            : Response.json({
                data: [{ ...department, revision_id: id(99), version: 2 }],
                page: { has_more: false, next_cursor: null },
              });
        return submissionResponse(url, init);
      }),
    );
    const { form } = await openSubmission();
    await waitFor(() =>
      expect(screen.queryByText("Checking the current department…")).not.toBeInTheDocument(),
    );
    expect(within(form).getByLabelText("Submission rationale")).toBeDisabled();
    expect(within(form).getByRole("button", { name: "Request independent review" })).toBeDisabled();
  });
}

it("clears a retained submission when fresh admission loses submit capability but keeps read access", async () => {
  let recovering = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") throw new TypeError("Synthetic lost response");
      if (url.includes("/commands/")) {
        recovering = true;
        return Response.json({ error: "not_found", message: "No receipt yet" }, { status: 404 });
      }
      if (recovering && url.includes("posting-review-scopes"))
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
      return submissionResponse(url, init);
    }),
  );
  const { user, form } = await openSubmission();
  await user.type(
    within(form).getByLabelText("Submission rationale"),
    "Synthetic submit-only authority to clear",
  );
  await user.click(
    within(form).getByLabelText("I examined this draft and the current employment source."),
  );
  await user.click(within(form).getByRole("button", { name: "Request independent review" }));
  await user.click(await screen.findByRole("button", { name: "Recover submission receipt" }));
  await waitFor(() =>
    expect(
      screen.queryByDisplayValue("Synthetic submit-only authority to clear"),
    ).not.toBeInTheDocument(),
  );
  expect(screen.queryByRole("button", { name: "Retry exact submission" })).not.toBeInTheDocument();
});

it("preserves the immutable submission when its fresh admission check is unavailable", async () => {
  let recovering = false;
  let available = false;
  let scopeReads = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") throw new TypeError("Synthetic lost response");
      if (url.includes("/commands/")) {
        recovering = true;
        return Response.json({ error: "not_found", message: "No receipt yet" }, { status: 404 });
      }
      if (url.includes("posting-review-scopes")) {
        scopeReads++;
        if (recovering && !available)
          return Response.json(
            { error: "unavailable", message: "Synthetic admission outage" },
            { status: 503 },
          );
      }
      return submissionResponse(url, init);
    }),
  );
  const { user, form } = await openSubmission();
  await user.type(
    within(form).getByLabelText("Submission rationale"),
    "Synthetic retained admission outage reason",
  );
  await user.click(
    within(form).getByLabelText("I examined this draft and the current employment source."),
  );
  await user.click(within(form).getByRole("button", { name: "Request independent review" }));
  await user.click(await screen.findByRole("button", { name: "Recover submission receipt" }));
  await waitFor(() => expect(scopeReads).toBeGreaterThan(1));
  await screen.findByText(/Posting review admission could not be rechecked/);
  expect(within(form).getByLabelText("Submission rationale")).toHaveValue(
    "Synthetic retained admission outage reason",
  );
  expect(within(form).getByLabelText("Submission rationale")).toBeDisabled();
  available = true;
  await user.click(await screen.findByRole("button", { name: "Recheck posting admission" }));
  await waitFor(() =>
    expect(
      screen.queryByText(/Posting review admission could not be rechecked/),
    ).not.toBeInTheDocument(),
  );
  expect(within(form).getByRole("button", { name: "Retry exact submission" })).toBeEnabled();
  expect(
    within(form).queryByRole("button", { name: "Request independent review" }),
  ).not.toBeInTheDocument();
});

it("finds the exact department after an empty page with continuation without treating it as missing", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("posting-department-choices"))
        return Response.json(
          url.includes("cursor=")
            ? { data: [department], page: { has_more: false, next_cursor: null } }
            : { data: [], page: { has_more: true, next_cursor: "department-page-two" } },
        );
      return submissionResponse(url, init);
    }),
  );
  const { user, form } = await openSubmission();
  expect(within(form).getByRole("button", { name: "Request independent review" })).toBeDisabled();
  await user.click(await screen.findByRole("button", { name: "More submission departments" }));
  await screen.findByText("Current department: SYN-PHARMA · Synthetic Pharmacy");
  expect(within(form).getByRole("button", { name: "Request independent review" })).toBeEnabled();
});

it("clears an unedited submission when moving to another Staff choices page", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("posting-targets"))
        return Response.json(
          url.includes("cursor=")
            ? { data: [], page: { has_more: false, next_cursor: null } }
            : {
                data: [{ id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" }],
                page: { has_more: true, next_cursor: "next-staff" },
              },
        );
      return submissionResponse(url, init);
    }),
  );
  const { user } = await openSubmission();
  await user.click(screen.getByRole("button", { name: "More submission Staff choices" }));
  await screen.findByText("No Staff choices on this page.");
  expect(screen.queryByRole("form", { name: "Posting submission" })).not.toBeInTheDocument();
});

it("does not expose submission controls or Staff discovery to a reviewer-only assignment", async () => {
  const reads: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      reads.push(url);
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
      return submissionResponse(url, init);
    }),
  );
  const { user } = renderReview(
    <StaffPostingReviewWorkspace scope={scope} onContextChanged={() => {}} />,
  );
  await user.click(
    await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" }),
  );
  await screen.findByText("No pending proposals on this page.");
  expect(screen.queryByRole("button", { name: "Submit prepared posting" })).not.toBeInTheDocument();
  expect(
    reads.some((url) => url.includes("posting-targets") || url.includes("posting-drafts")),
  ).toBe(false);
});

it("aborts and clears an in-flight submission when its account changes without replaying it", async () => {
  let switchAccount: (value: typeof scope) => void = () => {};
  let finish: (value: Response) => void = () => {};
  let signal: AbortSignal | undefined;
  let path = "";
  const writes: string[] = [];
  function AccountSwitch() {
    const [account, setAccount] = useState(scope);
    switchAccount = setAccount;
    return <StaffPostingReviewWorkspace scope={account} onContextChanged={() => {}} />;
  }
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        path = url;
        writes.push(url);
        signal = init.signal ?? undefined;
        return new Promise<Response>((resolve) => {
          finish = resolve;
        });
      }
      return submissionResponse(url, init);
    }),
  );
  const { user, form } = await openSubmission(() => {}, <AccountSwitch />);
  await user.type(
    within(form).getByLabelText("Submission rationale"),
    "Synthetic private pending submission",
  );
  await user.click(
    within(form).getByLabelText("I examined this draft and the current employment source."),
  );
  await user.click(within(form).getByRole("button", { name: "Request independent review" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  act(() => switchAccount({ ...scope, subject: "synthetic-replacement-account" }));
  await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" });
  expect(signal?.aborted).toBe(true);
  expect(
    screen.queryByDisplayValue("Synthetic private pending submission"),
  ).not.toBeInTheDocument();
  await act(async () => {
    finish(submissionResponse(path, { method: "PUT" }));
  });
  expect(
    screen.queryByText(
      "Posting proposal submitted for independent review. Operating authority is unchanged.",
    ),
  ).not.toBeInTheDocument();
  expect(writes).toHaveLength(1);
});

it("keeps an uncertain submission immutable across missing receipts and changed sources", async () => {
  const writes: { path: string; body: string }[] = [];
  let sourceReads = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes.push({ path: url, body: String(init.body) });
        if (writes.length === 1) throw new TypeError("Synthetic lost response");
      }
      if (url.includes("/commands/") && init?.method !== "PUT")
        return Response.json({ error: "not_found", message: "No receipt yet" }, { status: 404 });
      if (url.endsWith("posting-review-source")) {
        sourceReads++;
        return Response.json({ ...source, staff_version: writes.length ? 3 : 2 });
      }
      return submissionResponse(url, init);
    }),
  );
  const { user, form } = await openSubmission();
  await user.type(
    within(form).getByLabelText("Submission rationale"),
    "Synthetic retained proposal reason",
  );
  await user.click(
    within(form).getByLabelText("I examined this draft and the current employment source."),
  );
  await user.click(within(form).getByRole("button", { name: "Request independent review" }));
  await screen.findByText(/The submission outcome could not be confirmed/);
  expect(within(form).getByLabelText("Submission rationale")).toBeDisabled();
  expect(screen.getByRole("button", { name: "Back to proposals" })).toBeDisabled();
  await user.click(within(form).getByRole("button", { name: "Recover submission receipt" }));
  await waitFor(() => expect(sourceReads).toBeGreaterThan(1));
  await screen.findByText(/The employment source changed or is not eligible/);
  expect(within(form).getByLabelText("Submission rationale")).toHaveValue(
    "Synthetic retained proposal reason",
  );
  expect(
    within(form).queryByRole("button", { name: "Request independent review" }),
  ).not.toBeInTheDocument();
  await user.click(within(form).getByRole("button", { name: "Retry exact submission" }));
  await screen.findByText(
    "Posting proposal submitted for independent review. Operating authority is unchanged.",
  );
  expect(writes).toHaveLength(2);
  expect(writes[1]).toEqual(writes[0]);
});

it("clears the submission rationale when missing-receipt recovery independently finds revoked source access", async () => {
  const changed = vi.fn();
  let recovering = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") throw new TypeError("Synthetic lost response");
      if (url.includes("/commands/")) {
        recovering = true;
        return Response.json({ error: "not_found", message: "No receipt yet" }, { status: 404 });
      }
      if (recovering && url.endsWith("posting-review-source"))
        return Response.json(
          { error: "forbidden", message: "Source access withdrawn" },
          { status: 403 },
        );
      return submissionResponse(url, init);
    }),
  );
  const { user, form } = await openSubmission(changed);
  await user.type(
    within(form).getByLabelText("Submission rationale"),
    "Synthetic private rationale to clear",
  );
  await user.click(
    within(form).getByLabelText("I examined this draft and the current employment source."),
  );
  await user.click(within(form).getByRole("button", { name: "Request independent review" }));
  await user.click(await screen.findByRole("button", { name: "Recover submission receipt" }));
  await waitFor(() => expect(changed).toHaveBeenCalled());
  expect(
    screen.queryByDisplayValue("Synthetic private rationale to clear"),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Retry exact submission" })).not.toBeInTheDocument();
});

for (const failure of ["unavailable", "changed", "missing"] as const) {
  it(`blocks a new submission when the employment generation is ${failure}`, async () => {
    const writes: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (init?.method === "PUT") writes.push(url);
        if (url.endsWith("posting-review-source")) {
          if (failure === "unavailable")
            return Response.json(
              { error: "posting_source_unavailable", message: "Synthetic outage" },
              { status: 503 },
            );
          return Response.json(
            failure === "changed"
              ? { ...source, staff_version: 3 }
              : {
                  ...source,
                  generation_state: "no_current_generation",
                  generation_id: null,
                  starts_at: null,
                  ends_at: null,
                  eligibility_until: null,
                },
          );
        }
        return submissionResponse(url, init);
      }),
    );
    const { form } = await openSubmission();
    await waitFor(() =>
      expect(screen.queryByText("Checking current employment source…")).not.toBeInTheDocument(),
    );
    expect(within(form).getByRole("button", { name: "Request independent review" })).toBeDisabled();
    expect(within(form).getByLabelText("Submission rationale")).toBeDisabled();
    expect(writes).toHaveLength(0);
  });
}

it("allows review submission for an exact future reviewed employment generation", async () => {
  const future = "2026-11-05T00:00:00Z";
  const until = "2026-11-06T00:00:00Z";
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes.push(url);
        return Response.json({
          ...proposal,
          id: url.split("/").at(-3),
          command_id: url.split("/").at(-1),
          starts_at: future,
          eligibility_until: until,
        });
      }
      if (url.endsWith("posting-review-source"))
        return Response.json({
          ...source,
          generation_state: "not_started",
          starts_at: future,
          eligibility_until: until,
        });
      if (url.includes("posting-drafts"))
        return Response.json({
          data: [{ ...context.draft, starts_at: future }],
          page: { has_more: false, next_cursor: null },
        });
      return submissionResponse(url, init);
    }),
  );
  const { user, form } = await openSubmission();
  await user.type(
    within(form).getByLabelText("Submission rationale"),
    "Synthetic future posting for independent review",
  );
  await user.click(
    within(form).getByLabelText("I examined this draft and the current employment source."),
  );
  await user.click(within(form).getByRole("button", { name: "Request independent review" }));
  await screen.findByText(
    "Posting proposal submitted for independent review. Operating authority is unchanged.",
  );
  expect(writes).toHaveLength(1);
});

it("reports canonical control-character validation before issuing a posting submission", async () => {
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") writes.push(url);
      return submissionResponse(url, init);
    }),
  );
  const { user, form } = await openSubmission();
  fireEvent.change(within(form).getByLabelText("Submission rationale"), {
    target: { value: "invalid\u0001rationale" },
  });
  await user.click(
    within(form).getByLabelText("I examined this draft and the current employment source."),
  );
  await user.click(within(form).getByRole("button", { name: "Request independent review" }));
  await screen.findByText("Remove control characters.");
  expect(writes).toHaveLength(0);
});

it("submits an exact prepared draft only after examining current sources and entering a rationale", async () => {
  const writes: { path: string; input: Record<string, unknown>; headers: Headers }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes.push({
          path: url,
          input: JSON.parse(String(init.body)),
          headers: new Headers(init.headers),
        });
        return Response.json({
          ...proposal,
          id: url.split("/").at(-3),
          command_id: url.split("/").at(-1),
        });
      }
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
      if (url.includes("posting-targets"))
        return Response.json({
          data: [{ id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" }],
          page: { has_more: false, next_cursor: null },
        });
      if (url.endsWith("posting-review-source")) return Response.json(source);
      return Response.json({
        data: url.includes("posting-drafts")
          ? [context.draft]
          : url.includes("posting-department-choices")
            ? [department]
            : [],
        page: { has_more: false, next_cursor: null },
      });
    }),
  );
  const { user } = renderReview(
    <StaffPostingReviewWorkspace scope={scope} onContextChanged={() => {}} />,
  );
  await user.click(
    await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" }),
  );
  await user.click(await screen.findByRole("button", { name: "Submit prepared posting" }));
  await user.click(await screen.findByRole("button", { name: "Choose Synthetic Staff" }));
  await user.click(await screen.findByRole("button", { name: "Submit draft version 1" }));
  const form = await screen.findByRole("form", { name: "Posting submission" });
  await user.click(within(form).getByRole("button", { name: "Request independent review" }));
  expect(writes).toHaveLength(0);
  await user.type(
    within(form).getByLabelText("Submission rationale"),
    "Synthetic independently reviewed department placement",
  );
  await user.click(within(form).getByRole("button", { name: "Request independent review" }));
  expect(writes).toHaveLength(0);
  await user.click(
    within(form).getByLabelText("I examined this draft and the current employment source."),
  );
  await user.click(within(form).getByRole("button", { name: "Request independent review" }));
  await screen.findByText(
    "Posting proposal submitted for independent review. Operating authority is unchanged.",
  );
  expect(writes).toHaveLength(1);
  expect(writes[0].input).toEqual({
    expected_version: 0,
    draft_id: context.draft.id,
    draft_revision_id: context.draft.revision_id,
    draft_version: 1,
    generation_id: source.generation_id,
    staff_version: 2,
    employment_epoch: 2,
    reason: "Synthetic independently reviewed department placement",
    sources_examined: true,
  });
  expect(writes[0].headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(writes[0].headers.get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
});

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
      if (url.includes("/context?")) return postingContextResponse(url, context);
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
      return postingContextResponse(url, context);
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
      if (url.includes("/context?")) return postingContextResponse(url, context);
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
      if (url.includes("/context?"))
        return postingContextResponse(url, { ...context, purpose: "submission" });
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
      return postingContextResponse(url, context);
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
        return postingContextResponse(url, context);
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
          ? postingContextResponse(url, context)
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

it("requires guarded management context and explicit attestation before root approval", async () => {
  const writes: { url: string; input: Record<string, unknown> }[] = [];
  const guardedReads: string[] = [];
  let approved = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
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
          data: approved
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
        const query = new URL(url, "http://localhost").searchParams;
        if (query.has("management_review")) guardedReads.push(url);
        return postingContextResponse(url, context, { kind: "hospital_management" });
      }
      if (init?.method === "PUT" && url.includes("/decisions/")) {
        writes.push({ url, input: JSON.parse(String(init.body)) });
        approved = true;
        return Response.json({
          ...proposal,
          status: "approved",
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
  await user.click(
    await screen.findByRole("button", { name: "Review hospital management approval" }),
  );
  await screen.findByRole("button", { name: "Approve proposal" });
  expect(guardedReads).toHaveLength(1);
  expect(guardedReads[0]).toContain(
    "supervision_context=v1&lifecycle_review=current&management_review=current",
  );
  await user.type(
    screen.getByLabelText("Decision rationale"),
    "Independent synthetic management responsibility review",
  );
  await user.click(
    screen.getByLabelText("I examined the submitted proposal and current employment source."),
  );
  await user.click(screen.getByRole("button", { name: "Approve proposal" }));
  expect(writes).toHaveLength(0);
  await user.click(
    screen.getByLabelText("I independently reviewed the hospital management responsibility."),
  );
  await user.click(screen.getByRole("button", { name: "Approve proposal" }));
  await screen.findByText("Posting proposal approved.");
  expect(writes).toHaveLength(1);
  expect(writes[0].input).toEqual({
    expected_version: 1,
    expected_revision_id: proposal.revision_id,
    decision: "approve",
    reason: "Independent synthetic management responsibility review",
    sources_examined: true,
    hospital_management_reviewed: true,
    lifecycle_review: { cutoff_versions: [0], effective_until: source.eligibility_until },
  });
});

it.each([403, 503])(
  "withholds management approval and private context when its guarded read returns %s",
  async (status) => {
    const writes: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (init?.method === "PUT") writes.push(url);
        if (url.includes("management_review=current"))
          return Response.json(
            {
              error: status === 403 ? "forbidden" : "unavailable",
              message: "Synthetic management authority unavailable",
            },
            { status },
          );
        return reviewerResponse(url, { kind: "hospital_management" });
      }),
    );
    const { user } = await openReviewer();
    await user.click(
      await screen.findByRole("button", { name: "Review hospital management approval" }),
    );
    await screen.findByText(
      status === 403
        ? "Staff access changed. Reopen Staff to verify your current workspace."
        : /The selected proposal could not be verified/,
    );
    expect(
      screen.queryByLabelText("I independently reviewed the hospital management responsibility."),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Approve proposal" })).not.toBeInTheDocument();
    expect(screen.queryByText(context.draft_reason)).not.toBeInTheDocument();
    expect(writes).toHaveLength(0);
  },
);

it.each([
  null,
  {
    kind: "department_member",
    supervisor_staff_id: id(21),
    supervisor_proposal_id: id(22),
    supervisor_revision_id: id(23),
  },
])("does not offer root approval for an omitted or non-root intent", async (supervision) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => reviewerResponse(url, supervision)),
  );
  await openReviewer();
  await screen.findByText("Managerial responsibility");
  if (supervision) {
    expect(await screen.findByText(supervision.supervisor_staff_id)).toBeInTheDocument();
    expect(screen.getByText(supervision.supervisor_proposal_id)).toBeInTheDocument();
    expect(screen.getByText(supervision.supervisor_revision_id)).toBeInTheDocument();
  }
  expect(
    screen.queryByRole("button", { name: "Review hospital management approval" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Approve proposal" })).not.toBeInTheDocument();
});

it("retains the exact management attestation and command through an uncertain approval", async () => {
  const writes: { url: string; body: string }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "PUT") {
        writes.push({ url, body: String(init.body) });
        if (writes.length === 1) throw new TypeError("Synthetic lost approval response");
        return Response.json({
          ...proposal,
          status: "approved",
          version: 2,
          revision_id: id(9),
          command_id: url.split("/").at(-1),
          recorded_at: "2026-11-04T01:00:00Z",
        });
      }
      return reviewerResponse(url, { kind: "hospital_management" });
    }),
  );
  const { user } = await openReviewer();
  await user.click(
    await screen.findByRole("button", { name: "Review hospital management approval" }),
  );
  await screen.findByText("The current employment source matches this submission.");
  await user.type(
    screen.getByLabelText("Decision rationale"),
    "Synthetic independent root management review",
  );
  await user.click(
    screen.getByLabelText("I examined the submitted proposal and current employment source."),
  );
  await user.click(
    screen.getByLabelText("I independently reviewed the hospital management responsibility."),
  );
  await user.click(screen.getByRole("button", { name: "Approve proposal" }));
  await screen.findByRole("button", { name: "Retry exact decision" });
  expect(
    screen.getByLabelText("I independently reviewed the hospital management responsibility."),
  ).toBeDisabled();
  expect(screen.getByRole("button", { name: "Back to proposal review" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry exact decision" }));
  await screen.findByText("Posting proposal approved.");
  expect(writes).toHaveLength(2);
  expect(writes[1]).toEqual(writes[0]);
  expect(JSON.parse(writes[0].body).hospital_management_reviewed).toBe(true);
});

it("retains an explicit hospital-management submission through an uncertain response", async () => {
  const writes: { url: string; body: string }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes.push({ url, body: String(init.body) });
        if (writes.length === 1)
          throw new TypeError("Synthetic lost management submission response");
      }
      return submissionResponse(url, init);
    }),
  );
  const { user, form } = await openSubmission();
  await screen.findByText("Current department: SYN-PHARMA · Synthetic Pharmacy");
  await user.type(
    within(form).getByLabelText("Submission rationale"),
    "Synthetic explicit hospital-management responsibility request",
  );
  await choosePostingResponsibility(user, form, "hospital_management");
  await user.click(
    within(form).getByLabelText("I examined this draft and the current employment source."),
  );
  await user.click(within(form).getByRole("button", { name: "Request independent review" }));
  await screen.findByRole("button", { name: "Retry exact submission" });
  expect(within(form).getByLabelText("Posting responsibility")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry exact submission" }));
  await screen.findByText(
    "Posting proposal submitted for independent review. Operating authority is unchanged.",
  );
  expect(writes).toHaveLength(2);
  expect(writes[1]).toEqual(writes[0]);
  expect(JSON.parse(writes[0].body).supervision).toEqual({ kind: "hospital_management" });
});

it.each(["posting_proposal_conflict", "posting_lifecycle_review_required"])(
  "rechecks a changed employment source after %s without discarding the command",
  async (errorCode) => {
    const writes: string[] = [];
    let changed = false;
    let sourceReads = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (init?.method === "PUT") {
          writes.push(String(init.body));
          changed = true;
          return Response.json(
            {
              error: errorCode,
              message: "The selected employment source changed.",
            },
            { status: 409 },
          );
        }
        if (url.endsWith("posting-review-source")) {
          sourceReads++;
          return Response.json(
            changed
              ? { ...source, generation_state: "source_changed", eligibility_until: null }
              : source,
          );
        }
        return reviewerResponse(url, { kind: "hospital_management" });
      }),
    );
    const { user } = await openReviewer();
    await user.click(
      await screen.findByRole("button", { name: "Review hospital management approval" }),
    );
    await screen.findByText("The current employment source matches this submission.");
    const before = sourceReads;
    await user.type(
      screen.getByLabelText("Decision rationale"),
      "Independent review before a concurrent identity change",
    );
    await user.click(
      screen.getByLabelText("I examined the submitted proposal and current employment source."),
    );
    await user.click(
      screen.getByLabelText("I independently reviewed the hospital management responsibility."),
    );
    await user.click(screen.getByRole("button", { name: "Approve proposal" }));
    await screen.findByText(
      "The employment source differs from this submission or is no longer current.",
    );
    expect(sourceReads).toBeGreaterThan(before);
    expect(writes).toHaveLength(1);
    expect(screen.getByLabelText("Decision rationale")).toHaveValue(
      "Independent review before a concurrent identity change",
    );
    expect(
      screen.getByLabelText("I independently reviewed the hospital management responsibility."),
    ).toBeChecked();
    expect(screen.getByRole("button", { name: "Retry exact decision" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Recover or replay approval" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "Retry exact decision" }));
    expect(writes).toHaveLength(1);
  },
);

it("blocks a new approval of an ended posting despite current employment", async () => {
  const endsAt = new Date(Date.now() - 60_000).toISOString();
  const startsAt = new Date(Date.now() - 120_000).toISOString();
  const endedProposal = { ...proposal, starts_at: startsAt, ends_at: endsAt };
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "PUT") {
        writes.push(String(init.body));
        return Response.json({
          ...endedProposal,
          status: "rejected",
          version: 2,
          revision_id: id(20),
          command_id: url.split("/").at(-1),
        });
      }
      if (url.includes("posting-proposal-inbox"))
        return Response.json({
          data: [
            {
              proposal: endedProposal,
              staff: { id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" },
              is_submitter: false,
            },
          ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.includes("/context?"))
        return postingContextResponse(
          url,
          {
            ...context,
            proposal: endedProposal,
            draft: { ...context.draft, starts_at: startsAt, ends_at: endsAt },
          },
          { kind: "hospital_management" },
        );
      return reviewerResponse(url, { kind: "hospital_management" });
    }),
  );
  const { user } = await openReviewer();
  await user.click(
    await screen.findByRole("button", { name: "Review hospital management approval" }),
  );
  await screen.findByText("The current employment source matches this submission.");
  expect(screen.getByRole("button", { name: "Approve proposal" })).toBeDisabled();
  expect(
    screen.getByText("The effective responsibility has ended. A new approval cannot be recorded."),
  ).toBeVisible();
  expect(writes).toHaveLength(0);
});

it("disables an uncertain approval when its posting ends while preserving receipt recovery", async () => {
  const current = Date.now();
  const startsAt = new Date(current - 120_000).toISOString();
  const endsAt = new Date(current + 60_000).toISOString();
  const timedProposal = { ...proposal, starts_at: startsAt, ends_at: endsAt };
  const writes: string[] = [];
  let recovered = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "PUT") {
        writes.push(String(init.body));
        if (writes.length === 1)
          throw new TypeError("Synthetic lost approval response before nominal end");
      }
      if (url.includes("/decisions/")) {
        recovered = true;
        return Response.json({
          ...timedProposal,
          status: "approved",
          version: 2,
          revision_id: id(20),
          command_id: url.split("/").at(-1),
        });
      }
      if (url.includes("posting-proposal-inbox"))
        return Response.json({
          data: [
            {
              proposal: timedProposal,
              staff: { id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" },
              is_submitter: false,
            },
          ],
          page: { has_more: false, next_cursor: null },
        });
      if (url.includes("/context?"))
        return postingContextResponse(
          url,
          {
            ...context,
            proposal: timedProposal,
            draft: { ...context.draft, starts_at: startsAt, ends_at: endsAt },
          },
          { kind: "hospital_management" },
        );
      return reviewerResponse(url, { kind: "hospital_management" });
    }),
  );
  const { user } = await openReviewer();
  await user.click(
    await screen.findByRole("button", { name: "Review hospital management approval" }),
  );
  await screen.findByText("The current employment source matches this submission.");
  await user.type(
    screen.getByLabelText("Decision rationale"),
    "Synthetic independent timed appointment review",
  );
  await user.click(
    screen.getByLabelText("I examined the submitted proposal and current employment source."),
  );
  await user.click(
    screen.getByLabelText("I independently reviewed the hospital management responsibility."),
  );
  await user.click(screen.getByRole("button", { name: "Approve proposal" }));
  await screen.findByRole("button", { name: "Retry exact decision" });
  expect(screen.getByRole("button", { name: "Retry exact decision" })).toBeEnabled();
  const endedClock = vi.spyOn(Date, "now").mockReturnValue(current + 60_000);
  try {
    await user.click(screen.getByRole("button", { name: "Retry exact decision" }));
    expect(screen.getByRole("button", { name: "Retry exact decision" })).toBeDisabled();
    expect(writes).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Recover or replay approval" })).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "Recover or replay approval" }));
    await screen.findByText("Posting proposal approved.");
    expect(recovered).toBe(true);
    expect(writes).toHaveLength(2);
    expect(writes[1]).toBe(writes[0]);
  } finally {
    endedClock.mockRestore();
  }
});

it("expires an open approval form at the nominal posting end without another source read", async () => {
  vi.useFakeTimers();
  const current = Date.now();
  let sourceReads = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => {
      sourceReads++;
      return Response.json(source);
    }),
  );
  const rendered = renderReview(
    <PostingProposalDecision
      scope={{ ...scope, hospitalId: hospital, staffId: staff, access: "scoped" }}
      proposal={{
        ...proposal,
        status: "pending",
        ends_at: new Date(current + 30_000).toISOString(),
      }}
      action="approve"
      busy={false}
      onBusy={() => {}}
      onCompleted={() => {}}
      onContextChanged={() => {}}
      contextAvailable
      onContextRecheck={() => {}}
    />,
  );
  try {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(screen.getByRole("button", { name: "Approve proposal" })).toBeEnabled();
    const before = sourceReads;
    await act(async () => {
      await vi.advanceTimersByTimeAsync(29_999);
    });
    expect(screen.getByRole("button", { name: "Approve proposal" })).toBeDisabled();
    expect(
      screen.getByText("This posting has ended. A new approval cannot be recorded."),
    ).toBeVisible();
    expect(sourceReads).toBe(before);
  } finally {
    rendered.unmount();
    vi.useRealTimers();
  }
});

it("submits a named canonical supervisor reference for a department member", async () => {
  const supervisor = supervisorChoice();
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("posting-supervisor-choices"))
        return Response.json({ data: [supervisor], page: { has_more: false, next_cursor: null } });
      if (init?.method === "PUT") writes.push(String(init.body));
      return submissionResponse(url, init);
    }),
  );
  const { user, form } = await openSubmission();
  await screen.findByText("Current department: SYN-PHARMA · Synthetic Pharmacy");
  await choosePostingResponsibility(user, form, "department_member");
  expect(within(form).getByRole("button", { name: "Request independent review" })).toBeDisabled();
  expect(
    await within(form).findByRole("button", { name: "Choose supervisor Synthetic Supervisor" }),
  ).toBeEnabled();
  await user.click(
    await within(form).findByRole("button", { name: "Choose supervisor Synthetic Supervisor" }),
  );
  expect(within(form).getByRole("button", { name: "Request independent review" })).toBeEnabled();
  await user.type(
    within(form).getByLabelText("Submission rationale"),
    "Synthetic independently reviewed Pharmacy department membership",
  );
  await user.click(
    within(form).getByLabelText("I examined this draft and the current employment source."),
  );
  await user.click(within(form).getByRole("button", { name: "Request independent review" }));
  await screen.findByText(
    "Posting proposal submitted for independent review. Operating authority is unchanged.",
  );
  expect(writes).toHaveLength(1);
  expect(JSON.parse(writes[0]).supervision).toEqual({
    kind: "department_member",
    supervisor_staff_id: supervisor.staff.id,
    supervisor_proposal_id: supervisor.id,
    supervisor_revision_id: supervisor.revision_id,
  });
  expect(JSON.parse(writes[0]).supervision).not.toHaveProperty("display_name");
});

function supervisorChoice(n = 51, name = "Synthetic Supervisor") {
  return {
    id: id(n),
    revision_id: id(n + 1),
    hospital_id: hospital,
    staff: { id: id(n - 1), employee_code: `SYN-SUP-${n}`, display_name: name },
    kind: "hospital_management" as const,
    department_id: context.draft.department_id,
    department_revision_id: context.draft.department_revision_id,
    starts_at: context.draft.starts_at,
    ends_at: context.draft.ends_at,
    eligibility_until: source.eligibility_until,
    created_at: "2026-11-04T00:00:01Z",
  };
}

it("retains an exact supervised department-manager submission through an uncertain response", async () => {
  const supervisor = supervisorChoice();
  const writes: { url: string; body: string }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("posting-supervisor-choices"))
        return Response.json({ data: [supervisor], page: { has_more: false, next_cursor: null } });
      if (init?.method === "PUT") {
        writes.push({ url, body: String(init.body) });
        if (writes.length === 1) throw new TypeError("Synthetic lost supervised proposal response");
      }
      return submissionResponse(url, init);
    }),
  );
  const { user, form } = await openSubmission();
  await screen.findByText("Current department: SYN-PHARMA · Synthetic Pharmacy");
  await choosePostingResponsibility(user, form, "department_supervisor");
  await user.click(
    await within(form).findByRole("button", { name: "Choose supervisor Synthetic Supervisor" }),
  );
  await user.type(
    within(form).getByLabelText("Submission rationale"),
    "Synthetic Pharmacy supervisor under a reviewed hospital manager",
  );
  await user.click(
    within(form).getByLabelText("I examined this draft and the current employment source."),
  );
  await user.click(within(form).getByRole("button", { name: "Request independent review" }));
  await screen.findByRole("button", { name: "Retry exact submission" });
  expect(within(form).getByLabelText("Posting responsibility")).toBeDisabled();
  expect(
    within(form).getByRole("button", { name: "Choose supervisor Synthetic Supervisor" }),
  ).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry exact submission" }));
  await screen.findByText(
    "Posting proposal submitted for independent review. Operating authority is unchanged.",
  );
  expect(writes).toHaveLength(2);
  expect(writes[1]).toEqual(writes[0]);
  expect(JSON.parse(writes[0].body).supervision).toEqual({
    kind: "department_supervisor",
    supervisor_staff_id: supervisor.staff.id,
    supervisor_proposal_id: supervisor.id,
    supervisor_revision_id: supervisor.revision_id,
  });
});

it("clears the supervisor selection when paging and excludes the target Staff from choices", async () => {
  const first = supervisorChoice(),
    next = supervisorChoice(41, "Synthetic Second Supervisor");
  const self = {
    ...supervisorChoice(71, "Synthetic Target Self"),
    staff: { ...supervisorChoice(71).staff, id: staff },
  };
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("posting-supervisor-choices"))
        return Response.json(
          url.includes("cursor=next")
            ? { data: [next], page: { has_more: false, next_cursor: null } }
            : { data: [self, first], page: { has_more: true, next_cursor: "next" } },
        );
      if (init?.method === "PUT") writes.push(String(init.body));
      return submissionResponse(url, init);
    }),
  );
  const { user, form } = await openSubmission();
  await screen.findByText("Current department: SYN-PHARMA · Synthetic Pharmacy");
  await choosePostingResponsibility(user, form, "department_member");
  await user.click(
    await within(form).findByRole("button", { name: "Choose supervisor Synthetic Supervisor" }),
  );
  expect(
    within(form).queryByRole("button", { name: "Choose supervisor Synthetic Target Self" }),
  ).not.toBeInTheDocument();
  await user.click(within(form).getByRole("button", { name: "More supervisor references" }));
  await within(form).findByRole("button", {
    name: "Choose supervisor Synthetic Second Supervisor",
  });
  expect(within(form).getByRole("button", { name: "Request independent review" })).toBeDisabled();
  expect(writes).toHaveLength(0);
  await user.click(
    within(form).getByRole("button", { name: "Choose supervisor Synthetic Second Supervisor" }),
  );
  expect(within(form).getByRole("button", { name: "Request independent review" })).toBeEnabled();
});

it("withholds a supervised submission when the candidate page is unavailable", async () => {
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("posting-supervisor-choices"))
        return Response.json(
          { error: "posting_source_unavailable", message: "Synthetic owner outage" },
          { status: 503 },
        );
      if (init?.method === "PUT") writes.push(String(init.body));
      return submissionResponse(url, init);
    }),
  );
  const { user, form } = await openSubmission();
  await screen.findByText("Current department: SYN-PHARMA · Synthetic Pharmacy");
  await choosePostingResponsibility(user, form, "department_member");
  await within(form).findByRole("button", { name: "Retry supervisor references" });
  expect(within(form).getByRole("button", { name: "Request independent review" })).toBeDisabled();
  expect(within(form).getByLabelText("Posting responsibility")).toBeEnabled();
  expect(writes).toHaveLength(0);
});

async function choosePostingResponsibility(
  user: ReturnType<typeof renderReview>["user"],
  form: HTMLElement,
  value: "unspecified" | "hospital_management" | "department_supervisor" | "department_member",
) {
  const labels = {
    unspecified: "Preparation only",
    hospital_management: "Hospital management",
    department_supervisor: "Department supervisor",
    department_member: "Department member",
  };
  await user.click(within(form).getByRole("combobox", { name: "Posting responsibility" }));
  await user.click(await screen.findByRole("option", { name: labels[value] }));
}

it("clears supervisor disclosure when its scoped read permission is lost", async () => {
  const changed = vi.fn(),
    supervisor = supervisorChoice();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("posting-supervisor-choices"))
        return url.includes("cursor=next")
          ? Response.json(
              { error: "forbidden", message: "Synthetic current permission lost" },
              { status: 403 },
            )
          : Response.json({ data: [supervisor], page: { has_more: true, next_cursor: "next" } });
      return submissionResponse(url, init);
    }),
  );
  const { user, form } = await openSubmission(changed);
  await screen.findByText("Current department: SYN-PHARMA · Synthetic Pharmacy");
  await choosePostingResponsibility(user, form, "department_member");
  await user.click(
    await within(form).findByRole("button", { name: "Choose supervisor Synthetic Supervisor" }),
  );
  await user.click(within(form).getByRole("button", { name: "More supervisor references" }));
  await waitFor(() => expect(changed).toHaveBeenCalledTimes(1));
  expect(
    screen.queryByRole("button", { name: "Choose supervisor Synthetic Supervisor" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Request independent review" }),
  ).not.toBeInTheDocument();
});

it("requires current named chain review before approving a departmental posting", async () => {
  const parent = supervisorChoice();
  const supervision = {
    kind: "department_member",
    supervisor_staff_id: parent.staff.id,
    supervisor_proposal_id: parent.id,
    supervisor_revision_id: parent.revision_id,
  };
  const writes: Record<string, unknown>[] = [];
  let complete = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("lifecycle_review=current"))
        return lifecycleContextResponse(context, supervision, [
          { posting: parent, supervision: { kind: "hospital_management" } },
        ]);
      if (init?.method === "PUT" && url.includes("/decisions/")) {
        writes.push(JSON.parse(String(init.body)));
        complete = true;
        return Response.json({
          ...proposal,
          status: "approved",
          version: 2,
          revision_id: id(99),
          command_id: url.split("/").at(-1),
          recorded_at: "2026-11-04T01:00:00Z",
        });
      }
      return reviewerResponse(url, supervision, !complete);
    }),
  );
  const { user } = await openReviewer();
  expect(screen.queryByRole("button", { name: "Approve proposal" })).not.toBeInTheDocument();
  await user.click(await screen.findByRole("button", { name: "Review supervisor chain approval" }));
  expect(await screen.findByText("Synthetic Supervisor")).toBeInTheDocument();
  await user.type(
    screen.getByLabelText("Decision rationale"),
    "Independent current chain review for the department member",
  );
  await user.click(
    screen.getByLabelText("I examined the submitted proposal and current employment source."),
  );
  await user.click(screen.getByRole("button", { name: "Approve proposal" }));
  expect(writes).toHaveLength(0);
  await user.click(screen.getByLabelText("I independently reviewed the current supervisor chain."));
  await user.click(screen.getByRole("button", { name: "Approve proposal" }));
  await screen.findByText("Posting proposal approved.");
  expect(writes).toEqual([
    {
      expected_version: 1,
      expected_revision_id: proposal.revision_id,
      decision: "approve",
      reason: "Independent current chain review for the department member",
      sources_examined: true,
      lifecycle_review: { cutoff_versions: [0, 0], effective_until: source.eligibility_until },
    },
  ]);
});

it.each([403, 503, 409])(
  "withholds departmental approval when current chain review returns %s",
  async (status) => {
    const parent = supervisorChoice();
    const supervision = {
      kind: "department_member",
      supervisor_staff_id: parent.staff.id,
      supervisor_proposal_id: parent.id,
      supervisor_revision_id: parent.revision_id,
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input);
        if (url.includes("lifecycle_review=current"))
          return Response.json(
            {
              error: status === 403 ? "forbidden" : "posting_proposal_conflict",
              message: "Synthetic chain unavailable",
            },
            { status },
          );
        return reviewerResponse(url, supervision);
      }),
    );
    const { user } = await openReviewer();
    await user.click(
      await screen.findByRole("button", { name: "Review supervisor chain approval" }),
    );
    if (status === 403) await screen.findByText(/access changed/i);
    else {
      await screen.findByText(/selected proposal could not be verified/i);
      await user.click(screen.getByRole("button", { name: "Back to proposal review" }));
      await screen.findByRole("button", { name: "Reject proposal" });
    }
    expect(screen.queryByRole("button", { name: "Approve proposal" })).not.toBeInTheDocument();
    expect(screen.queryByText("Synthetic Supervisor")).not.toBeInTheDocument();
  },
);

it("retains the exact supervised approval without adding a management attestation", async () => {
  const parent = supervisorChoice();
  const supervision = {
    kind: "department_supervisor",
    supervisor_staff_id: parent.staff.id,
    supervisor_proposal_id: parent.id,
    supervisor_revision_id: parent.revision_id,
  };
  const writes: { url: string; body: string }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("lifecycle_review=current"))
        return lifecycleContextResponse(context, supervision, [
          { posting: parent, supervision: { kind: "hospital_management" } },
        ]);
      if (init?.method === "PUT" && url.includes("/decisions/")) {
        writes.push({ url, body: String(init.body) });
        throw new TypeError("Synthetic response lost after supervised command");
      }
      return reviewerResponse(url, supervision);
    }),
  );
  const { user } = await openReviewer();
  await user.click(await screen.findByRole("button", { name: "Review supervisor chain approval" }));
  await screen.findByText("Synthetic Supervisor");
  await user.type(
    screen.getByLabelText("Decision rationale"),
    "Independent chain attestation for uncertain command recovery",
  );
  await user.click(
    screen.getByLabelText("I examined the submitted proposal and current employment source."),
  );
  await user.click(screen.getByLabelText("I independently reviewed the current supervisor chain."));
  await user.click(screen.getByRole("button", { name: "Approve proposal" }));
  await user.click(await screen.findByRole("button", { name: "Retry exact decision" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]).toEqual(writes[0]);
  expect(JSON.parse(writes[0].body)).not.toHaveProperty("hospital_management_reviewed");
  expect(
    screen.getByLabelText("I independently reviewed the current supervisor chain."),
  ).toBeDisabled();
});

it("binds a scheduled supervisor ending into the normal approval workflow", async () => {
  const parent = supervisorChoice();
  const supervision = {
    kind: "department_member",
    supervisor_staff_id: parent.staff.id,
    supervisor_proposal_id: parent.id,
    supervisor_revision_id: parent.revision_id,
  };
  const observation = { id: id(301), version: 1, cutoff_at: "2026-11-04T03:00:00Z" };
  const writes: { url: string; input: Record<string, unknown> }[] = [];
  let complete = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("lifecycle_review=current"))
        return Response.json({
          context,
          supervision,
          cutoff: { id: null, version: 0, cutoff_at: null },
          chain: [
            { posting: parent, supervision: { kind: "hospital_management" }, cutoff: observation },
          ],
          checked_at: source.checked_at,
          effective_until: observation.cutoff_at,
        });
      if (url.includes("chain_review=current"))
        return Response.json({
          context,
          supervision,
          chain: [{ posting: parent, supervision: { kind: "hospital_management" } }],
          checked_at: source.checked_at,
          eligibility_until: source.eligibility_until,
        });
      if (init?.method === "PUT" && url.includes("/decisions/")) {
        writes.push({ url, input: JSON.parse(String(init.body)) });
        complete = true;
        return Response.json({
          ...proposal,
          status: "approved",
          version: 2,
          revision_id: id(302),
          command_id: url.split("/").at(-1),
          recorded_at: "2026-11-04T01:00:00Z",
        });
      }
      return reviewerResponse(url, supervision, !complete);
    }),
  );
  const { user } = await openReviewer();
  await user.click(await screen.findByRole("button", { name: "Review supervisor chain approval" }));
  expect(await screen.findByText("Effective responsibility ends (UTC)")).toBeInTheDocument();
  expect(screen.getAllByText("2026-11-04T03:00:00.000Z").length).toBeGreaterThan(0);
  await user.type(
    screen.getByLabelText("Decision rationale"),
    "Examined every scheduled source ending",
  );
  await user.click(screen.getByLabelText(/I examined.*current employment/));
  await user.click(screen.getByLabelText("I independently reviewed the current supervisor chain."));
  await user.click(screen.getByRole("button", { name: "Approve proposal" }));
  await screen.findByText("Posting proposal approved.");
  expect(writes).toHaveLength(1);
  expect(writes[0].input.lifecycle_review).toEqual({
    cutoff_versions: [0, 1],
    effective_until: observation.cutoff_at,
  });
});

function scopedLifecycleScenario() {
  const parent = supervisorChoice();
  const supervision = {
    kind: "department_member",
    supervisor_staff_id: parent.staff.id,
    supervisor_proposal_id: parent.id,
    supervisor_revision_id: parent.revision_id,
  };
  const envelope = (version: number) => ({
    context,
    supervision,
    cutoff: { id: null, version: 0, cutoff_at: null },
    chain: [
      {
        posting: parent,
        supervision: { kind: "hospital_management" },
        cutoff: {
          id: id(310 + version),
          version,
          cutoff_at: version === 1 ? "2026-11-04T03:00:00Z" : "2026-11-04T02:00:00Z",
        },
      },
    ],
    checked_at: source.checked_at,
    effective_until: version === 1 ? "2026-11-04T03:00:00Z" : "2026-11-04T02:00:00Z",
  });
  return { supervision, envelope };
}

it("requires renewed examination after a retained source ending changes without losing rationale", async () => {
  const { supervision, envelope } = scopedLifecycleScenario();
  let version = 1,
    complete = false;
  const writes: Record<string, unknown>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("lifecycle_review=current")) return Response.json(envelope(version));
      if (init?.method === "PUT") {
        writes.push(JSON.parse(String(init.body)));
        complete = true;
        return Response.json({
          ...proposal,
          status: "approved",
          version: 2,
          revision_id: id(320),
          command_id: url.split("/").at(-1),
          recorded_at: "2026-11-04T01:00:00Z",
        });
      }
      return reviewerResponse(url, supervision, !complete);
    }),
  );
  const { user } = await openReviewer();
  await user.click(await screen.findByRole("button", { name: "Review supervisor chain approval" }));
  await user.type(
    await screen.findByLabelText("Decision rationale"),
    "Retain the reason while ending observations change",
  );
  const examined = screen.getByLabelText(
    "I examined the submitted proposal and current employment source.",
  );
  const independent = screen.getByLabelText(
    "I independently reviewed the current supervisor chain.",
  );
  await user.click(examined);
  await user.click(independent);
  version = 2;
  await user.click(screen.getByRole("button", { name: "Recheck posting lifetimes" }));
  await waitFor(() =>
    expect(screen.getAllByText("2026-11-04T02:00:00.000Z").length).toBeGreaterThan(0),
  );
  expect(examined).not.toBeChecked();
  expect(independent).not.toBeChecked();
  expect(screen.getByLabelText("Decision rationale")).toHaveValue(
    "Retain the reason while ending observations change",
  );
  await user.click(screen.getByRole("button", { name: "Approve proposal" }));
  expect(writes).toHaveLength(0);
  await user.click(examined);
  await user.click(independent);
  await user.click(screen.getByRole("button", { name: "Approve proposal" }));
  await screen.findByText("Posting proposal approved.");
  expect(writes[0].lifecycle_review).toEqual({
    cutoff_versions: [0, 2],
    effective_until: envelope(2).effective_until,
  });
});

it.each(["changed", "unavailable"])(
  "recovers the original approval after %s context and elapsed eligibility using the identical PUT",
  async (kind) => {
    const { supervision, envelope } = scopedLifecycleScenario();
    const writes: { url: string; body: string }[] = [];
    let refresh = false,
      complete = false;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const url = String(input);
        if (url.includes("lifecycle_review=current"))
          return refresh && kind === "unavailable"
            ? Response.json(
                { error: "unavailable", message: "Synthetic owner unavailable" },
                { status: 503 },
              )
            : Response.json(envelope(refresh ? 2 : 1));
        if (url.includes("/decisions/")) {
          expect(init?.method).toBe("PUT");
          writes.push({ url, body: String(init?.body) });
          if (writes.length === 1)
            throw new TypeError("Synthetic response lost after approval committed");
          complete = true;
          return Response.json({
            ...proposal,
            status: "approved",
            version: 2,
            revision_id: id(321),
            command_id: url.split("/").at(-1),
            recorded_at: "2026-11-04T01:00:00Z",
          });
        }
        return reviewerResponse(url, supervision, !complete);
      }),
    );
    const { user } = await openReviewer();
    await user.click(
      await screen.findByRole("button", { name: "Review supervisor chain approval" }),
    );
    await user.type(
      await screen.findByLabelText("Decision rationale"),
      "Retain this exact examined approval for recovery",
    );
    await user.click(
      screen.getByLabelText("I examined the submitted proposal and current employment source."),
    );
    await user.click(
      screen.getByLabelText("I independently reviewed the current supervisor chain."),
    );
    await user.click(screen.getByRole("button", { name: "Approve proposal" }));
    await screen.findByRole("button", { name: "Recover or replay approval" });
    refresh = true;
    await user.click(screen.getByRole("button", { name: "Recheck posting lifetimes" }));
    if (kind === "changed")
      await waitFor(() =>
        expect(screen.getAllByText("2026-11-04T02:00:00.000Z").length).toBeGreaterThan(0),
      );
    else await screen.findByText(/selected proposal could not be verified/i);
    vi.setSystemTime(new Date("2026-11-04T04:00:00Z"));
    await user.click(screen.getByRole("button", { name: "Retry exact decision" }));
    expect(writes).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "Recover or replay approval" }));
    await screen.findByText("Posting proposal approved.");
    expect(writes).toHaveLength(2);
    expect(writes[1]).toEqual(writes[0]);
    expect(JSON.parse(writes[1].body).lifecycle_review).toEqual({
      cutoff_versions: [0, 1],
      effective_until: envelope(1).effective_until,
    });
  },
);

it("clears lifecycle source names and retained intent after scoped review revocation", async () => {
  const { supervision, envelope } = scopedLifecycleScenario();
  let revoked = false;
  const changed = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes("lifecycle_review=current"))
        return revoked
          ? Response.json(
              { error: "forbidden", message: "Synthetic reviewer permission revoked" },
              { status: 403 },
            )
          : Response.json(envelope(1));
      if (init?.method === "PUT") throw new TypeError("Synthetic unknown approval outcome");
      return reviewerResponse(url, supervision);
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
  await user.click(await screen.findByRole("button", { name: "Review supervisor chain approval" }));
  await user.type(
    await screen.findByLabelText("Decision rationale"),
    "Synthetic private retained lifecycle rationale",
  );
  await user.click(
    screen.getByLabelText("I examined the submitted proposal and current employment source."),
  );
  await user.click(screen.getByLabelText("I independently reviewed the current supervisor chain."));
  await user.click(screen.getByRole("button", { name: "Approve proposal" }));
  await screen.findByRole("button", { name: "Recover or replay approval" });
  revoked = true;
  await user.click(screen.getByRole("button", { name: "Recheck posting lifetimes" }));
  await screen.findByText(/Staff access changed/);
  expect(screen.queryByText("Synthetic Supervisor")).not.toBeInTheDocument();
  expect(
    screen.queryByDisplayValue("Synthetic private retained lifecycle rationale"),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Recover or replay approval" }),
  ).not.toBeInTheDocument();
  expect(changed).toHaveBeenCalledTimes(1);
});

it("aborts an in-flight lifecycle approval and clears its private intent on reviewer account change", async () => {
  const { supervision, envelope } = scopedLifecycleScenario();
  let switched = false;
  let release: ((value: Response) => void) | undefined;
  let signal: AbortSignal | undefined;
  let commandId = "";
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (init?.method === "PUT") {
        signal = init.signal ?? undefined;
        commandId = url.split("/").at(-1)!;
        return new Promise<Response>((resolve) => {
          release = resolve;
        });
      }
      if (switched)
        return Response.json({ data: [], page: { has_more: false, next_cursor: null } });
      if (url.includes("lifecycle_review=current")) return Response.json(envelope(1));
      return reviewerResponse(url, supervision);
    }),
  );
  function AccountBoundary() {
    const [subject, setSubject] = useState(scope.subject);
    return (
      <>
        <button
          onClick={() => {
            switched = true;
            setSubject("synthetic-other-reviewer");
          }}
        >
          Switch reviewer account
        </button>
        <StaffPostingReviewWorkspace scope={{ ...scope, subject }} onContextChanged={() => {}} />
      </>
    );
  }
  const { user } = renderReview(<AccountBoundary />);
  await user.click(
    await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" }),
  );
  await user.click(
    await screen.findByRole("button", { name: "Open proposal for Synthetic Staff" }),
  );
  await user.click(await screen.findByRole("button", { name: "Review supervisor chain approval" }));
  await user.type(
    await screen.findByLabelText("Decision rationale"),
    "Private first-reviewer lifecycle intent",
  );
  await user.click(
    screen.getByLabelText("I examined the submitted proposal and current employment source."),
  );
  await user.click(screen.getByLabelText("I independently reviewed the current supervisor chain."));
  await user.click(screen.getByRole("button", { name: "Approve proposal" }));
  await waitFor(() => expect(release).toBeTypeOf("function"));
  await user.click(screen.getByRole("button", { name: "Switch reviewer account" }));
  expect(signal?.aborted).toBe(true);
  expect(
    screen.queryByDisplayValue("Private first-reviewer lifecycle intent"),
  ).not.toBeInTheDocument();
  await act(async () => {
    release!(
      Response.json({
        ...proposal,
        status: "approved",
        version: 2,
        revision_id: id(322),
        command_id: commandId,
        recorded_at: "2026-11-04T01:00:00Z",
      }),
    );
  });
  expect(screen.queryByText("Posting proposal approved.")).not.toBeInTheDocument();
  expect(screen.queryByText("Synthetic Supervisor")).not.toBeInTheDocument();
});

function approvedHistoryResponse() {
  const approved = {
    ...proposal,
    version: 2,
    revision_id: id(301),
    command_id: id(302),
    status: "approved",
    recorded_at: "2026-11-03T03:00:00Z",
  };
  return {
    context: {
      ...context,
      proposal: approved,
      decision_reason: "Synthetic independent historical approval",
    },
    supervision: { kind: "hospital_management" },
    review: {
      revision_id: approved.revision_id,
      initial_revision_id: proposal.revision_id,
      reviewer_staff_id: id(303),
      reviewer_link_id: id(304),
      reviewer_generation_id: id(305),
      review_assignment_id: id(306),
      review_assignment_version: 2,
      scope_epoch: 1,
      membership_epoch: 2,
      support_permission: "workforce.posting.management.review",
      support_assignment_id: id(307),
      support_assignment_version: 3,
      checked_at: "2026-11-03T02:00:00Z",
      effective_until: "2026-11-05T00:00:00Z",
      source_count: 1,
      sources: [
        {
          hop: 0,
          staff_id: staff,
          proposal_id: proposal.id,
          revision_id: proposal.revision_id,
          cutoff: { id: null, version: 0, cutoff_at: null },
        },
      ],
    },
  };
}

function approvedHistoryFetcher(
  onRecord: (signal?: AbortSignal | null) => Response | Promise<Response>,
  history = approvedHistoryResponse(),
) {
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    if (url.includes("posting-review-scopes"))
      return Response.json({
        data: [
          {
            hospital_id: hospital,
            name: "Synthetic Hospital",
            can_review: true,
            can_submit: false,
          },
        ],
        page: { has_more: false, next_cursor: null },
      });
    if (url.includes("posting-proposal-inbox"))
      return Response.json({
        data: url.includes("status=approved")
          ? [
              {
                staff: { id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" },
                is_submitter: false,
                proposal: history.context.proposal,
              },
            ]
          : [],
        page: { has_more: false, next_cursor: null },
      });
    if (url.includes("/context"))
      return new URL(url, "http://localhost").searchParams.get("lifecycle_review") === "recorded"
        ? onRecord(init?.signal)
        : postingContextResponse(url, history.context, history.supervision);
    throw new Error(`Unexpected history request: ${url}`);
  });
}
async function openApprovedHistory(changed = vi.fn(), ui?: ReactElement) {
  const view = renderReview(
    ui ?? <StaffPostingReviewWorkspace scope={scope} onContextChanged={changed} />,
  );
  await view.user.click(
    await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" }),
  );
  await view.user.click(screen.getByRole("combobox", { name: "Proposal status" }));
  await view.user.click(await screen.findByRole("option", { name: "Approved" }));
  await view.user.click(
    await screen.findByRole("button", { name: "Open proposal for Synthetic Staff" }),
  );
  return { ...view, changed };
}

it("shows retained approval observations after their deadline without presenting operating authority", async () => {
  vi.setSystemTime(new Date("2026-11-06T00:00:00Z"));
  const history = approvedHistoryResponse();
  const fetcher = approvedHistoryFetcher(() => Response.json(history));
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderReview(
    <StaffPostingReviewWorkspace scope={scope} onContextChanged={vi.fn()} />,
  );
  await user.click(
    await screen.findByRole("button", { name: "Open posting reviews for Synthetic Hospital" }),
  );
  await user.click(screen.getByRole("combobox", { name: "Proposal status" }));
  await user.click(await screen.findByRole("option", { name: "Approved" }));
  await user.click(
    await screen.findByRole("button", { name: "Open proposal for Synthetic Staff" }),
  );
  const retained = await screen.findByRole("region", { name: "Retained lifecycle approval" });
  expect(within(retained).getByText("2026-11-05T00:00:00.000Z")).toBeInTheDocument();
  expect(
    within(retained).getByText(/does not establish current operating authority/i),
  ).toBeInTheDocument();
  expect(screen.getByText("Synthetic independent historical approval")).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Approve posting proposal" }),
  ).not.toBeInTheDocument();
  expect(
    fetcher.mock.calls.some(([url]) => String(url).includes("lifecycle_review=recorded")),
  ).toBe(true);
});

it("shows unavailable historical proof explicitly without requesting current observations", async () => {
  const history = approvedHistoryResponse();
  const fetcher = approvedHistoryFetcher(() => Response.json({ ...history, review: null }));
  vi.stubGlobal("fetch", fetcher);
  await openApprovedHistory();
  const retained = await screen.findByRole("region", { name: "Retained lifecycle approval" });
  expect(
    within(retained).getByText(/No retained lifecycle proof is available/),
  ).toBeInTheDocument();
  expect(
    within(retained).queryByRole("list", { name: "Sources retained at approval" }),
  ).not.toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => String(url).includes("lifecycle_review=current"))).toBe(
    false,
  );
});

it("withholds cached approval history when current access cannot be rechecked", async () => {
  let reads = 0;
  const history = approvedHistoryResponse();
  vi.stubGlobal(
    "fetch",
    approvedHistoryFetcher(() =>
      ++reads === 1
        ? Response.json(history)
        : Response.json(
            { error: "unavailable", message: "Current authority unavailable" },
            { status: 503 },
          ),
    ),
  );
  const view = await openApprovedHistory();
  await screen.findByRole("region", { name: "Retained lifecycle approval" });
  await view.user.click(screen.getByRole("button", { name: "Recheck history access" }));
  await screen.findByText(/The retained approval history could not be verified/);
  expect(screen.queryByText("Synthetic independent historical approval")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("region", { name: "Retained lifecycle approval" }),
  ).not.toBeInTheDocument();
  expect(view.changed).not.toHaveBeenCalled();
});

it("clears retained approval evidence when the reviewer loses current access", async () => {
  let reads = 0;
  const history = approvedHistoryResponse();
  vi.stubGlobal(
    "fetch",
    approvedHistoryFetcher(() =>
      ++reads === 1
        ? Response.json(history)
        : Response.json(
            { error: "forbidden", message: "Current review access ended" },
            { status: 403 },
          ),
    ),
  );
  const view = await openApprovedHistory();
  await screen.findByRole("region", { name: "Retained lifecycle approval" });
  await view.user.click(screen.getByRole("button", { name: "Recheck history access" }));
  await waitFor(() => expect(view.changed).toHaveBeenCalled());
  expect(screen.queryByText("Synthetic independent historical approval")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("region", { name: "Retained lifecycle approval" }),
  ).not.toBeInTheDocument();
});

it("aborts an in-flight historical disclosure when the account changes", async () => {
  let release!: (response: Response) => void;
  let originalSignal: AbortSignal | null | undefined;
  let switched = false;
  const history = approvedHistoryResponse();
  const original = approvedHistoryFetcher((signal) => {
    originalSignal = signal;
    return new Promise<Response>((resolve) => {
      release = resolve;
    });
  });
  vi.stubGlobal(
    "fetch",
    vi.fn((input: RequestInfo | URL, init?: RequestInit) =>
      switched
        ? Promise.resolve(Response.json({ data: [], page: { has_more: false, next_cursor: null } }))
        : original(input, init),
    ),
  );
  const changed = vi.fn();
  function HistoryAccountHarness() {
    const [subject, setSubject] = useState(scope.subject);
    return (
      <>
        <button
          onClick={() => {
            switched = true;
            setSubject("synthetic-next-reviewer");
          }}
        >
          Change history account
        </button>
        <StaffPostingReviewWorkspace scope={{ ...scope, subject }} onContextChanged={changed} />
      </>
    );
  }
  const view = await openApprovedHistory(changed, <HistoryAccountHarness />);
  await waitFor(() => expect(originalSignal).toBeDefined());
  await view.user.click(screen.getByRole("button", { name: "Change history account" }));
  await waitFor(() => expect(originalSignal?.aborted).toBe(true));
  await act(async () => release(Response.json(history)));
  await screen.findByText("No approved posting review hospitals on this page.");
  expect(screen.queryByText("Synthetic independent historical approval")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("region", { name: "Retained lifecycle approval" }),
  ).not.toBeInTheDocument();
});

it("preserves the original microsecond timestamps in retained approval evidence", async () => {
  const history = approvedHistoryResponse();
  history.review.checked_at = "2026-11-03T02:00:00.123456Z";
  history.review.effective_until = "2026-11-04T23:59:59.987654Z";
  history.context.proposal.eligibility_until = history.review.effective_until;
  vi.stubGlobal(
    "fetch",
    approvedHistoryFetcher(() => Response.json(history), history),
  );
  await openApprovedHistory();
  const retained = await screen.findByRole("region", { name: "Retained lifecycle approval" });
  expect(within(retained).getByText("2026-11-03T02:00:00.123456Z")).toBeInTheDocument();
  expect(within(retained).getByText("2026-11-04T23:59:59.987654Z")).toBeInTheDocument();
});
