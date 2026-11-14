import "@/test/select";
import { NavigationGuardProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";
import { screen } from "@testing-library/react";
import { StaffPostingWorkspace } from "./posting-workspace";
const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-posting-user",
};
const hospital = "22222222-2222-4222-8222-222222222222";
const staff = "33333333-3333-4333-8333-333333333333";
afterEach(() => vi.unstubAllGlobals());
for (const preparer of [false, true]) {
  it(`opens a scoped ${preparer ? "preparer" : "reader"} workspace with no profile or governance reads`, async () => {
    const paths: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        paths.push(url);
        if (url.includes("posting-scopes"))
          return Response.json({
            data: [{ hospital_id: hospital, name: "Synthetic Hospital", can_prepare: preparer }],
            page: { has_more: false, next_cursor: null },
          });
        if (url.includes("posting-targets"))
          return Response.json({
            data: [{ id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" }],
            page: { has_more: false, next_cursor: null },
          });
        if (url.endsWith("posting-draft-source"))
          return Response.json({
            hospital_id: hospital,
            staff_id: staff,
            staff_version: 1,
            employment_epoch: 1,
          });
        return Response.json({ data: [], page: { has_more: false, next_cursor: null } });
      }),
    );
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <StaffPostingWorkspace scope={scope} onContextChanged={() => {}} />
      </NavigationGuardProvider>,
    );
    await user.click(
      await screen.findByRole("button", { name: "Open posting hospital Synthetic Hospital" }),
    );
    await user.click(
      await screen.findByRole("button", { name: "Posting preparation for Synthetic Staff" }),
    );
    await screen.findByText("No posting drafts recorded.");
    if (preparer)
      expect(
        await screen.findByRole("button", { name: "Prepare posting draft" }),
      ).toBeInTheDocument();
    else
      expect(
        screen.queryByRole("button", { name: "Prepare posting draft" }),
      ).not.toBeInTheDocument();
    expect(
      paths.some((path) => path.includes(`/posting-staff/${staff}/posting-draft-source`)),
    ).toBe(true);
    expect(
      paths.some((path) => path.includes("/enrollment") || path.includes("/organization/")),
    ).toBe(false);
  });
}

it("prepares a scoped draft using only current department choices and mandatory inputs", async () => {
  const paths: string[] = [];
  const writes: Record<string, unknown>[] = [];
  const department = {
    id: "019a0000-0000-7000-8000-000000000003",
    revision_id: "019a0000-0000-7000-8000-000000000004",
    hospital_id: hospital,
    version: 1,
    code: "SYN-PHARMA",
    name: "Synthetic Pharmacy",
    kind: "department",
    status: "active",
    created_at: "2026-11-02T00:00:00Z",
    effective_at: "2026-11-02T00:00:00Z",
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      paths.push(url);
      if (init?.method === "PUT") {
        const body = JSON.parse(String(init.body));
        writes.push(body);
        const { expected_version, ...intent } = body;
        delete intent.reason;
        return Response.json({
          ...intent,
          id: url.split("/").at(-3),
          revision_id: "019a0000-0000-7000-8000-000000000005",
          version: expected_version + 1,
          hospital_id: hospital,
          staff_id: staff,
          status: "draft",
          grants_access: false,
          created_at: "2026-11-02T00:00:00Z",
          recorded_at: "2026-11-02T00:00:00Z",
        });
      }
      if (url.includes("posting-scopes"))
        return Response.json({
          data: [{ hospital_id: hospital, name: "Synthetic Hospital", can_prepare: true }],
          page: { has_more: false, next_cursor: null },
        });
      if (url.includes("posting-targets"))
        return Response.json({
          data: [{ id: staff, employee_code: "SYN-1", display_name: "Synthetic Staff" }],
          page: { has_more: false, next_cursor: null },
        });
      if (url.endsWith("posting-draft-source"))
        return Response.json({
          hospital_id: hospital,
          staff_id: staff,
          staff_version: 1,
          employment_epoch: 1,
        });
      return Response.json({
        data: url.includes("posting-department-choices") ? [department] : [],
        page: { has_more: false, next_cursor: null },
      });
    }),
  );
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <StaffPostingWorkspace scope={scope} onContextChanged={() => {}} />
    </NavigationGuardProvider>,
  );
  await user.click(
    await screen.findByRole("button", { name: "Open posting hospital Synthetic Hospital" }),
  );
  await user.click(
    await screen.findByRole("button", { name: "Posting preparation for Synthetic Staff" }),
  );
  await user.click(await screen.findByRole("button", { name: "Prepare posting draft" }));
  await user.click(await screen.findByRole("button", { name: "Save posting draft" }));
  expect(writes).toHaveLength(0);
  await user.click(await screen.findByLabelText("Department or sub-unit"));
  await user.click(await screen.findByRole("option", { name: "SYN-PHARMA · Synthetic Pharmacy" }));
  await user.type(screen.getByLabelText("Start (UTC)"), "2027-01-01T00:00");
  await user.type(screen.getByLabelText("Reason"), "Synthetic scoped placement preparation");
  await user.click(screen.getByRole("button", { name: "Save posting draft" }));
  await screen.findByText("Posting draft saved. Operating authority is unchanged.");
  expect(writes).toHaveLength(1);
  expect(paths.some((path) => path.includes(`/posting-staff/${staff}/posting-drafts/`))).toBe(true);
  expect(paths.some((path) => path.includes("/posting-department-choices"))).toBe(true);
  expect(paths.some((path) => path.includes(`/hospitals/${hospital}/departments`))).toBe(false);
});

it("asks again for posting hospitals after an outage", async () => {
  const fetcher = vi.fn(async () =>
    Response.json({ error: "unavailable", message: "Synthetic outage" }, { status: 503 }),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <StaffPostingWorkspace scope={scope} onContextChanged={vi.fn()} />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Posting hospital access could not be verified.",
  );
  const reads = fetcher.mock.calls.length;
  await user.click(screen.getByRole("button", { name: "Try again" }));
  await vi.waitFor(() => expect(fetcher.mock.calls.length).toBeGreaterThan(reads));
});
