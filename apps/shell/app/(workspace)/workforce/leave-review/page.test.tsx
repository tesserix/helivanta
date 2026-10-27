import "@/test/select";
import { act, screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { beginTenantTransition, cancelTenantTransition } from "@helivanta/api";
import ReviewLeave from "./page";

const tenant = "11111111-1111-4111-8111-111111111111";
const hospital = "22222222-2222-4222-8222-222222222222";
const staff = "33333333-3333-4333-8333-333333333333";
const id = "44444444-4444-4444-8444-444444444444";
const row = {
  id,
  staff_id: staff,
  employee_code: "SYN-001",
  display_name: "Synthetic Nurse",
  code: "SYN-CL",
  name: "Synthetic Casual",
  leave_year: 2026,
  unit: "full_day",
  start_on: "2026-11-02",
  end_on: "2026-11-02",
  half_days: 2,
  status: "pending",
  version: 3,
  created_at: "2026-10-27T00:00:00Z",
};
const page = (data: unknown[], cursor: string | null = null) => ({
  data,
  page: { has_more: cursor !== null, next_cursor: cursor },
});
afterEach(() => {
  act(() => cancelTenantTransition());
  vi.unstubAllGlobals();
});

function mock({ uncertain = false, emptyFirst = false, unavailable = false, rejected = 0 } = {}) {
  const writes: { body: unknown; tenant: string | null }[] = [];
  const reads: string[] = [];
  let done = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      reads.push(url);
      if (url.includes("/iam/me/permissions"))
        return Response.json({ tenant_id: tenant, subject: "synthetic-reviewer", data: [] });
      if (init?.method === "POST") {
        writes.push({
          body: JSON.parse(String(init.body)),
          tenant: new Headers(init.headers).get("X-Helivanta-Expected-Tenant"),
        });
        if (rejected)
          return Response.json(
            { error: "conflict", message: "Synthetic refused decision." },
            { status: rejected },
          );
        if (uncertain && writes.length === 1)
          return Response.json(
            { error: "upstream_unavailable", message: "The result could not be confirmed." },
            { status: 502 },
          );
        done = true;
        return Response.json({
          request: { ...row, status: "approved", version: 4 },
          days: [],
          roster_clashes: [],
        });
      }
      if (url.includes("/leave-review-scopes")) {
        if (unavailable)
          return Response.json(
            { error: "scoped_authorization_unavailable", message: "Access unavailable." },
            { status: 503 },
          );
        return Response.json(
          emptyFirst && !url.includes("cursor=")
            ? page([], "next")
            : page([{ hospital_id: hospital, name: "Synthetic Hospital" }]),
        );
      }
      if (url.endsWith("/leave-review-requests")) return Response.json(page(done ? [] : [row]));
      if (url.endsWith(`/leave-review-requests/${id}`))
        return Response.json({
          request: { ...row, reason: "Synthetic private reason" },
          days: [{ day: row.start_on, half_days: 2, holiday: false, calendar_revision_id: id }],
          roster_clashes: [],
        });
      throw new Error(`Unexpected request ${url}`);
    }),
  );
  return { writes, reads };
}

it("reviews leave without directory or tenant-wide business permissions", async () => {
  const { writes, reads } = mock();
  const { user } = renderWithProviders(<ReviewLeave />);
  await user.click(await screen.findByRole("button", { name: /Review leave for Synthetic Nurse/ }));
  const region = await screen.findByRole("region", { name: "Review leave request" });
  expect(await within(region).findByText(/Synthetic private reason/)).toBeInTheDocument();
  await user.click(within(region).getByRole("button", { name: "Reject leave" }));
  expect(
    await within(region).findByText("Add a note explaining the decision."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.type(within(region).getByLabelText("Decision note"), "Synthetic coverage review");
  await user.click(within(region).getByRole("button", { name: "Approve leave" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    tenant,
    body: { expected_version: 3, decision: "approve", note: "Synthetic coverage review" },
  });
  expect(
    reads.some((url) => url.includes("/hr/") || url.endsWith("/staff") || url.endsWith("/history")),
  ).toBe(false);
  expect(await screen.findByText("No pending requests need your review.")).toBeInTheDocument();
});

it("retries an uncertain decision with exactly the original content and version", async () => {
  const { writes } = mock({ uncertain: true });
  const { user } = renderWithProviders(<ReviewLeave />);
  await user.click(await screen.findByRole("button", { name: /Review leave for Synthetic Nurse/ }));
  const region = await screen.findByRole("region", { name: "Review leave request" });
  const note = await within(region).findByLabelText("Decision note");
  await user.type(note, "Synthetic immutable review");
  await user.click(within(region).getByRole("button", { name: "Approve leave" }));
  await within(region).findByRole("button", { name: "Retry same decision" });
  expect(note).toBeDisabled();
  expect(screen.getByRole("button", { name: "Refresh queue" })).toBeDisabled();
  expect(within(region).getByRole("button", { name: "Close request" })).toBeDisabled();
  await user.click(within(region).getByRole("button", { name: "Retry same decision" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]).toEqual(writes[0]);
});

it("continues discovery after an empty filtered page", async () => {
  mock({ emptyFirst: true });
  const { user } = renderWithProviders(<ReviewLeave />);
  await user.click(await screen.findByRole("button", { name: "Load more hospitals" }));
  expect(
    await screen.findByRole("button", { name: /Review leave for Synthetic Nurse/ }),
  ).toBeInTheDocument();
});

it("reports unavailable authority without showing a false empty queue", async () => {
  mock({ unavailable: true });
  renderWithProviders(<ReviewLeave />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not load your review access.");
  expect(screen.queryByText("No pending requests need your review.")).not.toBeInTheDocument();
});

it.each([401, 403, 404, 409])(
  "hides private details and requires refresh after a %s refusal",
  async (status) => {
    const { writes } = mock({ rejected: status });
    const { user } = renderWithProviders(<ReviewLeave />);
    await user.click(
      await screen.findByRole("button", { name: /Review leave for Synthetic Nurse/ }),
    );
    const region = await screen.findByRole("region", { name: "Review leave request" });
    await within(region).findByText(/Synthetic private reason/);
    await user.click(within(region).getByRole("button", { name: "Approve leave" }));
    expect(await within(region).findByRole("alert")).toHaveTextContent(/changed/);
    expect(within(region).queryByText(/Synthetic private reason/)).not.toBeInTheDocument();
    expect(
      within(region).queryByRole("button", { name: "Retry same decision" }),
    ).not.toBeInTheDocument();
    expect(within(region).getByRole("button", { name: "Close request" })).toBeEnabled();
    expect(writes).toHaveLength(1);
  },
);

it("removes the request and actions while the workspace is changing", async () => {
  const { writes } = mock();
  const { user } = renderWithProviders(<ReviewLeave />);
  await user.click(await screen.findByRole("button", { name: /Review leave for Synthetic Nurse/ }));
  await screen.findByText(/Synthetic private reason/);
  act(() => beginTenantTransition());
  expect(screen.getByRole("status")).toHaveTextContent("Checking your workspace");
  expect(screen.queryByText(/Synthetic private reason/)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Approve leave" })).not.toBeInTheDocument();
  expect(writes).toHaveLength(0);
});
