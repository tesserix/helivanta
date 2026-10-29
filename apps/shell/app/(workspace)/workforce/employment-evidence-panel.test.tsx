import { NavigationGuardProvider } from "@helivanta/ui";
import "@/test/select";
import { screen, within } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { EmploymentEvidencePanel } from "./employment-evidence-panel";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-admin",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
  periodId: "019a0000-0000-7000-8000-000000000001",
  periodVersion: 1,
};
const file = {
  id: "44444444-4444-4444-8444-444444444444",
  hospital_id: scope.hospitalId,
  staff_id: scope.staffId,
  record_id: scope.staffId,
  record_kind: "enrollment",
  record_version: 1,
  category: "employment_evidence",
  filename: "SYN-contract.png",
  content_type: "image/png",
  size: 3,
  sha256: "a".repeat(64),
  status: "available",
  scan_attempt: 1,
  uploaded_at: "2026-10-29T00:00:00Z",
  scanned_at: "2026-10-29T00:00:00Z",
  failure_code: null,
  expires_at: "2026-10-29T00:00:00Z",
  created_at: "2026-10-29T00:00:00Z",
  updated_at: "2026-10-29T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());
it("attaches only an available employment file and retries the exact command", async () => {
  const writes: Record<string, unknown>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        const input = JSON.parse(String(init.body));
        writes.push(input);
        if (writes.length === 1)
          return Response.json(
            { error: "temporarily_unavailable", message: "Retry same command" },
            { status: 503 },
          );
        return Response.json({
          hospital_id: scope.hospitalId,
          staff_id: scope.staffId,
          filename: file.filename,
          content_type: file.content_type,
          size: file.size,
          sha256: file.sha256,
          scan_attempt: file.scan_attempt,
          scanned_at: file.scanned_at,
          created_at: file.created_at,
          updated_at: file.updated_at,
          id: url.split("/").at(-1),
          revision_id: input.command_id,
          period_id: scope.periodId,
          period_revision_id: scope.periodId,
          period_version: 1,
          version: 1,
          status: "attached",
          staff_version: 1,
          employment_epoch: 1,
          document_id: file.id,
          document_record_version: 1,
          source_generation: "1",
          released_generation: "2",
          proposed_by_subject: scope.subject,
          actor_subject: scope.subject,
          reason: input.reason,
        });
      }
      return Response.json({
        data: url.endsWith("/documents") ? [file] : [],
        page: { has_more: false },
      });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <EmploymentEvidencePanel scope={scope} staffVersion={1} canAttach onClose={() => {}} />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Attach scanned employment file" }));
  const form = screen.getByRole("form", { name: "Attach employment evidence" });
  await user.click(within(form).getByRole("button", { name: "Attach evidence" }));
  expect(writes).toHaveLength(0);
  await user.click(within(form).getByLabelText("Scanned employment file"));
  await user.click(await screen.findByRole("option", { name: "SYN-contract.png" }));
  await user.type(within(form).getByLabelText("Reason"), "Synthetic appointment evidence");
  await user.click(within(form).getByRole("button", { name: "Attach evidence" }));
  await screen.findByRole("alert");
  await user.click(within(form).getByRole("button", { name: "Retry same evidence command" }));
  await screen.findByText("Employment evidence saved.");
  expect(writes).toHaveLength(2);
  expect(writes[1]).toEqual(writes[0]);
  expect(writes[0]).toMatchObject({
    document_id: file.id,
    reason: "Synthetic appointment evidence",
  });
});

it("keeps each evidence command field associated with its own label", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Response.json({ data: [], page: { has_more: false } })),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <EmploymentEvidencePanel scope={scope} staffVersion={1} canAttach onClose={() => {}} />
      <EmploymentEvidencePanel
        scope={{ ...scope, periodVersion: 2 }}
        staffVersion={1}
        canAttach
        onClose={() => {}}
      />
    </NavigationGuardProvider>,
  );
  const buttons = await screen.findAllByRole("button", { name: "Attach scanned employment file" });
  for (const button of buttons) await user.click(button);
  expect(screen.getAllByLabelText("Reason")).toHaveLength(2);
  expect(screen.getAllByLabelText("Scanned employment file")).toHaveLength(2);
});
