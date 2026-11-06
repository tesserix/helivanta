import { chooseOption } from "@/test/select";
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
const cardiology = "0190a000-0000-7000-8000-00000000000a";
const wards = "0190a000-0000-7000-8000-00000000000b";
const departmentNames: Record<string, string> = {
  [cardiology]: "Synthetic Cardiology",
  [wards]: "Synthetic Wards",
};
const departmentRow = (department: string) => ({
  id: department,
  hospital_id: hospital,
  revision_id: "0190a000-0000-7000-8000-0000000000f1",
  version: 1,
  code: "SYN",
  name: departmentNames[department],
  kind: "department",
  parent_id: null,
  status: "active",
  effective_at: "2026-10-01T00:00:00Z",
  created_at: "2026-10-01T00:00:00Z",
});
const page = (data: unknown[], cursor: string | null = null) => ({
  data,
  page: { has_more: cursor !== null, next_cursor: cursor },
});
afterEach(() => {
  act(() => cancelTenantTransition());
  vi.unstubAllGlobals();
});

function mock({
  uncertain = false,
  emptyFirst = false,
  unavailable = false,
  /** Answers the review-scope read with 403 while it returns true. */
  refuseScopes = (): boolean => false,
  /** Answers the queue read with 403 while it returns true. */
  refuseQueue = (): boolean => false,
  rejected = 0,
  rows = [row] as Record<string, unknown>[],
  departments = [] as string[],
  queueRefusal = 0,
  queueCode = "not_found",
  detailRefusal = 0,
  namesUnavailable = false,
} = {}) {
  const writes: { body: unknown; tenant: string | null }[] = [];
  const posts: string[] = [];
  const reads: string[] = [];
  const refusal = (status: number, error: string) =>
    Response.json({ error, message: "Synthetic refusal." }, { status });
  let done = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      reads.push(url);
      if (url.includes("/iam/me/permissions"))
        return Response.json({ tenant_id: tenant, subject: "synthetic-reviewer", data: [] });
      if (init?.method === "POST") {
        posts.push(url);
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
        if (refuseScopes())
          return Response.json({ error: "forbidden", message: "secret detail" }, { status: 403 });
        if (unavailable)
          return Response.json(
            { error: "scoped_authorization_unavailable", message: "Access unavailable." },
            { status: 503 },
          );
        return Response.json(
          emptyFirst && !url.includes("cursor=")
            ? page([], "next")
            : page([
                {
                  hospital_id: hospital,
                  name: "Synthetic Hospital",
                  scope_kind: departments.length > 0 ? "department" : "hospital",
                  department_ids: departments,
                },
              ]),
        );
      }
      if (url.endsWith("/leave-review-requests") && refuseQueue())
        return Response.json({ error: "forbidden", message: "secret detail" }, { status: 403 });
      if (url.includes("/posting-department-choices")) {
        if (namesUnavailable) return refusal(503, "upstream_unavailable");
        const department = new URL(url, "http://synthetic").searchParams.get("department")!;
        return Response.json(page([departmentRow(department)]));
      }
      if (/\/department-leave-review-requests\?department=[^&]+$/.test(url)) {
        if (queueRefusal) return refusal(queueRefusal, queueCode);
        return Response.json(page(done ? [] : rows));
      }
      if (url.includes(`/department-leave-review-requests/${id}?department=`)) {
        if (detailRefusal) return refusal(detailRefusal, "not_found");
        return Response.json({
          request: { ...row, reason: "Synthetic private reason" },
          days: [],
          roster_clashes: [],
        });
      }
      if (url.endsWith("/leave-review-requests")) return Response.json(page(done ? [] : rows));
      if (url.endsWith(`/leave-review-requests/${id}`))
        return Response.json({
          request: { ...row, reason: "Synthetic private reason" },
          days: [{ day: row.start_on, half_days: 2, holiday: false, calendar_revision_id: id }],
          roster_clashes: [],
        });
      throw new Error(`Unexpected request ${url}`);
    }),
  );
  return { writes, posts, reads };
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

it("marks routed requests as direct or escalated and leaves unrouted ones unmarked", async () => {
  const escalated = "55555555-5555-4555-8555-555555555555";
  const unrouted = "66666666-6666-4666-8666-666666666666";
  mock({
    rows: [
      { ...row, route_level: 1 },
      { ...row, id: escalated, display_name: "Synthetic Porter", route_level: 3 },
      { ...row, id: unrouted, display_name: "Synthetic Clerk" },
    ],
  });
  renderWithProviders(<ReviewLeave />);
  const item = (name: string) =>
    screen.getByRole("button", { name: `Review leave for ${name}` }).closest("li")!;
  await screen.findByRole("button", { name: "Review leave for Synthetic Clerk" });
  expect(within(item("Synthetic Nurse")).getByText("Direct report")).toBeInTheDocument();
  expect(within(item("Synthetic Porter")).getByText("Escalated · level 3")).toBeInTheDocument();
  expect(within(item("Synthetic Clerk")).queryByText(/Direct report|Escalated/)).toBeNull();
});

it("withdraws the review queue once review access is refused", async () => {
  let refused = false;
  mock({ refuseScopes: () => refused });
  renderWithProviders(<ReviewLeave />);
  expect(
    await screen.findByRole("button", { name: /Review leave for Synthetic Nurse/ }),
  ).toBeVisible();
  refused = true;
  window.dispatchEvent(new Event("visibilitychange"));
  expect(
    await screen.findByText("This wasn't found, or you don't have access to it."),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /Review leave for Synthetic Nurse/ })).toBeNull();
  expect(screen.queryByRole("button", { name: /refresh access|try again/i })).toBeNull();
});

it("withdraws the loaded queue once a refreshed read is refused, with no way to ask again", async () => {
  let refused = false;
  mock({ refuseQueue: () => refused });
  const { user } = renderWithProviders(<ReviewLeave />);
  expect(
    await screen.findByRole("button", { name: /Review leave for Synthetic Nurse/ }),
  ).toBeVisible();
  refused = true;
  await user.click(screen.getByRole("button", { name: "Refresh queue" }));
  expect(
    await screen.findByText("This wasn't found, or you don't have access to it."),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /Review leave for Synthetic Nurse/ })).toBeNull();
  expect(screen.queryByRole("button", { name: /refresh queue|try again/i })).toBeNull();
});

it("reviews leave in a held department through the department routes only", async () => {
  const { writes, posts, reads } = mock({ departments: [cardiology, wards] });
  const { user } = renderWithProviders(<ReviewLeave />);
  const chooser = await screen.findByRole("combobox", { name: "Department" });
  await waitFor(() => expect(chooser).toHaveTextContent("Synthetic Cardiology"));
  expect(
    screen.getByText(/posted only within Synthetic Cardiology and its current sub-units/),
  ).toBeInTheDocument();
  await chooseOption(user, chooser, "Synthetic Wards");
  await waitFor(() =>
    expect(reads).toContain(
      `/api/v1/workforce/hospitals/${hospital}/department-leave-review-requests?department=${wards}`,
    ),
  );
  await user.click(await screen.findByRole("button", { name: /Review leave for Synthetic Nurse/ }));
  const region = await screen.findByRole("region", { name: "Review leave request" });
  expect(await within(region).findByText(/Synthetic private reason/)).toBeInTheDocument();
  await user.type(within(region).getByLabelText("Decision note"), "Synthetic ward cover");
  await user.click(within(region).getByRole("button", { name: "Approve leave" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(posts).toEqual([
    `/api/v1/workforce/hospitals/${hospital}/staff/${staff}/leave-requests/${id}/department-decision?department=${wards}`,
  ]);
  expect(writes[0]).toEqual({
    tenant,
    body: { expected_version: 3, decision: "approve", note: "Synthetic ward cover" },
  });
  expect(reads.some((url) => /\/leave-review-requests(\/|$)/.test(url))).toBe(false);
  expect(await screen.findByText("No pending requests need your review.")).toBeInTheDocument();
});

it("keeps hospital-wide reviewers off department and organization routes", async () => {
  const { reads } = mock();
  renderWithProviders(<ReviewLeave />);
  await screen.findByRole("button", { name: /Review leave for Synthetic Nurse/ });
  expect(screen.queryByRole("combobox", { name: "Department" })).not.toBeInTheDocument();
  expect(reads.some((url) => url.includes("department"))).toBe(false);
});

it("shows a retired department instead of an empty queue", async () => {
  mock({ departments: [cardiology], queueRefusal: 404 });
  renderWithProviders(<ReviewLeave />);
  const alert = await screen.findByRole("alert");
  expect(alert).toHaveTextContent("Department retired. This access no longer applies.");
  expect(within(alert).queryByRole("button")).toBeNull();
  expect(screen.queryByRole("button", { name: "Refresh queue" })).toBeNull();
  expect(screen.getByRole("button", { name: "Refresh access" })).toBeEnabled();
  expect(screen.queryByText("No pending requests need your review.")).not.toBeInTheDocument();
});

it("asks for a reload when the department structure moved", async () => {
  const { reads } = mock({
    departments: [cardiology],
    queueRefusal: 409,
    queueCode: "department_scope_changed",
  });
  const { user } = renderWithProviders(<ReviewLeave />);
  const alert = await screen.findByRole("alert");
  expect(alert).toHaveTextContent(
    "The department structure changed. Reload the department leave queue.",
  );
  const queueReads = () =>
    reads.filter((url) => url.includes("/department-leave-review-requests?")).length;
  const before = queueReads();
  await user.click(within(alert).getByRole("button", { name: "Try again" }));
  await waitFor(() => expect(queueReads()).toBeGreaterThan(before));
});

it("explains a department request that left the reviewer's authority", async () => {
  mock({ departments: [cardiology], detailRefusal: 404 });
  const { user } = renderWithProviders(<ReviewLeave />);
  await user.click(await screen.findByRole("button", { name: /Review leave for Synthetic Nurse/ }));
  const region = await screen.findByRole("region", { name: "Review leave request" });
  expect(await within(region).findByRole("alert")).toHaveTextContent(
    "This request is no longer in your department's review. Close it and refresh the queue.",
  );
  expect(within(region).queryByRole("button", { name: "Approve leave" })).not.toBeInTheDocument();
});

it("keeps the department reference when its name cannot be loaded", async () => {
  mock({ departments: [cardiology], namesUnavailable: true });
  renderWithProviders(<ReviewLeave />);
  const chooser = await screen.findByRole("combobox", { name: "Department" });
  await waitFor(() => expect(chooser).toHaveTextContent(`Department reference ${cardiology}`));
  expect(
    await screen.findByRole("button", { name: /Review leave for Synthetic Nurse/ }),
  ).toBeInTheDocument();
});
