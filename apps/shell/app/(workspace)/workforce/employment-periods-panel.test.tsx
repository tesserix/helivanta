import { NavigationGuardProvider } from "@helivanta/ui";
import "@/test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { StaffEmploymentPeriods } from "./employment-periods-panel";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-admin",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
};
afterEach(() => vi.unstubAllGlobals());
it("requires explicit contract terms and preserves the exact retry command", async () => {
  const writes: Record<string, unknown>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body));
        writes.push(body);
        if (writes.length === 1)
          return Response.json(
            { error: "unavailable", message: "Synthetic uncertain result" },
            { status: 503 },
          );
        const { command_id, expected_version, ...terms } = body;
        return Response.json({
          ...terms,
          id: url.split("/").at(-1),
          revision_id: command_id,
          version: expected_version + 1,
          status: "draft",
          hospital_id: scope.hospitalId,
          staff_id: scope.staffId,
          proposed_by_subject: scope.subject,
          actor_subject: scope.subject,
          created_at: "2026-10-29T00:00:00Z",
          updated_at: "2026-10-29T00:00:00Z",
        });
      }
      if (url.endsWith("employment-source"))
        return Response.json({
          hospital_id: scope.hospitalId,
          staff_id: scope.staffId,
          staff_version: 1,
          employment_epoch: 1,
        });
      return Response.json({ data: [], page: { has_more: false, next_cursor: null } });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <StaffEmploymentPeriods
        staffLabel="Synthetic Contractor QA"
        scope={scope}
        onClose={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Prepare employment terms" }));
  const form = screen.getByRole("form", { name: "Employment terms draft" });
  await user.click(within(form).getByRole("button", { name: "Save draft" }));
  expect(writes).toHaveLength(0);

  await user.click(within(form).getByLabelText("Duration"));
  await user.click(screen.getByRole("option", { name: "Ongoing" }));
  await user.type(within(form).getByLabelText("Effective start"), "2026-11-01T09:00");
  await user.type(
    within(form).getByLabelText("Appointment or contract reference"),
    "SYN-APPOINTMENT",
  );
  await user.type(within(form).getByLabelText("Reason"), "Synthetic appointment intake");
  await user.click(within(form).getByRole("button", { name: "Save draft" }));
  await screen.findByRole("alert");
  expect(writes).toHaveLength(1);
  await user.click(within(form).getByRole("button", { name: "Retry same draft" }));
  await screen.findByText("Employment terms draft saved.");
  expect(writes).toHaveLength(2);
  expect(writes[1]).toEqual(writes[0]);
  expect(writes[0]).toMatchObject({ duration: "ongoing", staff_version: 1, employment_epoch: 1 });
});

it("keeps unsaved terms until the user explicitly discards them", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      Response.json(
        url.endsWith("employment-source")
          ? {
              hospital_id: scope.hospitalId,
              staff_id: scope.staffId,
              staff_version: 1,
              employment_epoch: 1,
            }
          : { data: [], page: { has_more: false, next_cursor: null } },
      ),
    ),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <StaffEmploymentPeriods
        staffLabel="Synthetic Contractor QA"
        scope={scope}
        onClose={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Prepare employment terms" }));
  const form = screen.getByRole("form", { name: "Employment terms draft" });
  await user.type(within(form).getByLabelText("Reason"), "Synthetic unsaved terms");
  await user.click(within(form).getByRole("button", { name: "Close draft form" }));
  const dialog = await screen.findByRole("dialog");
  await user.click(within(dialog).getByRole("button", { name: "Keep editing" }));
  expect(within(form).getByLabelText("Reason")).toHaveValue("Synthetic unsaved terms");
  await user.click(within(form).getByRole("button", { name: "Close draft form" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Discard" }),
  );
  expect(screen.queryByRole("form", { name: "Employment terms draft" })).not.toBeInTheDocument();
});

it("withdraws a draft with a required reason and the exact retry command", async () => {
  const draft = {
    id: "019a0000-0000-7000-8000-000000000001",
    revision_id: "019a0000-0000-7000-8000-000000000002",
    hospital_id: scope.hospitalId,
    staff_id: scope.staffId,
    version: 1,
    status: "draft",
    staff_version: 1,
    employment_epoch: 1,
    engagement_kind: "employee",
    duration: "ongoing",
    starts_at: "2026-11-01T00:00:00Z",
    contracting_party: "",
    sponsor_id: "",
    reason: "Synthetic appointment",
    evidence_reference: "SYN-WITHDRAW-UI",
    proposed_by_subject: scope.subject,
    actor_subject: scope.subject,
    created_at: "2026-10-29T00:00:00Z",
    updated_at: "2026-10-29T00:00:00Z",
  };
  let current = draft;
  const writes: Record<string, unknown>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "POST") {
        const body = JSON.parse(String(init.body));
        writes.push(body);
        if (writes.length === 1) return Response.json({ error: "unavailable" }, { status: 503 });
        current = {
          ...draft,
          status: "withdrawn",
          version: 2,
          revision_id: body.command_id,
          reason: body.reason,
        };
        return Response.json(current);
      }
      if (url.endsWith("employment-source"))
        return Response.json({
          hospital_id: scope.hospitalId,
          staff_id: scope.staffId,
          staff_version: 1,
          employment_epoch: 1,
        });
      return Response.json({ data: [current], page: { has_more: false, next_cursor: null } });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <StaffEmploymentPeriods staffLabel="Synthetic QA" scope={scope} onClose={() => {}} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Withdraw draft" }));
  const form = screen.getByRole("form", { name: "Withdraw employment draft" });
  expect(screen.getByRole("button", { name: "Prepare employment terms" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Close employment terms" })).toBeDisabled();
  await user.click(within(form).getByRole("button", { name: "Confirm withdrawal" }));
  expect(writes).toHaveLength(0);
  expect(
    await within(form).findByText("Enter a withdrawal reason of 1 to 500 characters."),
  ).toBeInTheDocument();
  await user.type(
    within(form).getByLabelText("Withdrawal reason"),
    "Synthetic preparation cancelled",
  );
  await user.click(within(form).getByRole("button", { name: "Confirm withdrawal" }));
  await screen.findByText(
    "Could not confirm withdrawal. Retry the same command to recover its result.",
  );
  expect(within(form).getByLabelText("Withdrawal reason")).toBeDisabled();
  await user.click(within(form).getByRole("button", { name: "Retry same withdrawal" }));
  await screen.findByText("Employment draft withdrawn. Earlier revisions are retained.");
  expect(writes).toHaveLength(2);
  expect(writes[1]).toEqual(writes[0]);
  expect(screen.queryByRole("button", { name: "Edit terms" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Withdraw draft" })).toBeNull();
  expect(screen.getByText("Synthetic preparation cancelled")).toBeInTheDocument();
});

it("uses scoped intake for source, lifecycle lists and sponsor choices without losing projections", async () => {
  const reads: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      reads.push(url);
      const pathname = new URL(url, "https://example.invalid").pathname;
      return Response.json(
        pathname.endsWith("/employment-source")
          ? {
              hospital_id: scope.hospitalId,
              staff_id: scope.staffId,
              staff_version: 1,
              employment_epoch: 1,
            }
          : { data: [], page: { has_more: false, next_cursor: null } },
      );
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <StaffEmploymentPeriods
        staffLabel="Synthetic Scoped HR"
        scope={{ ...scope, hrIntake: true }}
        onClose={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Prepare employment terms" }));
  expect(await screen.findByRole("form", { name: "Employment terms draft" })).toBeInTheDocument();
  expect(
    reads.some((url) => new URL(url, "https://example.invalid").pathname.endsWith("/staff")),
  ).toBe(true);
  for (const url of reads)
    expect(new URL(url, "https://example.invalid").searchParams.get("hr_intake")).toBe("v1");
  const periods = reads.find((url) =>
    new URL(url, "https://example.invalid").pathname.endsWith("/employment-periods"),
  );
  expect(new URL(periods!, "https://example.invalid").searchParams.get("lifecycle")).toBe(
    "withdrawal-v1",
  );
});

it("clears an employment draft and notifies the workspace after a known access cutoff", async () => {
  const changed = vi.fn();
  const writes: string[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes.push(String(init.body));
        return Response.json(
          { error: "forbidden", message: "Synthetic actor link revoked" },
          { status: 403 },
        );
      }
      return Response.json(
        new URL(url, "https://example.invalid").pathname.endsWith("/employment-source")
          ? {
              hospital_id: scope.hospitalId,
              staff_id: scope.staffId,
              staff_version: 1,
              employment_epoch: 1,
            }
          : { data: [], page: { has_more: false, next_cursor: null } },
      );
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <StaffEmploymentPeriods
        staffLabel="Synthetic Scoped HR"
        scope={{ ...scope, hrIntake: true }}
        onContextChanged={changed}
        onClose={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Prepare employment terms" }));
  const form = screen.getByRole("form", { name: "Employment terms draft" });
  await user.type(
    within(form).getByLabelText("Reason"),
    "Synthetic private retained employment reason",
  );
  await user.click(within(form).getByLabelText("Duration"));
  await user.click(screen.getByRole("option", { name: "Ongoing" }));
  await user.type(within(form).getByLabelText("Effective start"), "2026-11-01T09:00");
  await user.type(
    within(form).getByLabelText("Appointment or contract reference"),
    "SYN-PRIVATE-INTAKE",
  );
  await user.click(within(form).getByRole("button", { name: "Save draft" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(
    await screen.findByText("Staff access changed. Reopen Staff to verify your current workspace."),
  ).toBeInTheDocument();
  expect(
    screen.queryByDisplayValue("Synthetic private retained employment reason"),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("form", { name: "Employment terms draft" })).not.toBeInTheDocument();
  await waitFor(() => expect(changed).toHaveBeenCalled());
});

it("retains an unsaved draft through a temporary source outage and requires a fresh recheck", async () => {
  let checks = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (new URL(url, "https://example.invalid").pathname.endsWith("/employment-source")) {
        checks++;
        return checks === 2
          ? Response.json(
              {
                error: "scoped_authorization_unavailable",
                message: "Synthetic source unavailable",
              },
              { status: 503 },
            )
          : Response.json({
              hospital_id: scope.hospitalId,
              staff_id: scope.staffId,
              staff_version: 1,
              employment_epoch: 1,
            });
      }
      return Response.json({ data: [], page: { has_more: false, next_cursor: null } });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <StaffEmploymentPeriods
        staffLabel="Synthetic Scoped HR"
        scope={{ ...scope, hrIntake: true }}
        onClose={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Prepare employment terms" }));
  const form = screen.getByRole("form", { name: "Employment terms draft" });
  await user.type(within(form).getByLabelText("Reason"), "Synthetic retained source outage draft");
  await user.click(screen.getByRole("button", { name: "Recheck Staff details" }));
  expect(
    await screen.findByText(
      "Current Staff details could not be verified. Recheck before preparing a new command.",
    ),
  ).toBeInTheDocument();
  expect(within(form).getByLabelText("Reason")).toHaveValue(
    "Synthetic retained source outage draft",
  );
  expect(within(form).getByRole("button", { name: "Save draft" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Recheck Staff details" }));
  await waitFor(() =>
    expect(within(form).getByRole("button", { name: "Save draft" })).not.toBeDisabled(),
  );
  expect(within(form).getByLabelText("Reason")).toHaveValue(
    "Synthetic retained source outage draft",
  );
  expect(checks).toBe(3);
});
