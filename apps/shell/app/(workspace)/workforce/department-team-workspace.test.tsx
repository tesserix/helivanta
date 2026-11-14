import { useState } from "react";
import { act, screen, waitFor } from "@testing-library/react";
import { NavigationGuardProvider, useLeaveGuard } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import {
  beginTenantTransition,
  cancelTenantTransition,
  type EnrollmentRequestScope,
} from "@helivanta/api";
import { DepartmentTeamWorkspace } from "./department-team-workspace";

vi.mock("./posting-workspace", () => ({
  StaffPostingWorkspace: function Preparation({ scope }: { scope: EnrollmentRequestScope }) {
    const [dirty, setDirty] = useState(false);
    useLeaveGuard(dirty, "department placement");
    return (
      <>
        <p>
          Protected preparation: {scope.tenantId} / {scope.subject}
        </p>
        <button onClick={() => setDirty(true)}>Edit placement</button>
      </>
    );
  },
}));

vi.mock("./posting-review-workspace", () => ({
  StaffPostingReviewWorkspace: ({
    onBusyChanged,
    onContextChanged,
  }: {
    onBusyChanged?: (busy: boolean) => void;
    onContextChanged: () => void;
  }) => (
    <>
      <p>Protected responsibility review</p>
      <button onClick={() => onBusyChanged?.(true)}>Retain review command</button>
      <button onClick={() => onBusyChanged?.(false)}>Finish review command</button>
      <button onClick={onContextChanged}>Withdraw review access</button>
    </>
  ),
}));

vi.mock("./posting-cutoff-workspace", () => ({
  StaffPostingCutoffWorkspace: () => <p>Protected responsibility ending</p>,
}));

afterEach(() => {
  act(() => cancelTenantTransition());
  vi.unstubAllGlobals();
});

function renderTeam() {
  return renderWithProviders(
    <NavigationGuardProvider>
      <DepartmentTeamWorkspace />
    </NavigationGuardProvider>,
  );
}

function account() {
  const fetch = vi.fn(async (url: string) => {
    if (url !== "/api/v1/iam/me/permissions") throw new Error("Unexpected account discovery");
    return Response.json({
      tenant_id: "synthetic-hospital-tenant",
      subject: "synthetic-team-admin",
      data: [],
    });
  });
  vi.stubGlobal("fetch", fetch);
  return fetch;
}

it("opens existing protected preparation with the verified account and no private directory read", async () => {
  const fetch = account();
  const { user } = renderTeam();

  await user.click(await screen.findByRole("button", { name: "Prepare staff placements" }));

  expect(
    screen.getByText("Protected preparation: synthetic-hospital-tenant / synthetic-team-admin"),
  ).toBeInTheDocument();
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(fetch.mock.calls[0][0]).toBe("/api/v1/iam/me/permissions");
});

it("protects unsaved placements before changing to responsibility review", async () => {
  account();
  const { user } = renderTeam();
  await user.click(await screen.findByRole("button", { name: "Prepare staff placements" }));
  await user.click(screen.getByRole("button", { name: "Edit placement" }));
  await user.click(screen.getByRole("button", { name: "Review heads and supervisors" }));
  await screen.findByRole("dialog", { name: "Discard changes to department placement?" });
  expect(screen.queryByText("Protected responsibility review")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Keep editing" }));
  expect(screen.getByRole("button", { name: "Edit placement" })).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Review heads and supervisors" }));
  await user.click(screen.getByRole("button", { name: "Discard" }));
  await screen.findByText("Protected responsibility review");
});

it("holds task changes while a review command is unresolved", async () => {
  account();
  const { user } = renderTeam();
  await user.click(await screen.findByRole("button", { name: "Review heads and supervisors" }));
  await user.click(screen.getByRole("button", { name: "Retain review command" }));
  expect(screen.getByRole("button", { name: "End responsibilities" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Prepare staff placements" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Finish review command" }));
  await user.click(screen.getByRole("button", { name: "End responsibilities" }));
  await screen.findByText("Protected responsibility ending");
});

it("clears the selected task during a tenant switch and rechecks identity on cancellation", async () => {
  const requests = account();
  const { user } = renderTeam();
  await user.click(await screen.findByRole("button", { name: "Review heads and supervisors" }));
  act(() => beginTenantTransition());
  expect(screen.getByRole("status")).toHaveTextContent("Switching department team workspace");
  expect(screen.queryByText("Protected responsibility review")).not.toBeInTheDocument();
  act(() => cancelTenantTransition());
  await waitFor(() => expect(requests).toHaveBeenCalledTimes(2));
  await screen.findByRole("button", { name: "Review heads and supervisors" });
  expect(screen.queryByText("Protected responsibility review")).not.toBeInTheDocument();
});

it("removes all team tasks when protected workflow admission is withdrawn", async () => {
  account();
  const { user } = renderTeam();
  await user.click(await screen.findByRole("button", { name: "Review heads and supervisors" }));
  await user.click(screen.getByRole("button", { name: "Withdraw review access" }));
  expect(screen.queryByRole("group", { name: "Department team tasks" })).not.toBeInTheDocument();
  expect(screen.queryByText("Protected responsibility review")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Reopen department teams" }));
  await screen.findByRole("group", { name: "Department team tasks" });
});

it.each([401, 403, 503])("does not expose team tasks when identity returns %i", async (status) => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({ error: "unavailable", message: "Synthetic refusal" }, { status }),
    ),
  );
  renderTeam();
  await screen.findByRole("alert");
  expect(screen.queryByRole("group", { name: "Department team tasks" })).not.toBeInTheDocument();
  if (status < 500)
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  else expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
});
