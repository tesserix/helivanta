import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { screen, waitFor, within } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import type { Hospital } from "@helivanta/api";
import { chooseOption } from "@/test/select";
import CredentialReminders from "./credential-reminders/page";

// Synthetic staff and credentials only.
const tenant = "33333333-3333-4333-8333-333333333333";
const hospital: Hospital = {
  id: "11111111-1111-4111-8111-111111111111",
  name: "Synthetic Hospital",
  access_level: "manager",
};
const reminderId = "44444444-4444-4444-8444-444444444444";
const base = `/api/v1/workforce/hospitals/${hospital.id}/hr`;
const stamp = "2026-10-18T00:00:00Z";
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
const reminder = (status = "open") => ({
  id: reminderId,
  staff_id: "22222222-2222-4222-8222-222222222222",
  employee_code: "SYN-CRED-001",
  display_name: "Avery Synthetic",
  credential_id: "55555555-5555-4555-8555-555555555555",
  type: "Synthetic nursing registration",
  valid_until: "2026-11-17",
  validity_version: 1,
  threshold_days: 30,
  due_on: "2026-10-18",
  status,
  ...(status === "acknowledged"
    ? { acknowledged_by_subject: "synthetic-hr", acknowledged_at: stamp }
    : {}),
  created_at: stamp,
});

beforeEach(() => window.localStorage.clear());
afterEach(() => vi.unstubAllGlobals());

function fixture({
  permissions = ["workforce.enrollment.manage"],
  policy = { version: 0, thresholds: [] as number[] },
  policyWrite = 200,
  ackWrite = 200,
}: {
  permissions?: string[];
  policy?: { version: number; thresholds: number[] };
  policyWrite?: number;
  ackWrite?: number;
} = {}) {
  const writes: { url: string; method?: string; body?: unknown }[] = [];
  const reads: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/iam/me/permissions"))
        return Response.json({ data: permissions, subject: "synthetic-hr", tenant_id: tenant });
      if (url.endsWith("/iam/me/tenants"))
        return Response.json({ data: [{ tenant_id: tenant, current: true, roles: [] }] });
      if (!url.includes("/workforce/")) return Response.json({});
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body));
        writes.push({ url, method: init.method, body });
        if (policyWrite !== 200)
          return Response.json(
            { error: "conflict", message: "conflict", reason: "version_conflict" },
            { status: policyWrite },
          );
        policy = { version: policy.version + 1, thresholds: body.thresholds };
        return Response.json({ ...policy, created_by_subject: "synthetic-hr", created_at: stamp });
      }
      if (init?.method === "POST") {
        writes.push({ url, method: init.method });
        if (ackWrite !== 200)
          return Response.json(
            { error: "conflict", message: "conflict", reason: "reminder_superseded" },
            { status: ackWrite },
          );
        return Response.json(reminder("acknowledged"));
      }
      reads.push(url);
      if (url.endsWith("/workforce/hospitals")) return Response.json(page([hospital]));
      if (url.endsWith("/credential-reminder-policy")) return Response.json(policy);
      if (url.includes("/credential-reminders?status=open"))
        return Response.json(page([reminder()]));
      if (url.includes("/credential-reminders?status=acknowledged"))
        return Response.json(page([reminder("acknowledged")]));
      return Response.json(page([]));
    }),
  );
  return { writes, reads };
}

it("says reminders are off and saves validated days against the current version", async () => {
  const { writes } = fixture();
  const { user } = renderWithProviders(<CredentialReminders />);
  expect(await screen.findByText(/Reminders are off at this hospital/)).toBeInTheDocument();
  const form = screen.getByRole("form", { name: "Reminder days" });
  const input = within(form).getByLabelText("Days before expiry");
  await user.type(input, "30, 30");
  await user.click(within(form).getByRole("button", { name: "Save reminder days" }));
  expect(await within(form).findByText("List each day once.")).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.clear(input);
  await user.type(input, "90, 30, 0");
  await user.click(within(form).getByRole("button", { name: "Save reminder days" }));
  expect(await screen.findByText("Reminder days saved")).toBeInTheDocument();
  expect(writes[0]).toEqual({
    url: `${base}/credential-reminder-policy`,
    method: "PUT",
    body: { expected_version: 0, thresholds: [90, 30, 0] },
  });
  expect(
    await screen.findByText(/Version 1: a reminder is raised 90, 30, 0 days before/),
  ).toBeInTheDocument();
});

it("explains a policy version conflict next to the form", async () => {
  fixture({ policy: { version: 2, thresholds: [30, 0] }, policyWrite: 409 });
  const { user } = renderWithProviders(<CredentialReminders />);
  const form = await screen.findByRole("form", { name: "Reminder days" });
  const input = within(form).getByLabelText("Days before expiry");
  await waitFor(() => expect(input).toHaveValue("30, 0"));
  await user.clear(input);
  await user.type(input, "7");
  await user.click(within(form).getByRole("button", { name: "Save reminder days" }));
  expect(
    await screen.findByText(/Someone changed the reminder days since you opened them/),
  ).toBeInTheDocument();
});

it("lists open reminders, acknowledges one and filters by status", async () => {
  const { writes, reads } = fixture({ policy: { version: 1, thresholds: [30, 0] } });
  const { user } = renderWithProviders(<CredentialReminders />);
  const table = await screen.findByRole("table");
  expect(within(table).getByText("30 days before expiry")).toBeInTheDocument();
  expect(within(table).queryByText(/SYN-RN/)).toBeNull();
  await user.click(
    within(table).getByRole("button", {
      name: "Acknowledge Synthetic nursing registration reminder for Avery Synthetic",
    }),
  );
  expect(await screen.findByText("Reminder acknowledged")).toBeInTheDocument();
  expect(writes[0]).toEqual({
    url: `${base}/credential-reminders/${reminderId}/acknowledgement`,
    method: "POST",
  });
  await chooseOption(user, screen.getByRole("combobox", { name: "Status" }), "Acknowledged");
  expect(await screen.findByText("Acknowledged by synthetic-hr")).toBeInTheDocument();
  expect(reads).toContain(`${base}/credential-reminders?status=acknowledged`);
});

it("explains a reminder that was superseded before it was acknowledged", async () => {
  fixture({ policy: { version: 1, thresholds: [30, 0] }, ackWrite: 409 });
  const { user } = renderWithProviders(<CredentialReminders />);
  const table = await screen.findByRole("table");
  await user.click(within(table).getByRole("button", { name: /^Acknowledge / }));
  expect(await screen.findByText("This reminder was replaced by a newer one.")).toBeInTheDocument();
});

it("asks for enrollment management access and never reads reminders without it", async () => {
  const { reads } = fixture({ permissions: [] });
  renderWithProviders(<CredentialReminders />);
  expect(
    await screen.findByText(
      "You need enrollment management access to manage credential reminders.",
    ),
  ).toBeInTheDocument();
  expect(reads.filter((u) => u.includes("credential-reminder"))).toHaveLength(0);
});
