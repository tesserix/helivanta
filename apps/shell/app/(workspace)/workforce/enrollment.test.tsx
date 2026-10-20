import "@/test/select";
import { chooseOption } from "@/test/select";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import Workforce from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
afterEach(() => vi.unstubAllGlobals());

it("keeps typed text visible when HR choices arrive after the user starts editing", async () => {
  let resolveChoices!: (value: Response) => void;
  const choices = new Promise<Response>((resolve) => {
    resolveChoices = resolve;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("/hr/masters")) return choices;
      const body = url.includes("permissions")
        ? { data: ["workforce.enrollment.manage"] }
        : url.endsWith("/hospitals")
          ? {
              data: [{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }],
              page: { has_more: false },
            }
          : { data: [], page: { has_more: false } };
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Add staff" }));
  await user.click(screen.getByLabelText("Department"));
  await user.paste("Recorded Synthetic Care");
  resolveChoices(
    new Response(
      JSON.stringify({
        data: [
          {
            id: "master-1",
            revision_id: "33333333-3333-4333-8333-333333333333",
            kind: "department",
            code: "SYN-OTHER",
            name: "Other Synthetic Care",
            active: true,
          },
        ],
        page: { has_more: false },
      }),
      { status: 200 },
    ),
  );
  const trigger = await screen.findByRole("combobox", { name: "Department" });
  expect(trigger).toHaveTextContent("Recorded text: Recorded Synthetic Care");
});

it("pins an active department revision and preserves the selection on save failure", async () => {
  const revision = "33333333-3333-4333-8333-333333333333";
  const writes: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      let body: unknown = { data: [], page: { has_more: false } };
      if (url.includes("permissions")) body = { data: ["workforce.enrollment.manage"] };
      else if (url.endsWith("/hospitals"))
        body = {
          data: [{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }],
          page: { has_more: false },
        };
      else if (url.includes("/hr/masters"))
        body = {
          data: [
            {
              id: "master-1",
              revision_id: revision,
              kind: "department",
              code: "SYN-DEPT",
              name: "Synthetic Care",
              active: true,
              version: 1,
            },
          ],
          page: { has_more: false },
        };
      if (init?.method === "PUT") {
        writes.push(JSON.parse(String(init.body)));
        return new Response(JSON.stringify({ message: "Synthetic conflict" }), { status: 409 });
      }
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Add staff" }));
  await user.type(screen.getByLabelText("Full name"), "Synthetic Staff");
  await user.type(screen.getByLabelText("Employee code"), "SYN-HR-REF");
  await chooseOption(user, screen.getByLabelText("Department"), "Synthetic Care · SYN-DEPT");
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({
    profile: { department: "Synthetic Care", department_revision_id: revision },
  });
  expect(screen.getByLabelText("Department")).toHaveTextContent("Synthetic Care");
});

function mockEnrollment(
  options: { status?: number; loadFailure?: boolean; permitted?: boolean } = {},
) {
  const writes: Record<string, unknown>[] = [];
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "PUT") {
      writes.push(JSON.parse(String(init.body)));
      return new Response(
        JSON.stringify({
          error: "conflict",
          message: "Staff changed since you opened this form. Reload before saving.",
        }),
        { status: options.status ?? 409 },
      );
    }
    if (url.endsWith("/enrollment"))
      return new Response(JSON.stringify({ error: { message: "Unavailable" } }), { status: 503 });
    const body = url.includes("permissions")
      ? { data: options.permitted === false ? [] : ["workforce.enrollment.manage"] }
      : url.endsWith("/hospitals")
        ? {
            data: [{ id: hospital, name: "YES HOSPITAL", access_level: "manager" }],
            page: { has_more: false },
          }
        : {
            data: options.loadFailure
              ? [
                  {
                    id: hospital,
                    display_name: "Existing Synthetic",
                    employee_code: "EX-1",
                    job_category: "other",
                    employment_status: "active",
                  },
                ]
              : [],
            page: { has_more: false },
          };
    return new Response(JSON.stringify(body), { status: 200 });
  });
  vi.stubGlobal("fetch", fetcher);
  return { writes, fetcher };
}

it("reveals and focuses enrollment with separate personal, contact and professional details", async () => {
  mockEnrollment();
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Add staff" }));
  await waitFor(() => expect(screen.getByLabelText("Full name")).toHaveFocus());
  for (const label of [
    "Date of birth",
    "Personal email",
    "Mobile number",
    "Reported blood group",
    "Engagement type",
    "Department",
    "Start date",
  ])
    expect(screen.getByLabelText(label)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Add emergency contact" }));
  expect(screen.getByLabelText("Contact name")).toBeInTheDocument();
  expect(screen.getByLabelText("Emergency phone")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Add qualification" }));
  expect(screen.getByLabelText("Qualification")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Cancel" }));
  await waitFor(() => expect(screen.getByRole("button", { name: "Add staff" })).toHaveFocus());
});

it("preserves enrollment input and stable identity after a failed save", async () => {
  const { writes } = mockEnrollment();
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Add staff" }));
  await user.click(screen.getByLabelText("Full name"));
  await user.paste("Avery Synthetic");
  await user.click(screen.getByLabelText("Employee code"));
  await user.paste("ENR-001");
  await user.click(screen.getByLabelText("Personal email"));
  await user.paste("avery@example.invalid");
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(screen.getByLabelText("Personal email")).toHaveValue("avery@example.invalid");
  expect(await screen.findByRole("alert")).toHaveTextContent(/changed/);
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(writes[0]).toMatchObject({
    expected_version: 0,
    profile: { personal_email: "avery@example.invalid" },
  });
});

it("does not expose a writable blank form when protected enrollment fails to load", async () => {
  mockEnrollment({ loadFailure: true });
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Edit Existing Synthetic" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(/Could not load enrollment/);
  expect(screen.queryByRole("button", { name: "Save staff" })).not.toBeInTheDocument();
});

it("copies the edited address into the other address type", async () => {
  mockEnrollment();
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Add staff" }));
  await user.click(screen.getByRole("button", { name: "Add address" }));
  await chooseOption(user, screen.getByRole("combobox", { name: "Address type" }), "Permanent");
  await user.click(screen.getByLabelText("Address line 1"));
  await user.paste("1 Synthetic Road");
  await user.click(screen.getByRole("button", { name: "Copy to current address" }));
  expect(
    screen.getAllByLabelText("Address line 1").map((input) => (input as HTMLInputElement).value),
  ).toEqual(["1 Synthetic Road", "1 Synthetic Road"]);
  expect(
    screen.getAllByRole("combobox", { name: "Address type" }).map((select) => select.textContent),
  ).toEqual(["Permanent", "Current"]);
});
