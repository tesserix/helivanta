import { afterEach, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { NavigationGuardProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import { GovernanceAssignmentPreparation } from "./governance-assignment-preparation";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-owner",
  hospitalId: "22222222-2222-4222-8222-222222222222",
};
const target = {
  hospital_id: scope.hospitalId,
  staff_id: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa",
  subject: "synthetic-colleague",
  name: "Synthetic colleague",
  email: "colleague@example.invalid",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

it("uses a qualified named colleague and retains the exact proposal after a lost response", async () => {
  const writes: { url: string; body: string }[] = [];
  const onSaved = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("assignment-target-choices")) return Response.json(page([target]));
      writes.push({ url, body: String(init?.body) });
      if (writes.length === 1) return Response.json({ error: "unavailable" }, { status: 503 });
      const input = JSON.parse(String(init?.body));
      return Response.json({
        id: url.split("/").at(-1),
        input,
        scope_kind: "hospital",
        proposed_by: scope.subject,
        status: "pending_scope",
        version: 1,
        created_at: "2026-11-01T00:00:00Z",
        updated_at: "2026-11-01T00:00:00Z",
      });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceAssignmentPreparation
        scope={scope}
        family="leave"
        templateKeys={["workforce.leave_reviewer.v1"]}
        onClose={vi.fn()}
        onSaved={onSaved}
        onAccessLost={vi.fn()}
      />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Choose Synthetic colleague" }));
  expect(screen.getByText(target.email)).toBeVisible();
  expect(screen.queryByLabelText(/account id/i)).not.toBeInTheDocument();
  await user.type(
    screen.getByLabelText("Assignment reason"),
    "Synthetic leave review responsibility",
  );
  await user.click(screen.getByRole("button", { name: "Submit proposal" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("may have been recorded");
  expect(screen.getByLabelText("Assignment reason")).toBeDisabled();
  expect(screen.getByRole("button", { name: "Close preparation" })).toBeDisabled();
  vi.spyOn(Date, "now").mockReturnValue(Date.now() + 3 * 86400000);
  await user.click(screen.getByRole("button", { name: "Retry same request" }));
  await waitFor(() => expect(onSaved).toHaveBeenCalledOnce());
  expect(writes).toHaveLength(2);
  expect(writes[0]).toEqual(writes[1]);
  expect(JSON.parse(writes[0]!.body)).toMatchObject({
    hospital_id: scope.hospitalId,
    subject: target.subject,
    template_key: "workforce.leave_reviewer.v1",
  });
  expect(writes[0]!.url).toContain(`/iam/hospitals/${scope.hospitalId}/assignment-proposals/`);
});

it("withholds private choices and notifies the workspace when authority is lost", async () => {
  const onAccessLost = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ error: "not_found" }, { status: 404 })),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceAssignmentPreparation
        scope={scope}
        family="leave"
        templateKeys={["workforce.leave_reviewer.v1"]}
        onClose={vi.fn()}
        onSaved={vi.fn()}
        onAccessLost={onAccessLost}
      />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent("Staff access changed");
  expect(screen.queryByText(target.email)).not.toBeInTheDocument();
  await waitFor(() => expect(onAccessLost).toHaveBeenCalledOnce());
});

it("uses current named department choices for a department-only template", async () => {
  const department = {
    id: "019a55c0-1111-7111-8111-111111111112",
    hospital_id: scope.hospitalId,
    revision_id: "019a55c0-1111-7111-8111-111111111113",
    version: 1,
    code: "PHARMACY",
    name: "Synthetic pharmacy department",
    kind: "department",
    status: "active",
    effective_at: "2026-11-01T00:00:00Z",
    created_at: "2026-11-01T00:00:00Z",
  };
  const paths: string[] = [];
  let input: Record<string, unknown> | undefined;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      paths.push(url);
      if (url.includes("assignment-target-choices")) return Response.json(page([target]));
      if (url.includes("assignment-department-choices")) return Response.json(page([department]));
      input = JSON.parse(String(init?.body));
      return Response.json({ error: "source_changed" }, { status: 409 });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceAssignmentPreparation
        scope={scope}
        family="posting"
        templateKeys={["workforce.department_posting_reviewer.v1"]}
        onClose={vi.fn()}
        onSaved={vi.fn()}
        onAccessLost={vi.fn()}
      />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Choose Synthetic colleague" }));
  await user.click(await screen.findByLabelText("Department"));
  await user.click(await screen.findByRole("option", { name: department.name }));
  await user.type(
    screen.getByLabelText("Assignment reason"),
    "Synthetic department responsibility",
  );
  await user.click(screen.getByRole("button", { name: "Submit proposal" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("source changed");
  expect(input).toMatchObject({
    hospital_id: scope.hospitalId,
    department_id: department.id,
    subject: target.subject,
    template_key: "workforce.department_posting_reviewer.v1",
  });
  expect(
    paths.every(
      (url) =>
        url.includes("assignment-target-choices") ||
        url.includes("assignment-department-choices") ||
        url.includes(`/iam/hospitals/${scope.hospitalId}/assignment-proposals/`),
    ),
  ).toBe(true);
  expect(screen.queryByLabelText(/department id/i)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Retry same request" })).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Retry same request" })).toBeDisabled();
});

it("removes selected account details after the server denies a proposal", async () => {
  const onAccessLost = vi.fn();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url.includes("assignment-target-choices")
        ? Response.json(page([target]))
        : Response.json({ error: "forbidden" }, { status: 403 }),
    ),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceAssignmentPreparation
        scope={scope}
        family="leave"
        templateKeys={["workforce.leave_reviewer.v1"]}
        onClose={vi.fn()}
        onSaved={vi.fn()}
        onAccessLost={onAccessLost}
      />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Choose Synthetic colleague" }));
  await user.type(screen.getByLabelText("Assignment reason"), "Synthetic denied proposal");
  await user.click(screen.getByRole("button", { name: "Submit proposal" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Staff access changed");
  expect(screen.queryByText(target.email)).not.toBeInTheDocument();
  await waitFor(() => expect(onAccessLost).toHaveBeenCalledOnce());
});
