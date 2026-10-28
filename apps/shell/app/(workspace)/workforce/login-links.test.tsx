import { afterEach, expect, it, vi } from "vitest";
import { useState } from "react";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import type { Hospital, Staff } from "@helivanta/api";
import { StaffLoginLinkPanel } from "./login-link-panel";

const tenant = "11111111-1111-4111-8111-111111111111";
const hospital: Hospital = {
  id: "22222222-2222-4222-8222-222222222222",
  name: "Synthetic Hospital",
  access_level: "manager",
};
const staff: Staff = {
  id: "33333333-3333-4333-8333-333333333333",
  hospital_id: hospital.id,
  display_name: "Avery Synthetic",
  employee_code: "SYN-01",
  job_category: "nurse",
  employment_status: "active",
  created_at: "2026-10-26T00:00:00Z",
  updated_at: "2026-10-26T00:00:00Z",
};
const id = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const link = {
  id,
  staff_id: staff.id,
  subject: "candidate",
  status: "pending",
  version: 1,
  proposed_by_subject: "proposer",
  proposal_reason: "Verified synthetic account",
  actor_subject: "proposer",
  reason: "Verified synthetic account",
  created_at: staff.created_at,
  updated_at: staff.updated_at,
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
const perms = ["workforce.login_links.manage", "workforce.login_links.review"];
afterEach(() => vi.unstubAllGlobals());

it("keeps the proposal ID and body after an ambiguous failure", async () => {
  const writes: { url: string; body: unknown; tenant: string | null }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("permissions"))
        return Response.json({ tenant_id: tenant, subject: "proposer", data: perms });
      if (init?.method === "PUT") {
        writes.push({
          url,
          body: JSON.parse(String(init.body)),
          tenant: new Headers(init.headers).get("X-Helivanta-Expected-Tenant"),
        });
        return Response.json(
          { error: "upstream_unavailable", message: "Unavailable" },
          { status: 502 },
        );
      }
      return Response.json(page([]));
    }),
  );
  const { user } = renderWithProviders(
    <StaffLoginLinkPanel hospital={hospital} staff={staff} onClose={() => {}} />,
  );
  await user.click(await screen.findByRole("button", { name: "Propose account link" }));
  await user.type(screen.getByLabelText("Account ID"), "candidate");
  await user.type(screen.getByLabelText("Reason"), "Verified synthetic account");
  await user.click(screen.getByRole("button", { name: "Submit proposal" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("may have been saved");
  await user.click(screen.getByRole("button", { name: "Retry same request" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(writes[0]?.tenant).toBe(tenant);
  expect(screen.getByLabelText("Account ID")).toBeDisabled();
});

it.each(["proposer", "candidate"])("hides independent review from %s", async (subject) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      Response.json(
        url.includes("permissions") ? { tenant_id: tenant, subject, data: perms } : page([link]),
      ),
    ),
  );
  renderWithProviders(<StaffLoginLinkPanel hospital={hospital} staff={staff} onClose={() => {}} />);
  expect(await screen.findByText("candidate")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Reject" })).not.toBeInTheDocument();
});

it("requires a reason and submits independent review with the viewed version", async () => {
  let decision: unknown;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("permissions"))
        return Response.json({ tenant_id: tenant, subject: "reviewer", data: perms });
      if (init?.method === "POST") {
        decision = JSON.parse(String(init.body));
        return Response.json({ ...link, status: "active", version: 2 });
      }
      return Response.json(page([link]));
    }),
  );
  const { user } = renderWithProviders(
    <StaffLoginLinkPanel hospital={hospital} staff={staff} onClose={() => {}} />,
  );
  await user.click(await screen.findByRole("button", { name: "Approve" }));
  await user.click(screen.getByRole("button", { name: "Confirm approve" }));
  expect(decision).toBeUndefined();
  await user.type(screen.getByLabelText("Reason"), "Independently verified");
  await user.click(screen.getByRole("button", { name: "Confirm approve" }));
  await waitFor(() =>
    expect(decision).toEqual({
      expected_version: 1,
      decision: "approve",
      reason: "Independently verified",
    }),
  );
});

it("clears a draft when the staff scope changes", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      Response.json(
        url.includes("permissions")
          ? { tenant_id: tenant, subject: "proposer", data: perms }
          : page([]),
      ),
    ),
  );
  function SwitchStaff() {
    const [current, setCurrent] = useState(staff);
    return (
      <>
        <button
          onClick={() => setCurrent({ ...staff, id: "44444444-4444-4444-8444-444444444444" })}
        >
          Change staff
        </button>
        <StaffLoginLinkPanel hospital={hospital} staff={current} onClose={() => {}} />
      </>
    );
  }
  const { user } = renderWithProviders(<SwitchStaff />);
  await user.click(await screen.findByRole("button", { name: "Propose account link" }));
  await user.type(screen.getByLabelText("Account ID"), "private-draft");
  await user.click(screen.getByRole("button", { name: "Change staff" }));
  expect(screen.queryByDisplayValue("private-draft")).not.toBeInTheDocument();
});

it("shows an API failure instead of a false empty account state", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url.includes("permissions")
        ? Response.json({ tenant_id: tenant, subject: "reviewer", data: perms })
        : Response.json({ error: "authz_unavailable", message: "Unavailable" }, { status: 503 }),
    ),
  );
  renderWithProviders(<StaffLoginLinkPanel hospital={hospital} staff={staff} onClose={() => {}} />);
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not load account links");
  expect(screen.queryByText("No account has been linked.")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Propose account link" })).not.toBeInTheDocument();
});

it("loads private history and follows its cursor", async () => {
  const calls: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      calls.push(url);
      if (url.includes("permissions"))
        return Response.json({ tenant_id: tenant, subject: "reviewer", data: perms });
      if (url.includes("/history")) {
        const second = url.includes("cursor=");
        return Response.json({
          data: [
            {
              id,
              version: second ? 1 : 2,
              status: second ? "pending" : "active",
              actor_subject: second ? "proposer" : "reviewer",
              reason: second ? "Original verification" : "Independent check",
              created_at: staff.created_at,
            },
          ],
          page: { has_more: !second, next_cursor: second ? null : "opaque+/=" },
        });
      }
      return Response.json(page([link]));
    }),
  );
  const { user } = renderWithProviders(
    <StaffLoginLinkPanel hospital={hospital} staff={staff} onClose={() => {}} />,
  );
  await user.click(await screen.findByRole("button", { name: "History" }));
  expect(await screen.findByText("reviewer: Independent check")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Load more" }));
  expect(await screen.findByText("proposer: Original verification")).toBeInTheDocument();
  expect(calls.some((url) => url.endsWith("/history?limit=50&cursor=opaque%2B%2F%3D"))).toBe(true);
});
