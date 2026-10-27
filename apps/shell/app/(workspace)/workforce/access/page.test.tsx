import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import AccessPage from "./page";

const tenant = "11111111-1111-4111-8111-111111111111";
const hospital = "22222222-2222-4222-8222-222222222222";
const id = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa";
const timestamp = "2026-10-27T00:00:00Z";
const row = {
  id,
  input: {
    hospital_id: hospital,
    subject: "candidate",
    template_key: "workforce.leave_reviewer.v1",
    reason: "Synthetic review",
    valid_from: timestamp,
    expires_at: "2027-01-01T00:00:00Z",
  },
  proposed_by: "proposer",
  status: "pending_review",
  version: 2,
  created_at: timestamp,
  updated_at: timestamp,
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
const permissions = ["iam.assignment.manage", "iam.assignment.review"];
afterEach(() => vi.unstubAllGlobals());

it("lets a membership administrator enroll staff without assignment permissions", async () => {
  setup("membership-admin", [], ["iam.member.manage"]);
  const prior = fetch;
  const commands: Array<{ url: string; init?: RequestInit }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/iam/members") && init?.method === "POST") {
        commands.push({ url, init });
        return Response.json({ id });
      }
      return prior(url, init);
    }),
  );
  const { user } = renderWithProviders(<AccessPage />);
  await user.type(await screen.findByLabelText("Existing account ID"), "synthetic-reviewer");
  await user.click(screen.getByRole("button", { name: "Enroll staff member" }));
  await waitFor(() => expect(commands).toHaveLength(1));
  expect(JSON.parse(String(commands[0]?.init?.body))).toEqual({
    subject: "synthetic-reviewer",
    role_key: "staff_member",
  });
  expect(await screen.findByText(/Membership recorded for synthetic-reviewer/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Propose assignment" })).not.toBeInTheDocument();
});

it("does not offer membership enrollment to assignment reviewers", async () => {
  setup();
  renderWithProviders(<AccessPage />);
  await screen.findByText("Synthetic review");
  expect(screen.queryByLabelText("Existing account ID")).not.toBeInTheDocument();
});

it("retains the exact tenant and subject when enrollment has an uncertain outcome", async () => {
  setup("membership-admin", [], ["iam.member.manage"]);
  const prior = fetch;
  const commands: Array<{ body: string; tenant: string | null }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/iam/members") && init?.method === "POST") {
        commands.push({
          body: String(init.body),
          tenant: new Headers(init.headers).get("X-Helivanta-Expected-Tenant"),
        });
        if (commands.length === 1)
          return Response.json({ error: "unavailable", message: "Unavailable" }, { status: 502 });
        return Response.json({ id });
      }
      return prior(url, init);
    }),
  );
  const { user } = renderWithProviders(<AccessPage />);
  await user.type(await screen.findByLabelText("Existing account ID"), "synthetic-reviewer");
  await user.click(screen.getByRole("button", { name: "Enroll staff member" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("may have been recorded");
  expect(screen.getByLabelText("Existing account ID")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry same enrollment" }));
  await waitFor(() => expect(commands).toHaveLength(2));
  expect(commands[0]).toEqual(commands[1]);
  expect(commands[0]?.tenant).toBe(tenant);
  expect(await screen.findByText(/Membership recorded for synthetic-reviewer/)).toBeInTheDocument();
});

it("validates enrollment before submitting and stops after a workspace conflict", async () => {
  setup("membership-admin", [], ["iam.member.manage"]);
  const prior = fetch;
  const commands: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/iam/members") && init?.method === "POST") {
        commands.push(init.body);
        return Response.json(
          { error: "session_changed", message: "Your workspace changed." },
          { status: 409 },
        );
      }
      return prior(url, init);
    }),
  );
  const { user } = renderWithProviders(<AccessPage />);
  await user.click(await screen.findByRole("button", { name: "Enroll staff member" }));
  expect(screen.getByLabelText("Existing account ID")).toHaveAttribute("aria-invalid", "true");
  expect(commands).toHaveLength(0);
  await user.type(screen.getByLabelText("Existing account ID"), "synthetic-reviewer");
  await user.click(screen.getByRole("button", { name: "Enroll staff member" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("workspace changed");
  expect(screen.getByRole("button", { name: "Retry same enrollment" })).toBeDisabled();
  expect(commands).toHaveLength(1);
});
function setup(subject = "reviewer", rows = [row], perms = permissions) {
  const writes: Array<{ url: string; body: unknown; tenant: string | null }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/iam/me/permissions"))
        return Response.json({ tenant_id: tenant, subject, data: perms });
      if (url.includes("/workforce/hospitals"))
        return Response.json(
          page([{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }]),
        );
      if (init?.method === "POST") {
        writes.push({
          url,
          body: JSON.parse(String(init.body)),
          tenant: new Headers(init.headers).get("X-Helivanta-Expected-Tenant"),
        });
        return Response.json({ ...row, status: "pending_decision_scope", version: 3 });
      }
      return Response.json(page(rows));
    }),
  );
  return writes;
}

it.each(["proposer", "candidate"])("does not offer independent review to %s", async (subject) => {
  setup(subject);
  renderWithProviders(<AccessPage />);
  expect(await screen.findByText("Synthetic review")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Reject" })).not.toBeInTheDocument();
});

it("requires a reason and submits an independent decision with the displayed version", async () => {
  const writes = setup();
  const { user } = renderWithProviders(<AccessPage />);
  await user.click(await screen.findByRole("button", { name: "Approve" }));
  await user.click(screen.getByRole("button", { name: "Confirm approve" }));
  expect(writes).toHaveLength(0);
  await user.type(screen.getByLabelText("Decision reason"), "Independent synthetic review");
  await user.click(screen.getByRole("button", { name: "Confirm approve" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({
    tenant,
    body: { expected_version: 2, decision: "approve", reason: "Independent synthetic review" },
  });
  expect(await screen.findByRole("status")).toHaveTextContent("recorded");
});

it("does not call private assignment APIs without administrative permission", async () => {
  setup("member", [], []);
  renderWithProviders(<AccessPage />);
  expect(
    await screen.findByText("Assignment administration is not available for this account."),
  ).toBeInTheDocument();
  expect(vi.mocked(fetch).mock.calls.every(([url]) => String(url).includes("permissions"))).toBe(
    true,
  );
});

it("describes projection progress without presenting pending approval as active access", async () => {
  setup("reviewer", [{ ...row, status: "approved_pending_activation" }]);
  renderWithProviders(<AccessPage />);
  expect(await screen.findByText(/Approved; activation pending/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
});

it("offers withdrawal only to the proposer with manage permission", async () => {
  setup("proposer");
  renderWithProviders(<AccessPage />);
  expect(await screen.findByRole("button", { name: "Withdraw" })).toBeInTheDocument();
});

it("retries an uncertain decision with the same immutable command", async () => {
  setup();
  const read = fetch;
  const attempts: Array<{ body: string; tenant: string | null }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method !== "POST") return read(url, init);
      attempts.push({
        body: String(init.body),
        tenant: new Headers(init.headers).get("X-Helivanta-Expected-Tenant"),
      });
      if (attempts.length === 1)
        return Response.json({ error: "unavailable", message: "Unavailable" }, { status: 502 });
      return Response.json({ ...row, status: "pending_decision_scope", version: 3 });
    }),
  );
  const { user } = renderWithProviders(<AccessPage />);
  await user.click(await screen.findByRole("button", { name: "Approve" }));
  await user.type(screen.getByLabelText("Decision reason"), "Independent review");
  await user.click(screen.getByRole("button", { name: "Confirm approve" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("may have been recorded");
  expect(screen.getByLabelText("Decision reason")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry same request" }));
  await waitFor(() => expect(attempts).toHaveLength(2));
  expect(attempts[0]).toEqual(attempts[1]);
  expect(attempts[0]?.tenant).toBe(tenant);
});

it("does not offer decisions on expired assignments", async () => {
  setup("reviewer", [{ ...row, input: { ...row.input, expires_at: "2000-01-01T00:00:00Z" } }]);
  renderWithProviders(<AccessPage />);
  expect(await screen.findByText(/Expired/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
});

it("marks invalid proposal fields for assistive technology", async () => {
  setup();
  const { user } = renderWithProviders(<AccessPage />);
  await user.click(await screen.findByRole("button", { name: "Propose assignment" }));
  await user.click(await screen.findByRole("button", { name: "Submit proposal" }));
  await waitFor(() =>
    expect(screen.getByLabelText("Account ID", { exact: true })).toHaveAttribute(
      "aria-invalid",
      "true",
    ),
  );
  expect(screen.getByLabelText("Reason", { exact: true })).toHaveAttribute("aria-invalid", "true");
});
