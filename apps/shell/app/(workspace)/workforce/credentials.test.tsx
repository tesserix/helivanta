import "@/test/select";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import type { Hospital, Staff } from "@helivanta/api";
import { StaffCredentialPanel } from "./credential-panel";
import CredentialRegister from "./credentials/page";

// Synthetic staff, licence numbers and councils only.
const tenant = "33333333-3333-4333-8333-333333333333";
const hospital: Hospital = {
  id: "11111111-1111-4111-8111-111111111111",
  name: "Synthetic Hospital",
  access_level: "manager",
};
const staff: Staff = {
  id: "22222222-2222-4222-8222-222222222222",
  hospital_id: hospital.id,
  display_name: "Avery Synthetic",
  employee_code: "SYN-CRED-001",
  job_category: "nurse",
  employment_status: "active",
  created_at: "2026-10-15T00:00:00Z",
  updated_at: "2026-10-15T00:00:00Z",
};
const licence = "44444444-4444-4444-8444-444444444444";
const review = "55555555-5555-4555-8555-555555555555";
const file = "66666666-6666-4666-8666-666666666666";
const stamp = "2026-10-15T00:00:00Z";
const base = `/api/v1/workforce/hospitals/${hospital.id}`;
const credentialsURL = `${base}/staff/${staff.id}/credentials`;
const pendingReview = (by: string) => ({
  id: review,
  staff_id: staff.id,
  credential_id: licence,
  base_version: 1,
  status: "pending",
  type: "Synthetic nursing registration",
  number: "SYN-RN-2",
  issuer: "Synthetic Council",
  valid_from: "2026-10-01",
  valid_until: "2031-09-30",
  evidence_document_ids: [file],
  reason: "Checked the synthetic council register",
  proposed_by_subject: by,
  decision_note: "",
  created_at: stamp,
});
const credentials = (pending?: unknown) => ({
  time_zone: "Asia/Kolkata",
  local_date: "2026-10-18",
  credentials: [
    {
      id: licence,
      type: "Synthetic nursing registration",
      number: "SYN-RN-1",
      issuer: "Synthetic Council",
      valid_from: "2021-10-01",
      valid_until: "2026-10-20",
      reviewed: {
        number: "SYN-RN-1",
        valid_from: "2021-10-01",
        valid_until: "2026-10-20",
        version: 1,
        review_id: "77777777-7777-4777-8777-777777777777",
        updated_at: stamp,
      },
      status: "valid",
      days_remaining: 2,
      ...(pending ? { pending } : {}),
    },
  ],
});
const evidence = (status: string, id = file, record = licence) => ({
  id,
  hospital_id: hospital.id,
  staff_id: staff.id,
  record_id: record,
  record_kind: "credential",
  record_version: 1,
  category: "credential_evidence",
  filename: `synthetic-${status}.png`,
  content_type: "image/png",
  size: 3,
  sha256: "a".repeat(64),
  status,
  scan_attempt: 1,
  uploaded_at: stamp,
  scanned_at: null,
  failure_code: null,
  created_at: stamp,
  updated_at: stamp,
  expires_at: "2026-10-15T00:30:00Z",
});
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });

beforeEach(() => window.localStorage.clear());
afterEach(() => vi.unstubAllGlobals());

function fixture({
  permissions = ["workforce.enrollment.manage", "workforce.documents.manage"],
  subject = "synthetic-hr",
  pending,
  register = page([]),
  registerStatus = 200,
}: {
  permissions?: string[];
  subject?: string;
  pending?: unknown;
  register?: unknown;
  registerStatus?: number;
} = {}) {
  const writes: { url: string; method?: string; body: unknown }[] = [];
  const reads: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/iam/me/permissions"))
        return Response.json({ data: permissions, subject, tenant_id: tenant });
      if (url.endsWith("/iam/me/tenants"))
        return Response.json({ data: [{ tenant_id: tenant, current: true, roles: [] }] });
      if (init?.method === "PUT" || init?.method === "POST") {
        const body = JSON.parse(String(init.body));
        writes.push({ url, method: init.method, body });
        const id = url.match(/reviews\/([0-9a-f-]+)/)?.[1];
        const status = url.endsWith("/decision")
          ? body.decision === "approve"
            ? "approved"
            : "rejected"
          : "pending";
        return Response.json({ ...pendingReview(subject), id, status });
      }
      reads.push(url);
      if (url === credentialsURL) return Response.json(credentials(pending));
      if (url.includes("/documents"))
        return Response.json(
          page([
            evidence("available"),
            evidence("scan_pending", "88888888-8888-4888-8888-888888888888"),
            evidence("revoked", "99999999-9999-4999-8999-999999999999"),
            evidence("available", "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", staff.id),
          ]),
        );
      if (url.endsWith("/workforce/hospitals")) return Response.json(page([hospital]));
      if (url.includes("/hr/credentials"))
        return Response.json(register, { status: registerStatus });
      if (url.includes("/reviews")) return Response.json(page([pendingReview("synthetic-hr")]));
      return Response.json(page([]));
    }),
  );
  return { writes, reads };
}

const panel = () =>
  renderWithProviders(
    <StaffCredentialPanel
      account={{ tenantId: tenant, subject: "synthetic-hr" }}
      hospital={hospital}
      staff={staff}
      onClose={() => {}}
    />,
  );

it("shows reviewed validity in the hospital calendar", async () => {
  fixture();
  panel();
  expect(await screen.findByText("Valid · 2 days left")).toBeInTheDocument();
  expect(screen.getByText("Today is 2026-10-18 in Asia/Kolkata.")).toBeInTheDocument();
  expect(screen.getByText(/SYN-RN-1 · 2021-10-01 to 2026-10-20 · Reviewed v1/)).toBeInTheDocument();
});

it("proposes a review only with evidence and a reason, citing this credential's files", async () => {
  const { writes } = fixture();
  const { user } = panel();
  await user.click(
    await screen.findByRole("button", { name: "Propose review of Synthetic nursing registration" }),
  );
  const form = await screen.findByRole("form", { name: "Propose credential review" });
  await user.click(within(form).getByRole("button", { name: "Propose review" }));
  expect(await within(form).findByText("Choose at least one evidence file.")).toBeInTheDocument();
  expect(within(form).getByText("Say what you checked.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  const boxes = await within(form).findAllByRole("checkbox");
  expect(boxes.map((b) => b.parentElement?.textContent)).toEqual([
    "synthetic-available.png",
    "synthetic-scan_pending.png· still scanning",
  ]);
  const number = within(form).getByLabelText("Number");
  await user.clear(number);
  await user.type(number, "SYN-RN-2");
  const until = within(form).getByLabelText("Valid until");
  await user.clear(until);
  await user.type(until, "2031-09-30");
  await user.click(boxes[0]!);
  await user.type(within(form).getByLabelText("What you checked"), "Checked the council register");
  await user.click(within(form).getByRole("button", { name: "Propose review" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]!.method).toBe("PUT");
  expect(writes[0]!.url).toMatch(
    new RegExp(`^${credentialsURL}/${licence}/reviews/[0-9a-f-]{36}$`),
  );
  expect(writes[0]!.body).toEqual({
    expected_version: 1,
    number: "SYN-RN-2",
    valid_from: "2021-10-01",
    valid_until: "2031-09-30",
    evidence_document_ids: [file],
    reason: "Checked the council register",
  });
  expect(await screen.findByText("Review proposed")).toBeInTheDocument();
});

it("lets a second person approve, and needs a note to reject", async () => {
  const { writes } = fixture({
    permissions: ["workforce.enrollment.manage", "workforce.credentials.review"],
    subject: "synthetic-reviewer",
    pending: pendingReview("synthetic-hr"),
  });
  const { user } = panel();
  await user.click(
    await screen.findByRole("button", {
      name: "Review proposal for Synthetic nursing registration",
    }),
  );
  const form = await screen.findByRole("form", { name: "Decide credential review" });
  expect(screen.getByText("SYN-RN-2 · 2026-10-01 to 2031-09-30")).toBeInTheDocument();
  await user.click(within(form).getByRole("button", { name: "Reject" }));
  expect(await within(form).findByText("Say why you reject it.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.click(within(form).getByRole("button", { name: "Approve" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: `${credentialsURL}/${licence}/reviews/${review}/decision`,
    method: "POST",
    body: { expected_version: 1, decision: "approve", note: "" },
  });
  expect(await screen.findByText("Credential approved")).toBeInTheDocument();
});

it("offers the proposer a withdrawal, never a decision", async () => {
  const { writes } = fixture({
    permissions: ["workforce.enrollment.manage", "workforce.credentials.review"],
    pending: pendingReview("synthetic-hr"),
  });
  const { user } = panel();
  const withdraw = await screen.findByRole("button", {
    name: "Withdraw proposal for Synthetic nursing registration",
  });
  expect(screen.queryByRole("button", { name: /^Review proposal/ })).toBeNull();
  expect(screen.queryByRole("button", { name: /^Propose review/ })).toBeNull();
  await user.click(withdraw);
  const form = await screen.findByRole("form", { name: "Withdraw credential review" });
  await user.click(within(form).getByRole("button", { name: "Withdraw proposal" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({
    url: `${credentialsURL}/${licence}/reviews/${review}/withdraw`,
    method: "POST",
    body: { note: "" },
  });
  expect(await screen.findByText("Proposal withdrawn")).toBeInTheDocument();
});

it("hides decisions from members without the review permission", async () => {
  fixture({ subject: "synthetic-other", pending: pendingReview("synthetic-hr") });
  panel();
  await screen.findByText("Valid · 2 days left");
  expect(screen.queryByRole("button", { name: /^Review proposal/ })).toBeNull();
  expect(screen.queryByRole("button", { name: /^Withdraw proposal/ })).toBeNull();
});

it("filters the register by expiry window and checks the window first", async () => {
  const { reads } = fixture({
    register: page([
      {
        staff_id: staff.id,
        employee_code: staff.employee_code,
        display_name: staff.display_name,
        credential_id: licence,
        type: "Synthetic nursing registration",
        number: "SYN-RN-1",
        issuer: "Synthetic Council",
        valid_from: "2021-10-01",
        valid_until: "2026-10-15",
        version: 1,
        review_id: review,
        status: "expired",
        days_remaining: -3,
      },
    ]),
  });
  const { user } = renderWithProviders(<CredentialRegister />);
  const table = await screen.findByRole("table");
  expect(within(table).getByText("Expired 3 days ago")).toBeInTheDocument();
  const form = screen.getByRole("form", { name: "Register filter" });
  const input = within(form).getByLabelText("Expiring within (days)");
  await user.type(input, "400");
  await user.click(within(form).getByRole("button", { name: "Show credentials" }));
  expect(await within(form).findByText("Use 0 to 365 days.")).toBeInTheDocument();
  await user.clear(input);
  await user.type(input, "30");
  await user.click(within(form).getByRole("button", { name: "Show credentials" }));
  await waitFor(() => expect(reads).toContain(`${base}/hr/credentials?expiring_within=30`));
  expect(reads.filter((u) => u.includes("expiring_within=400"))).toHaveLength(0);
});

it("explains that filtering needs the hospital time zone", async () => {
  fixture({
    registerStatus: 409,
    register: {
      error: "conflict",
      message: "Set the hospital time zone in roster settings before filtering by expiry.",
      reason: "time_zone_unset",
    },
  });
  renderWithProviders(<CredentialRegister />);
  expect(
    await screen.findByText(
      "Set the hospital time zone in roster settings before filtering by expiry.",
    ),
  ).toBeInTheDocument();
});
