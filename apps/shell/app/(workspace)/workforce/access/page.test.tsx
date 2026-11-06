import { afterEach, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { staffAccessTemplates, staffAccessTemplateCatalogs } from "@helivanta/api";
import { NavigationGuardProvider } from "@helivanta/ui";
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
  scope_kind: "hospital",
  proposed_by: "proposer",
  status: "pending_review",
  version: 2,
  created_at: timestamp,
  updated_at: timestamp,
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
const catalogDefinitions = (catalog: keyof typeof staffAccessTemplateCatalogs) =>
  staffAccessTemplates.filter((template) =>
    staffAccessTemplateCatalogs[catalog].some((key) => key === template.key),
  );
const permissions = ["iam.assignment.manage", "iam.assignment.review"];
afterEach(() => vi.unstubAllGlobals());

it("lets a membership administrator enroll staff without assignment permissions", async () => {
  setup("membership-admin", [], ["iam.member.manage"]);
  const prior = fetch;
  const commands: Array<{ url: string; init?: RequestInit }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/iam/membership-enrollments/") && init?.method === "PUT") {
        commands.push({ url, init });
        return Response.json({
          id: url.split("/").at(-1),
          ...JSON.parse(String(init?.body)),
          membership_id: id,
          role_key: "staff_member",
          membership_epoch: 1,
          actor_subject: new Headers(init?.headers).get("X-Helivanta-Expected-Subject"),
          created_at: "2026-10-30T00:00:00Z",
        });
      }
      return prior(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.type(await screen.findByLabelText("Existing account ID"), "synthetic-reviewer");
  await user.type(screen.getByLabelText("Enrollment reason"), "Verified first enrollment");
  await user.click(screen.getByLabelText("I understand this creates ordinary membership only."));
  await user.click(screen.getByRole("button", { name: "Enroll staff member" }));
  await waitFor(() => expect(commands).toHaveLength(1));
  expect(JSON.parse(String(commands[0]?.init?.body))).toEqual({
    subject: "synthetic-reviewer",
    reason: "Verified first enrollment",
  });
  expect(await screen.findByText(/Membership recorded for synthetic-reviewer/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Propose assignment" })).not.toBeInTheDocument();
});

it("does not offer membership enrollment to assignment reviewers", async () => {
  setup();
  renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
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
      if (url.includes("/iam/membership-enrollments/") && init?.method === "PUT") {
        commands.push({
          body: String(init.body),
          tenant: new Headers(init.headers).get("X-Helivanta-Expected-Tenant"),
        });
        if (commands.length === 1)
          return Response.json({ error: "unavailable", message: "Unavailable" }, { status: 502 });
        return Response.json({
          id: url.split("/").at(-1),
          ...JSON.parse(String(init?.body)),
          membership_id: id,
          role_key: "staff_member",
          membership_epoch: 1,
          actor_subject: new Headers(init?.headers).get("X-Helivanta-Expected-Subject"),
          created_at: "2026-10-30T00:00:00Z",
        });
      }
      return prior(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.type(await screen.findByLabelText("Existing account ID"), "synthetic-reviewer");
  await user.type(screen.getByLabelText("Enrollment reason"), "Verified first enrollment");
  await user.click(screen.getByLabelText("I understand this creates ordinary membership only."));
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
      if (url.includes("/iam/membership-enrollments/") && init?.method === "PUT") {
        commands.push(init.body);
        return Response.json(
          { error: "session_changed", message: "Your workspace changed." },
          { status: 409 },
        );
      }
      return prior(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Enroll staff member" }));
  expect(screen.getByLabelText("Existing account ID")).toHaveAttribute("aria-invalid", "true");
  expect(commands).toHaveLength(0);
  await user.type(screen.getByLabelText("Existing account ID"), "synthetic-reviewer");
  await user.type(screen.getByLabelText("Enrollment reason"), "Verified first enrollment");
  await user.click(screen.getByLabelText("I understand this creates ordinary membership only."));
  await user.click(screen.getByRole("button", { name: "Enroll staff member" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("workspace changed");
  expect(screen.getByRole("button", { name: "Retry same enrollment" })).toBeDisabled();
  expect(commands).toHaveLength(1);
});
function setup(subject = "reviewer", rows: unknown[] = [row], perms = permissions) {
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
  renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByText("Synthetic review")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Reject" })).not.toBeInTheDocument();
});

it("requires a reason and submits an independent decision with the displayed version", async () => {
  const writes = setup();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
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
  renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  expect(
    await screen.findByText("Assignment administration is not available for this account."),
  ).toBeInTheDocument();
  expect(vi.mocked(fetch).mock.calls.every(([url]) => String(url).includes("permissions"))).toBe(
    true,
  );
});

it("describes projection progress without presenting pending approval as active access", async () => {
  setup("reviewer", [{ ...row, status: "approved_pending_activation" }]);
  renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByText(/Approved; activation pending/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
});

it("offers withdrawal only to the proposer with manage permission", async () => {
  setup("proposer");
  renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
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
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
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
  renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByText(/Expired/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Approve" })).not.toBeInTheDocument();
});

it("marks invalid proposal fields for assistive technology", async () => {
  setup();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
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

it("offers canonical identity templates separately from leave assignments", async () => {
  setup();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByLabelText("Assignment area"));
  await user.click(screen.getByRole("option", { name: "Staff identity checks" }));
  await user.click(screen.getByRole("button", { name: "Propose assignment" }));
  await user.click(screen.getByLabelText("Access template"));
  const templates = screen.getByRole("listbox");
  expect(templates).toHaveTextContent("Staff identity checks manager");
  expect(templates).toHaveTextContent("Staff identity checks reviewer");
  expect(templates).not.toHaveTextContent("Leave reviewer");
  expect(
    vi
      .mocked(fetch)
      .mock.calls.some(([url]) => String(url).includes("/iam/identity-assignment-proposals?")),
  ).toBe(true);
});

it("identifies HR assignment records using the canonical template name", async () => {
  setup("reviewer", [
    { ...row, input: { ...row.input, template_key: "workforce.identity_manager.v1" } },
  ]);
  renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  expect(
    await screen.findByRole("heading", { name: "Staff identity checks manager · candidate" }),
  ).toBeInTheDocument();
});

it("prepares renewal as a new independently reviewed proposal without changing the old assignment", async () => {
  setup("proposer", [{ ...row, status: "active" }]);
  const read = fetch;
  const proposals: Array<{ url: string; body: typeof row.input; tenant: string | null }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method !== "PUT") return read(url, init);
      const body = JSON.parse(String(init.body));
      proposals.push({
        url,
        body,
        tenant: new Headers(init.headers).get("X-Helivanta-Expected-Tenant"),
      });
      return Response.json({
        ...row,
        id: url.split("/").at(-1),
        input: body,
        status: "pending_scope",
        version: 1,
      });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Renew access" }));
  const form = within(screen.getByRole("form", { name: "Renew assignment" }));
  expect(form.getByText(/new proposal.*independent review/i)).toBeInTheDocument();
  expect(form.getByLabelText("Account ID")).toHaveValue("candidate");
  expect(form.getByLabelText("Account ID")).toHaveAttribute("readonly");
  expect(form.getByLabelText("Starts at")).toHaveValue("");
  expect(form.getByLabelText("Expires at")).toHaveValue("");
  await user.click(form.getByRole("button", { name: "Propose renewal" }));
  expect(proposals).toHaveLength(0);
  expect(form.getByLabelText("Starts at")).toHaveAttribute("aria-invalid", "true");
  expect(form.getByLabelText("Expires at")).toHaveAttribute("aria-invalid", "true");
  expect(form.getByLabelText("Reason")).toHaveAttribute("aria-invalid", "true");
  const start = new Date(Date.now() + 2 * 86400000);
  const end = new Date(Date.now() + 3 * 86400000);
  const local = (date: Date) =>
    new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  fireEvent.change(form.getByLabelText("Starts at"), { target: { value: local(start) } });
  fireEvent.change(form.getByLabelText("Expires at"), { target: { value: local(end) } });
  await user.type(form.getByLabelText("Reason"), "Synthetic continued review duty");
  await user.click(form.getByRole("button", { name: "Propose renewal" }));
  await waitFor(() => expect(proposals).toHaveLength(1));
  expect(proposals[0]?.url).not.toContain(id);
  expect(proposals[0]?.tenant).toBe(tenant);
  expect(proposals[0]?.body).toMatchObject({
    hospital_id: hospital,
    subject: "candidate",
    template_key: row.input.template_key,
    reason: `Renewal of ${id} v2: Synthetic continued review duty`,
  });
  expect(
    Date.parse(proposals[0]!.body.expires_at) - Date.parse(proposals[0]!.body.valid_from),
  ).toBe(86400000);
  expect(await screen.findByText(/Proposal recorded/)).toBeInTheDocument();
});

it("allows an expired activated assignment to seed a fresh proposal", async () => {
  setup("proposer", [
    { ...row, status: "active", input: { ...row.input, expires_at: "2000-01-01T00:00:00Z" } },
  ]);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Renew access" }));
  expect(screen.getByRole("form", { name: "Renew assignment" })).toBeInTheDocument();
  expect(screen.getByLabelText("Reason")).toHaveValue("");
});

it.each([
  "pending_review",
  "withdrawn",
  "rejected",
  "scope_rejected",
  "approved_pending_activation",
])("does not offer renewal for %s assignments", async (status) => {
  setup("proposer", [{ ...row, status }]);
  renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await screen.findByText("Synthetic review");
  expect(screen.queryByRole("button", { name: "Renew access" })).not.toBeInTheDocument();
});

it("does not offer renewal to review-only accounts", async () => {
  setup("reviewer", [{ ...row, status: "active" }], ["iam.assignment.review"]);
  renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await screen.findByText("Synthetic review");
  expect(screen.queryByRole("button", { name: "Renew access" })).not.toBeInTheDocument();
});

it("retains the renewal proposal ID and exact payload after an uncertain response", async () => {
  setup("proposer", [{ ...row, status: "active" }]);
  const read = fetch;
  const attempts: Array<{ url: string; body: string }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method !== "PUT") return read(url, init);
      attempts.push({ url, body: String(init.body) });
      if (attempts.length === 1)
        return Response.json({ error: "unavailable", message: "Unavailable" }, { status: 502 });
      return Response.json({
        ...row,
        id: url.split("/").at(-1),
        input: JSON.parse(String(init.body)),
        status: "pending_scope",
        version: 1,
      });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Renew access" }));
  const local = (days: number) => {
    const date = new Date(Date.now() + days * 86400000);
    return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  };
  fireEvent.change(screen.getByLabelText("Starts at"), { target: { value: local(2) } });
  fireEvent.change(screen.getByLabelText("Expires at"), { target: { value: local(3) } });
  await user.type(screen.getByLabelText("Reason"), "x".repeat(500));
  await user.click(screen.getByRole("button", { name: "Propose renewal" }));
  expect(screen.getByLabelText("Reason")).toHaveAttribute("aria-invalid", "true");
  expect(attempts).toHaveLength(0);
  await user.clear(screen.getByLabelText("Reason"));
  await user.type(screen.getByLabelText("Reason"), "Synthetic renewed review duty");
  await user.click(screen.getByRole("button", { name: "Propose renewal" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("may have been recorded");
  expect(screen.getByLabelText("Reason")).toBeDisabled();
  expect(screen.getByLabelText("Starts at")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry same request" }));
  await waitFor(() => expect(attempts).toHaveLength(2));
  expect(attempts[0]).toEqual(attempts[1]);
  expect(await screen.findByText(/Proposal recorded/)).toBeInTheDocument();
});

it("offers HR intake separately when the explicit server catalog confirms its permissions", async () => {
  setup();
  const prior = fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/iam/staff-access-templates?catalog=hr-intake-v1"))
        return Response.json(catalogDefinitions("hr-intake-v1"));
      return prior(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByLabelText("Assignment area"));
  await user.click(await screen.findByRole("option", { name: "Staff onboarding" }));
  await user.click(screen.getByRole("button", { name: "Propose assignment" }));
  await user.click(screen.getByLabelText("Access template"));
  const templates = screen.getByRole("listbox");
  expect(templates).toHaveTextContent("HR intake administrator");
  expect(templates).not.toHaveTextContent("Leave reviewer");
  expect(templates).not.toHaveTextContent("Staff identity checks reviewer");
  expect(
    vi
      .mocked(fetch)
      .mock.calls.some(([url]) => String(url).includes("/iam/intake-assignment-proposals?")),
  ).toBe(true);
});

it.each(["legacy", "changed-permissions"])(
  "does not offer unverified HR authority from a %s catalog",
  async (kind) => {
    setup();
    const prior = fetch;
    const catalog =
      kind === "legacy"
        ? catalogDefinitions("legacy-v1")
        : catalogDefinitions("hr-intake-v1").map((template) =>
            template.family === "intake"
              ? { ...template, permissions: ["workforce.payroll.approve"] }
              : template,
          );
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.includes("/iam/staff-access-templates?catalog=hr-intake-v1"))
          return Response.json(catalog);
        return prior(url, init);
      }),
    );
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <AccessPage />
      </NavigationGuardProvider>,
    );
    expect(
      await screen.findByText("Staff onboarding assignments are unavailable in this workspace."),
    ).toBeInTheDocument();
    await user.click(screen.getByLabelText("Assignment area"));
    expect(screen.queryByRole("option", { name: "Staff onboarding" })).not.toBeInTheDocument();
    expect(screen.getByRole("option", { name: "Leave review" })).toBeInTheDocument();
  },
);

it.each(["unavailable", "changed"])(
  "blocks retained onboarding decisions when the server catalog becomes %s",
  async (state) => {
    setup("reviewer", []);
    const prior = fetch;
    let catalogReads = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.includes("/iam/staff-access-templates?catalog=hr-intake-v1")) {
          catalogReads++;
          if (catalogReads === 1) return Response.json(catalogDefinitions("hr-intake-v1"));
          return state === "unavailable"
            ? Response.json({ error: "unavailable", message: "Unavailable" }, { status: 503 })
            : Response.json(catalogDefinitions("legacy-v1"));
        }
        if (url.includes("/iam/intake-assignment-proposals?"))
          return Response.json(
            page([
              {
                ...row,
                input: {
                  ...row.input,
                  template_key: "workforce.hr_intake.v1",
                  reason: "Synthetic onboarding assignment",
                },
              },
            ]),
          );
        return prior(url, init);
      }),
    );
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <AccessPage />
      </NavigationGuardProvider>,
    );
    await user.click(await screen.findByLabelText("Assignment area"));
    await user.click(await screen.findByRole("option", { name: "Staff onboarding" }));
    await user.click(await screen.findByRole("button", { name: "Approve" }));
    await user.type(screen.getByLabelText("Decision reason"), "Retained independent review");
    await user.click(screen.getByRole("button", { name: "Refresh assignments" }));
    expect(
      await screen.findByText("Staff onboarding assignments are unavailable in this workspace."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Confirm approve" })).toBeDisabled();
    expect(screen.getByLabelText("Decision reason")).toHaveValue("Retained independent review");
    expect(screen.getByRole("button", { name: "Propose assignment" })).toBeDisabled();
  },
);

it("offers employment review separately only after its exact server catalog is verified", async () => {
  setup();
  const prior = fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/iam/staff-access-templates?catalog=employment-v1"))
        return Response.json(catalogDefinitions("employment-v1"));
      if (url.includes("/iam/employment-assignment-proposals?")) return Response.json(page([]));
      return prior(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByLabelText("Assignment area"));
  await user.click(await screen.findByRole("option", { name: "Employment terms review" }));
  expect(screen.getByText(/Employment reviewers independently examine/)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Propose assignment" }));
  await user.click(screen.getByLabelText("Access template"));
  const templates = screen.getByRole("listbox");
  expect(templates).toHaveTextContent("Staff employment terms reviewer");
  expect(templates).not.toHaveTextContent("HR intake administrator");
  expect(templates).not.toHaveTextContent("Staff identity checks reviewer");
  expect(
    vi
      .mocked(fetch)
      .mock.calls.some(([url]) => String(url).includes("/iam/employment-assignment-proposals?")),
  ).toBe(true);
});
it("offers separately reviewed activation assignments only from their exact catalog", async () => {
  setup();
  const prior = fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/iam/staff-access-templates?catalog=employment-activation-v1"))
        return Response.json(catalogDefinitions("employment-activation-v1"));
      if (url.includes("/iam/employment-activation-assignment-proposals?"))
        return Response.json(page([]));
      return prior(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByLabelText("Assignment area"));
  await user.click(await screen.findByRole("option", { name: "Employment activation" }));
  await user.click(screen.getByRole("button", { name: "Propose assignment" }));
  await user.click(screen.getByLabelText("Access template"));
  const templates = screen.getByRole("listbox");
  expect(templates).toHaveTextContent("Staff employment activator");
  expect(templates).not.toHaveTextContent("Staff employment terms reviewer");
  expect(templates).not.toHaveTextContent("HR intake administrator");
  expect(
    vi
      .mocked(fetch)
      .mock.calls.some(([url]) =>
        String(url).includes("/iam/employment-activation-assignment-proposals?"),
      ),
  ).toBe(true);
});

it.each(["missing", "broader", "extra"])(
  "does not offer activation authority from a %s catalog",
  async (kind) => {
    setup();
    const prior = fetch;
    const definitions = catalogDefinitions("employment-activation-v1");
    const altered =
      kind === "missing"
        ? definitions.filter((template) => template.family !== "activation")
        : kind === "extra"
          ? [...definitions, definitions[0]]
          : definitions.map((template) =>
              template.family === "activation"
                ? {
                    ...template,
                    permissions: ["workforce.employment.activate", "workforce.payroll.approve"],
                  }
                : template,
            );
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.includes("/iam/staff-access-templates?catalog=employment-activation-v1"))
          return Response.json(altered);
        return prior(url, init);
      }),
    );
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <AccessPage />
      </NavigationGuardProvider>,
    );
    await screen.findByText("Employment activation assignments are unavailable in this workspace.");
    await user.click(await screen.findByLabelText("Assignment area"));
    expect(screen.queryByRole("option", { name: "Employment activation" })).not.toBeInTheDocument();
  },
);

it.each(["missing", "broader", "extra"])(
  "does not offer employment authority from a %s catalog",
  async (state) => {
    setup();
    const prior = fetch;
    const catalog =
      state === "missing"
        ? catalogDefinitions("employment-v1").filter((template) => template.family !== "employment")
        : state === "extra"
          ? [...catalogDefinitions("employment-v1"), staffAccessTemplates[0]]
          : catalogDefinitions("employment-v1").map((template) =>
              template.family === "employment"
                ? {
                    ...template,
                    permissions: ["workforce.employment.review", "workforce.payroll.approve"],
                  }
                : template,
            );
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.includes("/iam/staff-access-templates?catalog=employment-v1"))
          return Response.json(catalog);
        return prior(url, init);
      }),
    );
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <AccessPage />
      </NavigationGuardProvider>,
    );
    await screen.findByText("Employment review assignments are unavailable in this workspace.");
    await user.click(screen.getByLabelText("Assignment area"));
    expect(
      screen.queryByRole("option", { name: "Employment terms review" }),
    ).not.toBeInTheDocument();
  },
);

it("offers posting reader and preparer assignments only from their exact catalog", async () => {
  setup();
  const prior = fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/iam/staff-access-templates?catalog=staff-postings-v1"))
        return Response.json(catalogDefinitions("staff-postings-v1"));
      if (url.includes("/iam/posting-assignment-proposals?")) return Response.json(page([]));
      return prior(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByLabelText("Assignment area"));
  await user.click(await screen.findByRole("option", { name: "Staff posting access" }));
  await user.click(screen.getByRole("button", { name: "Propose assignment" }));
  await user.click(screen.getByLabelText("Access template"));
  const templates = screen.getByRole("listbox");
  expect(templates).toHaveTextContent("Staff posting reader");
  expect(templates).toHaveTextContent("Staff posting preparer");
  expect(templates).not.toHaveTextContent("Staff posting submitter");
  expect(templates).not.toHaveTextContent("Staff posting reviewer");
  expect(templates).not.toHaveTextContent("Staff posting management reviewer");
  expect(templates).not.toHaveTextContent("Staff employment activator");
  expect(templates).not.toHaveTextContent("HR intake administrator");
  expect(
    vi
      .mocked(fetch)
      .mock.calls.some(([url]) => String(url).includes("/iam/posting-assignment-proposals?")),
  ).toBe(true);
});

const pharmacy = "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbb1";
const departmentRow = (node: string, name: string, status: string) => ({
  id: node,
  revision_id: `${node.slice(0, -1)}9`,
  hospital_id: hospital,
  version: 1,
  code: name.toUpperCase().replace(/\W/g, "-"),
  name,
  kind: "department",
  status,
  created_at: timestamp,
  effective_at: timestamp,
});
function departmentCatalogSetup(
  rows: unknown[] = [],
  supported: keyof typeof staffAccessTemplateCatalogs = "staff-department-posting-reviews-v1",
) {
  setup("proposer", rows);
  const prior = fetch;
  const commands: Array<{ url: string; body: Record<string, unknown> }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const catalog = new URL(url, "http://localhost").searchParams.get("catalog");
      if (catalog === supported) return Response.json(catalogDefinitions(supported));
      if (url.includes(`/organization/hospitals/${hospital}/departments/${pharmacy}/revisions`))
        return Response.json(page([departmentRow(pharmacy, "Synthetic Pharmacy", "active")]));
      if (url.includes(`/organization/hospitals/${hospital}/departments?`))
        return Response.json(
          page([
            departmentRow(pharmacy, "Synthetic Pharmacy", "active"),
            departmentRow("019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbb2", "Retired Ward", "retired"),
          ]),
        );
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body)) as Record<string, unknown>;
        commands.push({ url, body });
        return Response.json({
          ...row,
          id: url.split("/").at(-1),
          input: body,
          scope_kind: body.department_id ? "department" : "hospital",
          status: "pending_scope",
          version: 1,
        });
      }
      return prior(url, init);
    }),
  );
  return commands;
}
async function openPostingProposal() {
  const rendered = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await rendered.user.click(await screen.findByLabelText("Assignment area"));
  await rendered.user.click(await screen.findByRole("option", { name: "Staff posting access" }));
  await rendered.user.click(screen.getByRole("button", { name: "Propose assignment" }));
  return {
    ...rendered,
    form: within(await screen.findByRole("form", { name: "Propose assignment" })),
  };
}
async function fillProposal(
  user: ReturnType<typeof renderWithProviders>["user"],
  form: ReturnType<typeof within>,
) {
  await user.click(form.getByLabelText("Hospital"));
  await user.click(await screen.findByRole("option", { name: "Synthetic Hospital" }));
  await user.type(form.getByLabelText("Account ID"), "candidate");
  await user.type(form.getByLabelText("Reason"), "Pharmacy posting review duty");
}

it("proposes department posting review for one active department and its sub-units", async () => {
  const commands = departmentCatalogSetup();
  const { user, form } = await openPostingProposal();
  await user.click(form.getByLabelText("Applies to"));
  await user.click(await screen.findByRole("option", { name: "One department and its sub-units" }));
  await user.click(form.getByLabelText("Access template"));
  const templates = screen.getByRole("listbox");
  expect(templates).toHaveTextContent("Department posting reviewer");
  expect(templates).not.toHaveTextContent("Staff posting reviewer");
  await user.click(within(templates).getByRole("option", { name: "Department posting reviewer" }));
  await fillProposal(user, form);
  await user.click(await form.findByLabelText("Department"));
  expect(screen.queryByRole("option", { name: "Retired Ward" })).not.toBeInTheDocument();
  await user.click(await screen.findByRole("option", { name: "Synthetic Pharmacy" }));
  await user.click(form.getByRole("button", { name: "Submit proposal" }));
  await waitFor(() => expect(commands).toHaveLength(1));
  expect(commands[0]!.body).toMatchObject({
    hospital_id: hospital,
    department_id: pharmacy,
    template_key: "workforce.department_posting_reviewer.v1",
    subject: "candidate",
  });
});

it("requires a department before submitting department-scoped access", async () => {
  const commands = departmentCatalogSetup();
  const { user, form } = await openPostingProposal();
  await user.click(form.getByLabelText("Applies to"));
  await user.click(await screen.findByRole("option", { name: "One department and its sub-units" }));
  await fillProposal(user, form);
  await user.click(form.getByRole("button", { name: "Submit proposal" }));
  expect(await form.findByText("Choose a department.")).toBeInTheDocument();
  expect(commands).toHaveLength(0);
});

it("explains a missing department structure permission instead of offering a retry", async () => {
  const commands = departmentCatalogSetup();
  const catalog = fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) =>
      url.includes(`/organization/hospitals/${hospital}/departments?`)
        ? Response.json(
            { error: "forbidden", message: "Synthetic structure cutoff" },
            { status: 403 },
          )
        : catalog(url, init),
    ),
  );
  const { user, form } = await openPostingProposal();
  await user.click(form.getByLabelText("Applies to"));
  await user.click(await screen.findByRole("option", { name: "One department and its sub-units" }));
  await fillProposal(user, form);
  expect(
    await form.findByText(
      "You cannot read this hospital's departments. Ask a tenant administrator for hospital structure access.",
    ),
  ).toBeInTheDocument();
  expect(form.queryByRole("button", { name: "Retry departments" })).not.toBeInTheDocument();
  expect(form.queryByLabelText("Department")).not.toBeInTheDocument();
  expect(commands).toHaveLength(0);
});

it("keeps hospital-wide proposals free of department templates and department IDs", async () => {
  const commands = departmentCatalogSetup();
  const { user, form } = await openPostingProposal();
  await user.click(form.getByLabelText("Access template"));
  const templates = screen.getByRole("listbox");
  expect(templates).toHaveTextContent("Staff posting reviewer");
  expect(templates).not.toHaveTextContent("Department posting reviewer");
  await user.click(within(templates).getByRole("option", { name: "Staff posting reviewer" }));
  await fillProposal(user, form);
  expect(form.queryByLabelText("Department")).not.toBeInTheDocument();
  await user.click(form.getByRole("button", { name: "Submit proposal" }));
  await waitFor(() => expect(commands).toHaveLength(1));
  expect(commands[0]!.body).not.toHaveProperty("department_id");
  expect(commands[0]!.body.template_key).toBe("workforce.posting_reviewer.v1");
});

it("shows a department assignment's scope by its current department name", async () => {
  departmentCatalogSetup([
    {
      ...row,
      input: {
        ...row.input,
        department_id: pharmacy,
        template_key: "workforce.department_posting_reviewer.v1",
      },
      scope_kind: "department",
    },
  ]);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByLabelText("Assignment area"));
  await user.click(await screen.findByRole("option", { name: "Staff posting access" }));
  expect(
    await screen.findByText("Applies to: Synthetic Pharmacy and its current sub-units"),
  ).toBeInTheDocument();
});

async function openLeaveProposal() {
  const rendered = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await rendered.user.click(await screen.findByRole("button", { name: "Propose assignment" }));
  return {
    ...rendered,
    form: within(await screen.findByRole("form", { name: "Propose assignment" })),
  };
}

it("proposes department leave review for one active department and its sub-units", async () => {
  const commands = departmentCatalogSetup([], "staff-department-leave-reviews-v1");
  const { user, form } = await openLeaveProposal();
  await user.click(form.getByLabelText("Applies to"));
  await user.click(await screen.findByRole("option", { name: "One department and its sub-units" }));
  await user.click(form.getByLabelText("Access template"));
  const templates = screen.getByRole("listbox");
  expect(templates).toHaveTextContent("Department leave reviewer");
  expect(templates).not.toHaveTextContent("Department posting reviewer");
  expect(within(templates).queryByRole("option", { name: "Leave reviewer" })).toBeNull();
  await user.click(within(templates).getByRole("option", { name: "Department leave reviewer" }));
  await fillProposal(user, form);
  await user.click(await form.findByLabelText("Department"));
  await user.click(await screen.findByRole("option", { name: "Synthetic Pharmacy" }));
  await user.click(form.getByRole("button", { name: "Submit proposal" }));
  await waitFor(() => expect(commands).toHaveLength(1));
  expect(commands[0]!.body).toMatchObject({
    hospital_id: hospital,
    department_id: pharmacy,
    template_key: "workforce.department_leave_reviewer.v1",
    subject: "candidate",
  });
});

it("keeps hospital-wide leave proposals on the hospital leave reviewer", async () => {
  const commands = departmentCatalogSetup([], "staff-department-leave-reviews-v1");
  const { user, form } = await openLeaveProposal();
  await user.click(form.getByLabelText("Access template"));
  const templates = screen.getByRole("listbox");
  expect(templates).not.toHaveTextContent("Department leave reviewer");
  await user.click(within(templates).getByRole("option", { name: "Leave reviewer" }));
  await fillProposal(user, form);
  await user.click(form.getByRole("button", { name: "Submit proposal" }));
  await waitFor(() => expect(commands).toHaveLength(1));
  expect(commands[0]!.body).not.toHaveProperty("department_id");
  expect(commands[0]!.body.template_key).toBe("workforce.leave_reviewer.v1");
});

it("offers no department leave scope before the department leave catalog is verified", async () => {
  departmentCatalogSetup();
  const { form } = await openLeaveProposal();
  await screen.findByRole("form", { name: "Propose assignment" });
  expect(form.queryByLabelText("Applies to")).not.toBeInTheDocument();
});

it("shows a department leave assignment's scope by its current department name", async () => {
  departmentCatalogSetup(
    [
      {
        ...row,
        input: {
          ...row.input,
          department_id: pharmacy,
          template_key: "workforce.department_leave_reviewer.v1",
        },
        scope_kind: "department",
      },
    ],
    "staff-department-leave-reviews-v1",
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  expect(
    await screen.findByText("Applies to: Synthetic Pharmacy and its current sub-units"),
  ).toBeInTheDocument();
});

it("rejects a template catalog whose declared scope kinds differ", async () => {
  setup("proposer", []);
  const prior = fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("catalog=staff-department-posting-reviews-v1"))
        return Response.json(
          catalogDefinitions("staff-department-posting-reviews-v1").map((template) =>
            template.key === "workforce.department_posting_reviewer.v1"
              ? { ...template, scope_kinds: ["hospital"] }
              : template,
          ),
        );
      return prior(url, init);
    }),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  expect(
    await screen.findByText("Staff posting assignments are unavailable in this workspace."),
  ).toBeInTheDocument();
});

it.each(["missing", "broader", "extra"])(
  "does not offer posting authority from a %s catalog",
  async (kind) => {
    setup();
    const prior = fetch;
    const definitions = catalogDefinitions("staff-postings-v1");
    const altered =
      kind === "missing"
        ? definitions.filter((template) => template.family !== "posting")
        : kind === "extra"
          ? [...definitions, definitions[0]]
          : definitions.map((template) =>
              template.family === "posting"
                ? {
                    ...template,
                    permissions: [...template.permissions, "organization.structure.manage"],
                  }
                : template,
            );
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.includes("/iam/staff-access-templates?catalog=staff-postings-v1"))
          return Response.json(altered);
        return prior(url, init);
      }),
    );
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <AccessPage />
      </NavigationGuardProvider>,
    );
    await screen.findByText("Staff posting assignments are unavailable in this workspace.");
    await user.click(await screen.findByLabelText("Assignment area"));
    expect(screen.queryByRole("option", { name: "Staff posting access" })).not.toBeInTheDocument();
  },
);

it("offers cutoff administration only from its verified lifecycle catalog", async () => {
  setup();
  const prior = fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("catalog=staff-posting-lifecycle-v1"))
        return Response.json(catalogDefinitions("staff-posting-lifecycle-v1"));
      if (url.includes("catalog=staff-posting-management-v1"))
        return Response.json(catalogDefinitions("staff-posting-management-v1"));
      if (url.includes("/iam/posting-assignment-proposals?")) return Response.json(page([]));
      return prior(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByLabelText("Assignment area"));
  await user.click(await screen.findByRole("option", { name: "Staff posting access" }));
  await user.click(screen.getByRole("button", { name: "Propose assignment" }));
  await user.click(screen.getByLabelText("Access template"));
  expect(
    await screen.findByRole("option", { name: "Staff posting cutoff administrator" }),
  ).toBeInTheDocument();
});

it.each(["missing", "broader", "extra", "unavailable"])(
  "retains verified management roles when the lifecycle catalog is %s",
  async (kind) => {
    setup();
    const prior = fetch;
    const definitions = catalogDefinitions("staff-posting-lifecycle-v1");
    const altered =
      kind === "missing"
        ? definitions.filter((t) => t.key !== "workforce.posting_cutoff_admin.v1")
        : kind === "extra"
          ? [...definitions, definitions[0]]
          : definitions.map((t) =>
              t.key === "workforce.posting_cutoff_admin.v1"
                ? { ...t, permissions: [...t.permissions, "workforce.posting.review"] }
                : t,
            );
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.includes("catalog=staff-posting-lifecycle-v1"))
          return kind === "unavailable"
            ? Response.json({ error: "unavailable" }, { status: 503 })
            : Response.json(altered);
        if (url.includes("catalog=staff-posting-management-v1"))
          return Response.json(catalogDefinitions("staff-posting-management-v1"));
        if (url.includes("/iam/posting-assignment-proposals?")) return Response.json(page([]));
        return prior(url, init);
      }),
    );
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <AccessPage />
      </NavigationGuardProvider>,
    );
    await user.click(await screen.findByLabelText("Assignment area"));
    await user.click(await screen.findByRole("option", { name: "Staff posting access" }));
    await user.click(screen.getByRole("button", { name: "Propose assignment" }));
    await user.click(screen.getByLabelText("Access template"));
    expect(screen.getByRole("listbox")).toHaveTextContent("Staff posting management reviewer");
    expect(screen.getByRole("listbox")).not.toHaveTextContent("Staff posting cutoff administrator");
  },
);

it.each(["missing", "broader", "extra", "unavailable"])(
  "keeps older verified posting roles when management catalog is %s",
  async (kind) => {
    setup();
    const prior = fetch;
    const definitions = catalogDefinitions("staff-posting-management-v1");
    const altered =
      kind === "missing"
        ? definitions.filter(
            (template) => template.key !== "workforce.posting_management_reviewer.v1",
          )
        : kind === "extra"
          ? [...definitions, definitions[0]]
          : definitions.map((template) =>
              template.key === "workforce.posting_management_reviewer.v1"
                ? {
                    ...template,
                    permissions: [...template.permissions, "organization.structure.manage"],
                  }
                : template,
            );
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.includes("catalog=staff-posting-management-v1"))
          return kind === "unavailable"
            ? Response.json({ error: "unavailable" }, { status: 503 })
            : Response.json(altered);
        if (url.includes("catalog=staff-posting-reviews-v1"))
          return Response.json(catalogDefinitions("staff-posting-reviews-v1"));
        if (url.includes("catalog=staff-postings-v1"))
          return Response.json(catalogDefinitions("staff-postings-v1"));
        if (url.includes("/iam/posting-assignment-proposals?")) return Response.json(page([]));
        return prior(url, init);
      }),
    );
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <AccessPage />
      </NavigationGuardProvider>,
    );
    await user.click(await screen.findByLabelText("Assignment area"));
    await user.click(await screen.findByRole("option", { name: "Staff posting access" }));
    await user.click(screen.getByRole("button", { name: "Propose assignment" }));
    await user.click(screen.getByLabelText("Access template"));
    const templates = screen.getByRole("listbox");
    expect(templates).toHaveTextContent("Staff posting reviewer");
    expect(templates).toHaveTextContent("Staff posting submitter");
    expect(templates).not.toHaveTextContent("Staff posting management reviewer");
  },
);

it("retains an uncertain management renewal and blocks retry until its exact catalog is restored", async () => {
  const management = {
    ...row,
    status: "active",
    input: { ...row.input, template_key: "workforce.posting_management_reviewer.v1" },
  };
  setup("proposer", [management]);
  const prior = fetch;
  let available = true;
  const commands: Array<{ url: string; body: string; tenant: string | null }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("catalog=staff-posting-management-v1"))
        return available
          ? Response.json(catalogDefinitions("staff-posting-management-v1"))
          : Response.json({ error: "unavailable" }, { status: 503 });
      if (url.includes("catalog=staff-posting-reviews-v1"))
        return Response.json(catalogDefinitions("staff-posting-reviews-v1"));
      if (url.includes("catalog=staff-postings-v1"))
        return Response.json(catalogDefinitions("staff-postings-v1"));
      if (init?.method === "PUT") {
        commands.push({
          url,
          body: String(init.body),
          tenant: new Headers(init.headers).get("X-Helivanta-Expected-Tenant"),
        });
        if (commands.length === 1) return Response.json({ error: "unavailable" }, { status: 502 });
        return Response.json({
          ...management,
          id: url.split("/").at(-1),
          input: JSON.parse(String(init.body)),
          status: "pending_scope",
          version: 1,
        });
      }
      return prior(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByLabelText("Assignment area"));
  await user.click(await screen.findByRole("option", { name: "Staff posting access" }));
  await user.click(await screen.findByRole("button", { name: "Renew access" }));
  const form = within(screen.getByRole("form", { name: "Renew assignment" }));
  const local = (value: Date) =>
    new Date(value.getTime() - value.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  fireEvent.change(form.getByLabelText("Starts at"), {
    target: { value: local(new Date(Date.now() + 2 * 86400000)) },
  });
  fireEvent.change(form.getByLabelText("Expires at"), {
    target: { value: local(new Date(Date.now() + 3 * 86400000)) },
  });
  await user.type(
    form.getByLabelText("Reason"),
    "Independent continued hospital management review duty",
  );
  await user.click(form.getByRole("button", { name: "Propose renewal" }));
  expect(await screen.findByText(/may have been recorded/)).toBeInTheDocument();
  expect(commands).toHaveLength(1);
  expect(JSON.parse(commands[0]!.body).template_key).toBe(
    "workforce.posting_management_reviewer.v1",
  );
  available = false;
  await user.click(screen.getByRole("button", { name: "Refresh assignments" }));
  await screen.findByText(
    "This access template is unavailable. Refresh its verified catalog before submitting or retrying.",
  );
  expect(form.getByRole("button", { name: "Retry same request" })).toBeDisabled();
  await user.click(form.getByRole("button", { name: "Retry same request" }));
  expect(commands).toHaveLength(1);
  available = true;
  await user.click(screen.getByRole("button", { name: "Refresh assignments" }));
  await waitFor(() =>
    expect(form.getByRole("button", { name: "Retry same request" })).toBeEnabled(),
  );
  await user.click(form.getByRole("button", { name: "Retry same request" }));
  await waitFor(() => expect(commands).toHaveLength(2));
  expect(commands[1]).toEqual(commands[0]);
  expect(commands[0]?.tenant).toBe(tenant);
  expect(await screen.findByText(/Proposal recorded/)).toBeInTheDocument();
});

it("offers source consumption only from its exact verified source catalog", async () => {
  setup();
  const prior = fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("catalog=staff-posting-source-v1"))
        return Response.json(catalogDefinitions("staff-posting-source-v1"));
      if (url.includes("/iam/posting-assignment-proposals?")) return Response.json(page([]));
      return prior(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByLabelText("Assignment area"));
  await user.click(await screen.findByRole("option", { name: "Staff posting access" }));
  await user.click(screen.getByRole("button", { name: "Propose assignment" }));
  await user.click(screen.getByLabelText("Access template"));
  expect(
    await screen.findByRole("option", { name: "Staff posting source consumer" }),
  ).toBeInTheDocument();
});

it.each(["missing", "broader", "extra", "unavailable"])(
  "withholds source capability and preserves verified lifecycle roles when source catalog is %s",
  async (kind) => {
    setup();
    const prior = fetch;
    const definitions = catalogDefinitions("staff-posting-source-v1");
    const altered =
      kind === "missing"
        ? definitions.filter((t) => t.key !== "workforce.posting_source_consumer.v1")
        : kind === "extra"
          ? [...definitions, definitions[0]]
          : definitions.map((t) =>
              t.key === "workforce.posting_source_consumer.v1"
                ? { ...t, permissions: [...t.permissions, "workforce.posting.proposal.read"] }
                : t,
            );
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.includes("catalog=staff-posting-source-v1"))
          return kind === "unavailable"
            ? Response.json({ error: "unavailable" }, { status: 503 })
            : Response.json(altered);
        if (url.includes("catalog=staff-posting-lifecycle-v1"))
          return Response.json(catalogDefinitions("staff-posting-lifecycle-v1"));
        if (url.includes("/iam/posting-assignment-proposals?")) return Response.json(page([]));
        return prior(url, init);
      }),
    );
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <AccessPage />
      </NavigationGuardProvider>,
    );
    await user.click(await screen.findByLabelText("Assignment area"));
    await user.click(await screen.findByRole("option", { name: "Staff posting access" }));
    await user.click(screen.getByRole("button", { name: "Propose assignment" }));
    await user.click(screen.getByLabelText("Access template"));
    expect(
      await screen.findByRole("option", { name: "Staff posting cutoff administrator" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("option", { name: "Staff posting source consumer" }),
    ).not.toBeInTheDocument();
  },
);

it("offers the exact Pharmacy relationship templates in their own assignment area", async () => {
  setup("proposer", []);
  const prior = fetch;
  const requests: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      requests.push(url);
      if (url.includes("catalog=pharmacy-department-bindings-v1"))
        return Response.json(catalogDefinitions("pharmacy-department-bindings-v1"));
      if (url.includes("/iam/pharmacy-binding-assignment-proposals?"))
        return Response.json(page([]));
      return prior(url, init);
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <AccessPage />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByLabelText("Assignment area"));
  await user.click(
    await screen.findByRole("option", { name: "Pharmacy department relationships" }),
  );
  await user.click(screen.getByRole("button", { name: "Propose assignment" }));
  await user.click(screen.getByLabelText("Access template"));
  const list = screen.getByRole("listbox");
  for (const template of catalogDefinitions("pharmacy-department-bindings-v1"))
    expect(list).toHaveTextContent(template.name);
  expect(list).not.toHaveTextContent("Staff posting reviewer");
  expect(
    requests.some((url) => url.includes("/iam/pharmacy-binding-assignment-proposals?limit=50")),
  ).toBe(true);
});

it.each(["unavailable", "extra", "widened"] as const)(
  "withholds Pharmacy assignment preparation when its catalog is %s",
  async (kind) => {
    setup("proposer", []);
    const prior = fetch;
    const definitions = catalogDefinitions("pharmacy-department-bindings-v1");
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (url.includes("catalog=pharmacy-department-bindings-v1")) {
          if (kind === "unavailable")
            return Response.json({ error: "unavailable" }, { status: 503 });
          return Response.json(
            kind === "extra"
              ? [...definitions, definitions[0]]
              : definitions.map((template) => ({
                  ...template,
                  permissions: [...template.permissions, "pharmacy.catalogue.read"],
                })),
          );
        }
        return prior(url, init);
      }),
    );
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <AccessPage />
      </NavigationGuardProvider>,
    );
    await user.click(await screen.findByLabelText("Assignment area"));
    expect(
      screen.queryByRole("option", { name: "Pharmacy department relationships" }),
    ).not.toBeInTheDocument();
  },
);

// #1595: a refused hospital or department read is a refusal, not an outage with a retry.
it("shows refused hospitals in the proposal form as a refusal, with no retry", async () => {
  departmentCatalogSetup();
  const catalog = fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) =>
      new URL(url, "http://localhost").pathname.endsWith("/workforce/hospitals")
        ? Response.json({ error: "forbidden", message: "secret detail" }, { status: 403 })
        : catalog(url, init),
    ),
  );
  const { form } = await openPostingProposal();
  expect(
    await form.findByText("This wasn't found, or you don't have access to it."),
  ).toBeInTheDocument();
  expect(form.queryByRole("button", { name: /retry hospitals|try again/i })).toBeNull();
});

it("shows hidden departments in the proposal form as a refusal, with no retry", async () => {
  departmentCatalogSetup();
  const catalog = fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) =>
      url.includes(`/organization/hospitals/${hospital}/departments?`)
        ? Response.json({ error: "not_found", message: "secret detail" }, { status: 404 })
        : catalog(url, init),
    ),
  );
  const { user, form } = await openPostingProposal();
  await user.click(form.getByLabelText("Applies to"));
  await user.click(await screen.findByRole("option", { name: "One department and its sub-units" }));
  await fillProposal(user, form);
  expect(
    await form.findByText("This wasn't found, or you don't have access to it."),
  ).toBeInTheDocument();
  expect(form.queryByRole("button", { name: /retry departments|try again/i })).toBeNull();
});
