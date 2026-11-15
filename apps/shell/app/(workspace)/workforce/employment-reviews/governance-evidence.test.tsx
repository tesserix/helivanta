import "@/test/select";
import { screen, within } from "@testing-library/react";
import { NavigationGuardProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import { GovernanceEmploymentEvidence } from "./governance-evidence";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-native-preparer",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
  staffVersion: 3,
  employmentEpoch: 2,
};
const terms = {
  id: "019a0000-0000-7000-8000-000000000011",
  revision_id: "019a0000-0000-7000-8000-000000000012",
  version: 1,
  status: "draft" as const,
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  staff_version: 3,
  employment_epoch: 2,
  engagement_kind: "employee" as const,
  duration: "ongoing" as const,
  starts_at: "2026-11-01T00:00:00Z",
  contracting_party: "",
  sponsor_id: "",
  reason: "Synthetic terms",
  evidence_reference: "SYN-TERMS",
  proposed_by_subject: scope.subject,
  actor_subject: scope.subject,
  created_at: "2026-11-01T00:00:00Z",
  updated_at: "2026-11-01T00:00:00Z",
};
const file = {
  id: "019a0000-0000-7000-8000-000000000023",
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  record_id: scope.staffId,
  record_kind: "enrollment",
  record_version: 3,
  category: "employment_evidence",
  filename: "synthetic-contract.png",
  content_type: "image/png",
  size: 1,
  sha256: "a".repeat(64),
  status: "available",
  scan_attempt: 1,
  uploaded_at: "2026-11-01T00:01:00Z",
  scanned_at: "2026-11-01T00:02:00Z",
  failure_code: null,
  created_at: "2026-11-01T00:00:00Z",
  updated_at: "2026-11-01T00:02:00Z",
  expires_at: "2026-11-01T00:30:00Z",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
it("attaches a released file to the reread exact terms revision and replays uncertain saves", async () => {
  const writes: { url: string; body: Record<string, unknown> }[] = [];
  let binding: Record<string, unknown> | null = null;
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (new URL(url, "https://synthetic.invalid").pathname.endsWith(terms.id))
      return Response.json(terms);
    if (url.includes("employment-preparation-documents")) return Response.json(page([file]));
    if (init?.method === "PUT") {
      const input = JSON.parse(String(init.body));
      writes.push({ url, body: input });
      if (writes.length === 1)
        return Response.json(
          { error: "unavailable", message: "Uncertain binding" },
          { status: 503 },
        );
      binding = {
        id: url.split("?")[0].split("/").at(-1),
        revision_id: input.command_id,
        hospital_id: scope.hospitalId,
        staff_id: scope.staffId,
        period_id: terms.id,
        period_revision_id: terms.revision_id,
        period_version: 1,
        staff_version: 3,
        employment_epoch: 2,
        version: 1,
        status: "attached",
        document_id: file.id,
        document_record_version: 3,
        filename: file.filename,
        content_type: file.content_type,
        size: file.size,
        sha256: file.sha256,
        source_generation: "11",
        released_generation: "12",
        scan_attempt: 1,
        scanned_at: file.scanned_at,
        proposed_by_subject: scope.subject,
        actor_subject: scope.subject,
        reason: input.reason,
        created_at: "2026-11-01T00:03:00Z",
        updated_at: "2026-11-01T00:03:00Z",
      };
      return Response.json(binding);
    }
    if (url.includes("/evidence?")) return Response.json(page(binding ? [binding] : []));
    throw new Error("Unexpected legacy request: " + url);
  });
  vi.stubGlobal("fetch", fetcher);
  const busy = vi.fn();
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentEvidence
        scope={scope}
        terms={terms}
        onBusy={busy}
        onClose={() => {}}
        onAccessLost={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Attach scanned employment file" }));
  const form = screen.getByRole("form", { name: "Attach exact employment evidence" });
  await user.click(within(form).getByLabelText("Scanned employment file"));
  await user.click(await screen.findByRole("option", { name: file.filename }));
  await user.type(within(form).getByLabelText("Reason"), "Synthetic exact terms binding");
  await user.click(within(form).getByRole("button", { name: "Attach evidence" }));
  await screen.findByText(
    "Could not confirm the binding. Retry the same command to recover its result.",
  );
  expect(screen.getByRole("button", { name: "Close terms evidence" })).toBeDisabled();
  await user.click(within(form).getByRole("button", { name: "Retry same evidence command" }));
  await screen.findByText("Employment evidence saved for this terms revision.");
  expect(writes).toHaveLength(2);
  expect(writes[1]).toEqual(writes[0]);
  expect(writes[0].url).toContain(
    `/employment-preparation-periods/${terms.id}/revisions/1/evidence/`,
  );
  expect(writes[0].url).toContain("staff_version=3&employment_epoch=2");
  expect(writes[0].body.document_id).toBe(file.id);
  expect(busy).toHaveBeenCalledWith(true);
  for (const [url, init] of fetcher.mock.calls) {
    expect(url).not.toContain("hr_intake");
    expect(new Headers(init?.headers).get("X-Helivanta-Expected-Subject")).toBe(scope.subject);
  }
});
it("blocks authoring after a concurrent terms change before loading evidence", async () => {
  const fetcher = vi.fn(async () => Response.json({ ...terms, version: 2, status: "withdrawn" }));
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentEvidence
        scope={scope}
        terms={terms}
        onBusy={() => {}}
        onClose={() => {}}
        onAccessLost={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await screen.findByText("These terms changed. Close evidence and select the current revision.");
  expect(
    screen.queryByRole("button", { name: "Attach scanned employment file" }),
  ).not.toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledTimes(1);
});
it("removes the workspace after native evidence access is denied without legacy retry", async () => {
  const lost = vi.fn();
  const fetcher = vi.fn(async () =>
    Response.json({ error: "forbidden", message: "Denied" }, { status: 403 }),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentEvidence
        scope={scope}
        terms={terms}
        onBusy={() => {}}
        onClose={() => {}}
        onAccessLost={lost}
      />
    </NavigationGuardProvider>,
  );
  await screen.findByRole("alert");
  expect(lost).toHaveBeenCalledTimes(1);
  expect(fetcher).toHaveBeenCalledTimes(1);
});

it("retains an uncertain withdrawal command and preserves immutable evidence history", async () => {
  const original = {
    id: "019a0000-0000-7000-8000-000000000041",
    revision_id: "019a0000-0000-7000-8000-000000000042",
    hospital_id: scope.hospitalId,
    staff_id: scope.staffId,
    period_id: terms.id,
    period_revision_id: terms.revision_id,
    period_version: 1,
    staff_version: 3,
    employment_epoch: 2,
    version: 1,
    status: "attached",
    document_id: file.id,
    document_record_version: 3,
    filename: file.filename,
    content_type: file.content_type,
    size: file.size,
    sha256: file.sha256,
    source_generation: "11",
    released_generation: "12",
    scan_attempt: 1,
    scanned_at: file.scanned_at,
    proposed_by_subject: scope.subject,
    actor_subject: scope.subject,
    reason: "Synthetic original evidence",
    created_at: "2026-11-01T00:03:00Z",
    updated_at: "2026-11-01T00:03:00Z",
  };
  let row = original;
  const writes: Record<string, unknown>[] = [];
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (new URL(url, "https://synthetic.invalid").pathname.endsWith(terms.id))
      return Response.json(terms);
    if (init?.method === "POST") {
      const input = JSON.parse(String(init.body));
      writes.push(input);
      if (writes.length === 1)
        return Response.json(
          { error: "unavailable", message: "Uncertain withdrawal" },
          { status: 503 },
        );
      row = {
        ...original,
        status: "withdrawn",
        version: 2,
        revision_id: input.command_id,
        reason: input.reason,
        updated_at: "2026-11-01T00:04:00Z",
      };
      return Response.json(row);
    }
    if (url.includes("/history?")) return Response.json(page([row, original]));
    if (url.includes("/evidence?")) return Response.json(page([row]));
    throw new Error("Unexpected document or legacy request: " + url);
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentEvidence
        scope={scope}
        terms={terms}
        onBusy={() => {}}
        onClose={() => {}}
        onAccessLost={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Withdraw evidence" }));
  const form = screen.getByRole("form", { name: "Withdraw exact employment evidence" });
  await user.type(
    within(form).getByLabelText("Reason"),
    "Synthetic withdrawal preserves the original file",
  );
  await user.click(within(form).getByRole("button", { name: "Confirm withdrawal" }));
  await user.click(
    await within(form).findByRole("button", { name: "Retry same evidence command" }),
  );
  await screen.findByText("Employment evidence saved for this terms revision.");
  expect(writes).toHaveLength(2);
  expect(writes[1]).toEqual(writes[0]);
  expect(writes[0].expected_version).toBe(1);
  expect(screen.queryByRole("button", { name: "Download evidence" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Evidence history" }));
  await screen.findByText(/Revision 2 · Withdrawn/);
  await screen.findByText(/Revision 1 · Attached/);
  expect(fetcher.mock.calls.some(([url]) => url.includes("employment-preparation-documents"))).toBe(
    false,
  );
});

it("keeps historical evidence readable after Staff basis changes without allowing new binding or submission", async () => {
  const fetcher = vi.fn(async (url: string) =>
    Response.json(
      new URL(url, "https://synthetic.invalid").pathname.endsWith(terms.id) ? terms : page([]),
    ),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(
    <NavigationGuardProvider>
      <GovernanceEmploymentEvidence
        scope={{ ...scope, staffVersion: 4, employmentEpoch: 3 }}
        terms={terms}
        onBusy={() => {}}
        onClose={() => {}}
        onAccessLost={() => {}}
      />
    </NavigationGuardProvider>,
  );
  await screen.findByText(
    "The Staff employment basis changed. Existing evidence remains available; prepare current terms before attaching or submitting new evidence.",
  );
  await screen.findByText("No evidence bound to this terms revision.");
  expect(
    screen.queryByRole("button", { name: "Attach scanned employment file" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Prepare independent employment review" }),
  ).not.toBeInTheDocument();
  expect(
    fetcher.mock.calls.some(([url]) => url.includes("staff_version=4&employment_epoch=3")),
  ).toBe(true);
});
