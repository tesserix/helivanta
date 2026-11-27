import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { useReleaseCapabilities } from "@helivanta/api";
import { releaseSnapshotForTest } from "@helivanta/api/testing";
import { renderWithNamedAccounts } from "../../test/account-names";
import { StaffMFARecoveryPanel } from "./staff-mfa-recovery";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "acting-admin",
  employerKind: "hospital" as const,
  employerId: "11111111-1111-4111-8111-111111111112",
  staffId: "11111111-1111-4111-8111-111111111113",
};
const link = { id: "019f39c2-1738-7998-b5de-6d4214f614e1", version: 3, subject: "staff-user" };
const record = {
  id: scope.staffId,
  hospital_id: scope.employerId,
  employee_code: "SYN-MFA",
  display_name: "QA Staff",
  job_category: "other",
  employment_status: "active",
  version: 8,
  directory_context_version: "v1",
  created_at: "2026-11-19T00:00:00Z",
  updated_at: "2026-11-19T00:00:00Z",
};
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  sessionStorage.clear();
});
function AvailabilityProbe() {
  const release = useReleaseCapabilities();
  return release.available("staff_mfa_recovery") ? <span>Availability checked</span> : null;
}

function setup(enabled = true, admin = true) {
  const writes: Array<{ url: string; body: Record<string, unknown> }> = [];
  const reads: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/iam/me/permissions"))
        return Response.json({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: ["workforce.login_links.manage", ...(admin ? ["iam.member.manage"] : [])],
        });
      if (url.endsWith("/workspace/release-capabilities"))
        return Response.json(
          releaseSnapshotForTest(scope, [
            "team_accounts",
            "staff_core",
            ...(enabled ? ["staff_mfa_recovery"] : []),
          ]),
        );
      if (url.endsWith("?directory_context=v1")) return Response.json(record);
      if (url.includes("/mfa-recoveries/")) {
        const command = url.split("/mfa-recoveries/")[1].split("/")[0];
        if (init?.method === "PUT") {
          writes.push({ url, body: JSON.parse(String(init.body)) });
          return Response.json(
            { error: "unavailable", message: "Outcome unknown" },
            { status: 503 },
          );
        }
        if (init?.method === "POST") {
          writes.push({ url, body: JSON.parse(String(init.body)) });
          return Response.json({ commandId: command, state: "cancelled" });
        }
        reads.push(url);
        return Response.json({ commandId: command, state: "prepared" });
      }
      throw new Error("Unexpected request: " + url);
    }),
  );
  const render = () =>
    renderWithNamedAccounts(
      <>
        <StaffMFARecoveryPanel scope={scope} link={link} />
        <AvailabilityProbe />
      </>,
      {
        "staff-user": "QA Staff",
      },
    );
  return {
    writes,
    reads,
    render,
    setEnabled: (value: boolean) => {
      enabled = value;
    },
  };
}

it("requires explicit confirmation, uses the current Staff version and never renders account IDs", async () => {
  const f = setup();
  const { user } = f.render();
  await user.click(await screen.findByRole("button", { name: "Reset authenticator" }));
  await screen.findByLabelText("Reason for authenticator reset");
  await waitFor(() =>
    expect(screen.getByLabelText("Reason for authenticator reset")).toBeEnabled(),
  );
  expect(screen.getByRole("button", { name: "Confirm authenticator reset" })).toBeDisabled();
  await user.type(screen.getByLabelText("Reason for authenticator reset"), "Verified lost phone");
  await user.click(screen.getByLabelText("I verified the Staff member and authorize this reset."));
  await user.click(screen.getByRole("button", { name: "Confirm authenticator reset" }));
  await screen.findByText(/outcome is not confirmed/i);
  expect(f.writes).toHaveLength(1);
  expect(f.writes[0].body).toMatchObject({
    expectedStaffVersion: 8,
    expectedLinkVersion: 3,
    expectedLinkId: link.id,
    reason: "Verified lost phone",
  });
  expect(document.body.textContent).not.toContain(link.subject);
  expect(document.body.textContent).not.toContain(String(f.writes[0].body.commandId));
  expect(JSON.stringify({ ...sessionStorage })).not.toContain("Verified lost phone");
  expect(screen.queryByLabelText("Reason for authenticator reset")).not.toBeInTheDocument();
});

it("recovers the selected request after remount without replaying a reset or retaining its reason", async () => {
  const f = setup();
  const first = f.render();
  await first.user.click(await screen.findByRole("button", { name: "Reset authenticator" }));
  await screen.findByLabelText("Reason for authenticator reset");
  await waitFor(() =>
    expect(screen.getByLabelText("Reason for authenticator reset")).toBeEnabled(),
  );
  await first.user.type(
    screen.getByLabelText("Reason for authenticator reset"),
    "Verified lost phone",
  );
  await first.user.click(
    screen.getByLabelText("I verified the Staff member and authorize this reset."),
  );
  await first.user.click(screen.getByRole("button", { name: "Confirm authenticator reset" }));
  await screen.findByText(/outcome is not confirmed/i);
  const command = f.writes[0].body.commandId;
  first.unmount();
  const second = f.render();
  await second.user.click(await screen.findByRole("button", { name: "Check recovery status" }));
  await waitFor(() => expect(f.reads).toHaveLength(1));
  await screen.findByText(/This request is prepared/);
  expect(f.reads[0]).toContain(String(command));
  expect(f.writes).toHaveLength(1);
  await second.user.click(screen.getByRole("button", { name: "Cancel if still prepared" }));
  await screen.findByText(/recovery request was cancelled/i);
  expect(f.writes).toHaveLength(2);
  expect(f.writes[1].body).toEqual({});
  expect(sessionStorage.length).toBe(0);
});

it("hides new reset controls when the feature or administration is unavailable", async () => {
  const f = setup(false);
  f.render();
  await waitFor(() => expect(fetch).toHaveBeenCalled());
  expect(screen.queryByRole("button", { name: "Reset authenticator" })).not.toBeInTheDocument();
  expect(f.writes).toHaveLength(0);
});

it("preserves reconciliation when new resets are switched off", async () => {
  const f = setup();
  const first = f.render();
  await first.user.click(await screen.findByRole("button", { name: "Reset authenticator" }));
  const reason = await screen.findByLabelText("Reason for authenticator reset");
  await waitFor(() => expect(reason).toBeEnabled());
  await first.user.type(reason, "Verified lost phone");
  await first.user.click(
    screen.getByLabelText("I verified the Staff member and authorize this reset."),
  );
  await first.user.click(screen.getByRole("button", { name: "Confirm authenticator reset" }));
  await screen.findByText(/outcome is not confirmed/i);
  first.unmount();
  f.setEnabled(false);
  const next = f.render();
  await next.user.click(await screen.findByRole("button", { name: "Check recovery status" }));
  await waitFor(() => expect(f.reads).toHaveLength(1));
  await screen.findByText(/This request is prepared/);
  expect(screen.queryByRole("button", { name: "Reset authenticator" })).not.toBeInTheDocument();
  await next.user.click(screen.getByRole("button", { name: "Cancel if still prepared" }));
  await screen.findByText(/recovery request was cancelled/i);
  expect(f.writes).toHaveLength(2);
});

it("does not issue a reset when the browser cannot retain the request", async () => {
  const f = setup();
  const { user } = f.render();
  await user.click(await screen.findByRole("button", { name: "Reset authenticator" }));
  const reason = await screen.findByLabelText("Reason for authenticator reset");
  await waitFor(() => expect(reason).toBeEnabled());
  await user.type(reason, "Verified lost phone");
  await user.click(screen.getByLabelText("I verified the Staff member and authorize this reset."));
  vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
    throw new Error("blocked");
  });
  await user.click(screen.getByRole("button", { name: "Confirm authenticator reset" }));
  await screen.findByText(/could not retain the recovery request safely/i);
  expect(f.writes).toHaveLength(0);
});

it("does not offer recovery to a link manager without current tenant administration", async () => {
  const f = setup(true, false);
  f.render();
  await screen.findByText("Availability checked");
  expect(screen.queryByRole("button", { name: "Reset authenticator" })).not.toBeInTheDocument();
  expect(f.writes).toHaveLength(0);
});

it("does not load another account's retained Staff recovery request", async () => {
  const f = setup();
  const initial = f.render();
  await initial.user.click(await screen.findByRole("button", { name: "Reset authenticator" }));
  const reason = await screen.findByLabelText("Reason for authenticator reset");
  await waitFor(() => expect(reason).toBeEnabled());
  await initial.user.type(reason, "Verified lost phone");
  await initial.user.click(
    screen.getByLabelText("I verified the Staff member and authorize this reset."),
  );
  await initial.user.click(screen.getByRole("button", { name: "Confirm authenticator reset" }));
  await screen.findByText(/outcome is not confirmed/i);
  initial.unmount();
  renderWithNamedAccounts(
    <>
      <StaffMFARecoveryPanel scope={{ ...scope, subject: "another-admin" }} link={link} />
      <AvailabilityProbe />
    </>,
    { "staff-user": "QA Staff" },
  );
  await screen.findByText("Availability checked");
  expect(screen.queryByRole("button", { name: "Check recovery status" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Reset authenticator" })).not.toBeInTheDocument();
  expect(f.writes).toHaveLength(1);
  expect(f.reads).toHaveLength(0);
  expect(sessionStorage.length).toBe(1);
});

it("shows an inline error for an overlong reset reason and sends nothing", async () => {
  const f = setup();
  const { user } = f.render();
  await user.click(await screen.findByRole("button", { name: "Reset authenticator" }));
  const reason = await screen.findByLabelText("Reason for authenticator reset");
  await waitFor(() => expect(reason).toBeEnabled());
  await user.click(reason);
  await user.paste("x".repeat(501));
  await user.click(screen.getByLabelText("I verified the Staff member and authorize this reset."));
  await user.click(screen.getByRole("button", { name: "Confirm authenticator reset" }));
  expect(await screen.findByText("Use up to 500 characters")).toBeInTheDocument();
  expect(f.writes).toHaveLength(0);
});
