import "@/test/select";
import { screen, within } from "@testing-library/react";
import { NavigationGuardProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import Workforce from "./page";

const tenant = "11111111-1111-4111-8111-111111111111";
const hospital = "22222222-2222-4222-8222-222222222222";
const first = "33333333-3333-4333-8333-333333333333";
const second = "44444444-4444-4444-8444-444444444444";
afterEach(() => vi.unstubAllGlobals());
it("confirms before switching the selected Staff record with unsaved employment terms", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/iam/me/permissions"))
        return Response.json({
          tenant_id: tenant,
          subject: "synthetic-admin",
          data: ["workforce.enrollment.manage"],
        });
      if (url.includes("employment-source"))
        return Response.json({
          hospital_id: hospital,
          staff_id: url.includes(first) ? first : second,
          staff_version: 1,
          employment_epoch: 1,
        });
      if (url.includes("employment-periods"))
        return Response.json({ data: [], page: { has_more: false, next_cursor: null } });
      const data = url.endsWith("/staff")
        ? [first, second].map((id, index) => ({
            id,
            hospital_id: hospital,
            employee_code: `SYN-${index}`,
            display_name: `Synthetic Staff ${index === 0 ? "A" : "B"}`,
            employment_status: "active",
            job_category: "other",
            created_at: "2026-10-29T00:00:00Z",
            updated_at: "2026-10-29T00:00:00Z",
          }))
        : [
            {
              id: hospital,
              name: "Synthetic Hospital",
              access_level: "manager",
              created_at: "2026-10-29T00:00:00Z",
            },
          ];
      return Response.json({ data, page: { has_more: false, next_cursor: null } });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Workforce />
    </NavigationGuardProvider>,
  );
  await user.click(
    await screen.findByRole("button", { name: "Employment terms for Synthetic Staff A" }),
  );
  await user.click(await screen.findByRole("button", { name: "Prepare employment terms" }));
  const form = screen.getByRole("form", { name: "Employment terms draft" });
  await user.type(within(form).getByLabelText("Reason"), "Synthetic unsaved terms");
  await user.click(screen.getByRole("button", { name: "Employment terms for Synthetic Staff B" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Keep editing" }),
  );
  expect(within(form).getByLabelText("Reason")).toHaveValue("Synthetic unsaved terms");
  await user.click(screen.getByRole("button", { name: "Employment terms for Synthetic Staff B" }));
  await user.click(
    within(await screen.findByRole("dialog")).getByRole("button", { name: "Discard" }),
  );
  expect(
    await screen.findByRole("heading", { name: "Employment terms · Synthetic Staff B · SYN-1" }),
  ).toBeInTheDocument();
  expect(screen.queryByRole("form", { name: "Employment terms draft" })).not.toBeInTheDocument();
});
