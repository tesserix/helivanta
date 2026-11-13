import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { StaffLoginLinkPanel } from "../../app/(workspace)/workforce/login-link-panel";
import { IndependentStaffLinks } from "./independent-staff-links";
import { StaffAccountInitiation } from "./staff-account-initiation";

const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-owner" };
afterEach(() => vi.unstubAllGlobals());
it("retains uncertain creation and delivery commands and requires private setup before explicit enrollment", async () => {
  const onEnrolled = vi.fn();
  const creations: Array<{ url: string; body: string }> = [];
  const deliveries: string[] = [];
  let active = false;
  const receipt = (url: string) => ({
    schema_version: 1,
    command_id: url.split("/")[5],
    tenant_id: scope.tenantId,
    product_key: "helivanta",
    actor_subject: scope.subject,
    command_state: "created",
    prepared_at: "2026-11-13T00:00:00Z",
    user_id: "synthetic-new-staff",
    created_at: "2026-11-13T00:00:01Z",
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/membership-enrollments/") && init?.method === "PUT")
        return Response.json({
          id: url.split("/").at(-1),
          ...JSON.parse(String(init.body)),
          membership_id: "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb",
          role_key: "staff_member",
          membership_epoch: 1,
          actor_subject: scope.subject,
          created_at: "2026-11-13T00:00:00Z",
        });
      if (url.endsWith("/observations"))
        return Response.json({
          receipt: receipt(url),
          observation: {
            state: active ? "active" : "initial",
            email_verified: active,
            observed_at: "2026-11-13T00:00:02Z",
          },
        });
      if (url.includes("/invitations/")) {
        deliveries.push(url);
        if (deliveries.length === 1)
          return Response.json({ error: "unavailable", message: "Unavailable" }, { status: 503 });
        return Response.json({
          state: "email_action_required",
          invitation_id: url.split("/").at(-1),
          phase: "email",
          expires_at: "2026-11-13T01:00:00Z",
        });
      }
      if (init?.method === "PUT" && url.includes("staff-account-commands")) {
        creations.push({ url, body: String(init.body) });
        if (creations.length === 1)
          return Response.json({ error: "unavailable", message: "Unavailable" }, { status: 503 });
        return Response.json(receipt(url));
      }
      return Response.json({ data: [], page: { has_more: false, next_cursor: null } });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <StaffAccountInitiation scope={scope} onEnrolled={onEnrolled} />
    </NavigationGuardProvider>,
  );
  await user.type(screen.getByLabelText("First name"), "Synthetic");
  await user.type(screen.getByLabelText("Last name"), "Staff");
  await user.type(screen.getByLabelText("Work email"), "synthetic@example.invalid");
  await user.type(screen.getByLabelText("Account creation reason"), "Authorized account setup");
  await user.click(screen.getByRole("button", { name: "Create staff account" }));
  expect(await screen.findByText(/result may be unknown/i)).toBeInTheDocument();
  expect(screen.getByLabelText("Work email")).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Retry same account command" }));
  await screen.findByText("Account created. No access has been granted.");
  expect(creations).toHaveLength(2);
  expect(creations[0]).toEqual(creations[1]);
  expect(screen.queryByRole("button", { name: "Enroll staff member" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Send private setup email" }));
  await screen.findByRole("alert");
  await user.click(screen.getByRole("button", { name: "Retry same email delivery" }));
  await waitFor(() => expect(deliveries).toHaveLength(2));
  expect(deliveries[0]).toBe(deliveries[1]);
  await user.click(screen.getByRole("button", { name: "Check recipient setup" }));
  expect(await screen.findByText(/Recipient setup is incomplete/)).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Enroll staff member" })).toBeNull();
  active = true;
  await user.click(screen.getByRole("button", { name: "Check recipient setup" }));
  expect(await screen.findByRole("button", { name: "Enroll staff member" })).toBeInTheDocument();
  expect(screen.getByLabelText("Existing account ID")).toHaveValue("synthetic-new-staff");
  expect(screen.getByLabelText("Existing account ID")).toBeDisabled();
  await user.type(
    screen.getByLabelText("Enrollment reason"),
    "Verified recipient and first ordinary membership",
  );
  await user.click(screen.getByRole("button", { name: "Enroll staff member" }));
  expect(onEnrolled).not.toHaveBeenCalled();
  await user.click(screen.getByLabelText("I understand this creates ordinary membership only."));
  await user.click(screen.getByRole("button", { name: "Enroll staff member" }));
  await waitFor(() => expect(onEnrolled).toHaveBeenCalledExactlyOnceWith("synthetic-new-staff"));
  expect(screen.queryByRole("button", { name: "Enroll another account" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Set up another staff login" }));
  expect(screen.getByLabelText("Work email")).toHaveValue("");
  expect(screen.getByLabelText("Work email")).toBeEnabled();
  expect(screen.queryByRole("button", { name: "Enroll staff member" })).toBeNull();
});

const recoveredId = "019a7777-7777-7777-8777-777777777777";
const recoveredReceipt = {
  schema_version: 1,
  command_id: recoveredId,
  tenant_id: scope.tenantId,
  actor_subject: scope.subject,
  product_key: "helivanta",
  command_state: "created",
  prepared_at: "2026-11-13T00:00:00Z",
  user_id: "synthetic-new-staff",
};
async function recoverSaved() {
  const result = renderWithProviders(
    <NavigationGuardProvider>
      <StaffAccountInitiation scope={scope} />
    </NavigationGuardProvider>,
  );
  await result.user.type(screen.getByLabelText("Saved account command reference"), recoveredId);
  await result.user.click(screen.getByRole("button", { name: "Recover saved account command" }));
  return result;
}
it("retries an unresolved source delivery with its original delivery id", async () => {
  const deliveries: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/invitations/")) {
        deliveries.push(url);
        return Response.json({
          state: "delivery_unresolved",
          phase: "email",
          expires_at: "2026-11-13T01:00:00Z",
          invitation_id: url.split("/").at(-1),
        });
      }
      return Response.json(recoveredReceipt);
    }),
  );
  const { user } = await recoverSaved();
  await user.click(await screen.findByRole("button", { name: "Send private setup email" }));
  await screen.findByText(/Setup delivery: delivery unresolved/);
  expect(screen.queryByRole("button", { name: "Start a new delivery attempt" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Retry same email delivery" }));
  await waitFor(() => expect(deliveries).toHaveLength(2));
  expect(deliveries[0]).toBe(deliveries[1]);
});
it.each(["quarantined", "superseded", "unknown"])(
  "does not offer setup or membership for recovery state %s",
  async (recovery_state) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json({ ...recoveredReceipt, recovery_state })),
    );
    await recoverSaved();
    expect(await screen.findByText(/Account creation is unresolved/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Send private setup email" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Enroll staff member" })).toBeNull();
  },
);
it("removes enrollment when a fresh source observation becomes unavailable", async () => {
  let observations = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("/observations")) {
        if (++observations > 1)
          return Response.json({ error: "unavailable", message: "Unavailable" }, { status: 503 });
        return Response.json({
          receipt: recoveredReceipt,
          observation: {
            state: "active",
            email_verified: true,
            observed_at: "2026-11-13T00:00:02Z",
          },
        });
      }
      return Response.json(
        url.includes("membership-enrollments")
          ? { data: [], page: { has_more: false, next_cursor: null } }
          : recoveredReceipt,
      );
    }),
  );
  const { user } = await recoverSaved();
  await user.click(await screen.findByRole("button", { name: "Check recipient setup" }));
  await screen.findByRole("button", { name: "Enroll staff member" });
  await user.click(screen.getByRole("button", { name: "Check recipient setup" }));
  await screen.findByRole("alert");
  expect(screen.queryByRole("button", { name: "Enroll staff member" })).toBeNull();
});
it("locks account actions after the current owner is refused", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ error: "forbidden", message: "No access" }, { status: 403 })),
  );
  await recoverSaved();
  await screen.findByRole("alert");
  expect(screen.getByRole("button", { name: "Create staff account" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Recover saved account command" })).toBeDisabled();
});

it.each(["hospital", "pharmacy"])(
  "continues %s enrollment into a separate unsubmitted reviewed Staff link",
  async (kind) => {
    const writes: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        if (init?.method && init.method !== "GET") writes.push(url);
        if (url.includes("permissions"))
          return Response.json({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["iam.member.manage", "workforce.login_links.manage"],
          });
        if (url.endsWith("/observations"))
          return Response.json({
            receipt: recoveredReceipt,
            observation: {
              state: "active",
              email_verified: true,
              observed_at: "2026-11-13T00:00:02Z",
            },
          });
        if (url.includes("staff-account-commands")) return Response.json(recoveredReceipt);
        if (url.includes("membership-enrollments/") && init?.method === "PUT")
          return Response.json({
            id: url.split("/").at(-1),
            ...JSON.parse(String(init.body)),
            membership_id: "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb",
            role_key: "staff_member",
            membership_epoch: 1,
            actor_subject: scope.subject,
            created_at: "2026-11-13T00:00:00Z",
          });
        return Response.json({ data: [], page: { has_more: false, next_cursor: null } });
      }),
    );
    const hospital = {
      id: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa",
      name: "Synthetic Hospital",
      access_level: "manager" as const,
    };
    const staff = {
      id: "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb",
      hospital_id: hospital.id,
      display_name: "Synthetic Staff",
      employee_code: "SYN-01",
      job_category: "nurse" as const,
      employment_status: "active" as const,
      created_at: "2026-11-13T00:00:00Z",
      updated_at: "2026-11-13T00:00:00Z",
    };
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        {kind === "hospital" ? (
          <StaffLoginLinkPanel hospital={hospital} staff={staff} onClose={() => {}} />
        ) : (
          <IndependentStaffLinks
            scope={{
              ...scope,
              employerKind: "pharmacy",
              employerId: hospital.id,
              staffId: staff.id,
            }}
            staffVersion={1}
            canManage
            canReview
            canInitiate
          />
        )}
      </NavigationGuardProvider>,
    );
    await user.click(await screen.findByRole("button", { name: "Set up new staff login" }));
    await user.type(screen.getByLabelText("Saved account command reference"), recoveredId);
    await user.click(screen.getByRole("button", { name: "Recover saved account command" }));
    await user.click(await screen.findByRole("button", { name: "Check recipient setup" }));
    await user.type(
      await screen.findByLabelText("Enrollment reason"),
      "Confirmed first ordinary membership",
    );
    await user.click(screen.getByLabelText("I understand this creates ordinary membership only."));
    await user.click(screen.getByRole("button", { name: "Enroll staff member" }));
    expect(await screen.findByLabelText("Account ID")).toHaveValue("synthetic-new-staff");
    expect(screen.getByLabelText("Account ID")).toBeDisabled();
    expect(
      screen.getByRole("button", {
        name: kind === "hospital" ? "Submit proposal" : "Propose account link",
      }),
    ).toBeEnabled();
    expect(writes).toHaveLength(2);
    expect(writes[0]).toContain("/observations");
    expect(writes[1]).toContain("/membership-enrollments/");
  },
);
