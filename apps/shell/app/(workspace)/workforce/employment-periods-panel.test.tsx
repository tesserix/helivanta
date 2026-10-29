import "@/test/select";
import { screen, within } from "@testing-library/react";
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
    <StaffEmploymentPeriods
      staffLabel="Synthetic Contractor QA"
      scope={scope}
      onClose={() => {}}
    />,
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
