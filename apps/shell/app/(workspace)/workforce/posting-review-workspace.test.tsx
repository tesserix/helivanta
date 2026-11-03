import { useState } from "react";
import "@/test/select";
import { renderWithProviders } from "@helivanta/api/testing";
import { screen } from "@testing-library/react";
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
afterEach(() => vi.unstubAllGlobals());

it("discovers reviewer hospitals and opens only the selected exact-head protected context", async () => {
  const paths: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      paths.push(url);
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
  const { user } = renderWithProviders(
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
            error: {
              code: status === 403 ? "forbidden" : "unavailable",
              message: "Synthetic protected refusal",
            },
          },
          { status },
        );
      }),
    );
    const { user } = renderWithProviders(
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
  const { user } = renderWithProviders(<AccountHarness />);
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
