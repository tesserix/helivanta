import "@/test/select";
import { chooseOption } from "@/test/select";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { EMPTY_PROFILE } from "@helivanta/api";
import Workforce from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
afterEach(() => vi.unstubAllGlobals());

it.each([
  {},
  { profile_masters_version: "v1" },
  { profile_masters_version: "v1", qualification_masters_version: "v1" },
])(
  "keeps the editor closed when the API cannot confirm all master projections: %j",
  async (projection) => {
    const staff = {
      id: "44444444-3333-4333-8444-555555555555",
      display_name: "Synthetic older service staff",
      employee_code: "SYN-OLDER-SERVICE",
      job_category: "other",
      employment_status: "active",
    };
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        const body = url.includes("permissions")
          ? { data: ["workforce.enrollment.manage"] }
          : url.endsWith("/hospitals")
            ? {
                data: [{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }],
                page: { has_more: false },
              }
            : url.includes("/enrollment")
              ? {
                  staff,
                  version: 1,
                  profile: EMPTY_PROFILE,
                  ...projection,
                  addresses: [],
                  emergency_contacts: [],
                  credentials: [],
                  qualifications: [],
                  experiences: [],
                }
              : { data: [staff], page: { has_more: false } };
        return new Response(JSON.stringify(body), { status: 200 });
      }),
    );
    const { user } = renderWithProviders(<Workforce />);
    await user.click(
      await screen.findByRole("button", { name: "Edit Synthetic older service staff" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This enrollment view is not available yet",
    );
    expect(screen.queryByRole("button", { name: /^Save staff$/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Try again$/ })).toBeEnabled();
  },
);

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
  expect(fetch).toHaveBeenCalledWith(
    expect.stringMatching(
      /\/enrollment\?profile_masters=v1&qualification_masters=v1&contact_masters=v1$/,
    ),
    expect.objectContaining({ method: "PUT" }),
  );
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
    if (url.includes("/enrollment?"))
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

it("keeps long relationship masters unavailable and saves an eligible reference without truncation", async () => {
  const revision = "33333333-3333-4333-8333-333333333333";
  const name = "R".repeat(60);
  const writes: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes.push(JSON.parse(String(init.body)));
        return new Response(JSON.stringify({ message: "Synthetic conflict" }), { status: 409 });
      }
      const body = url.includes("permissions")
        ? { data: ["workforce.enrollment.manage"] }
        : url.endsWith("/hospitals")
          ? {
              data: [{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }],
              page: { has_more: false },
            }
          : url.includes("/hr/masters")
            ? {
                data: [
                  {
                    id: "synthetic-fit",
                    revision_id: revision,
                    kind: "relationship",
                    code: "SYN-FIT",
                    name,
                    active: true,
                  },
                  {
                    id: "synthetic-long",
                    revision_id: "44444444-3333-4333-8333-333333333333",
                    kind: "relationship",
                    code: "SYN-LONG",
                    name: "𑀓".repeat(61),
                    active: true,
                  },
                ],
                page: { has_more: false },
              }
            : { data: [], page: { has_more: false } };
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Add staff" }));
  await user.type(screen.getByLabelText("Full name"), "Synthetic Contact Staff");
  await user.type(screen.getByLabelText("Employee code"), "SYN-CONTACT-UI");
  await user.click(screen.getByRole("button", { name: "Add emergency contact" }));
  const relationship = await screen.findByRole("combobox", { name: "Relationship" });
  await user.click(relationship);
  expect(screen.getByRole("option", { name: /SYN-LONG/ })).toHaveAttribute("aria-disabled", "true");
  await user.keyboard("{Escape}");
  await chooseOption(user, relationship, `${name} · SYN-FIT`);
  await user.type(screen.getByLabelText("Contact name"), "Synthetic emergency contact");
  await user.type(screen.getByLabelText("Emergency phone"), "+61400000001");
  expect(screen.getByText(/60 characters/i)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({
    emergency_contacts: [{ relationship: name, relationship_revision_id: revision }],
  });
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]).toEqual(writes[0]);
  await chooseOption(user, relationship, "Enter relationship text");
  await user.clear(screen.getByLabelText("Relationship text"));
  await user.type(screen.getByLabelText("Relationship text"), "Synthetic free-text support");
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  await waitFor(() => expect(writes).toHaveLength(3));
  expect(writes[2]).toMatchObject({
    emergency_contacts: [
      { relationship: "Synthetic free-text support", relationship_revision_id: "" },
    ],
  });
});

it("selects a qualification revision without replacing its row after a failed save", async () => {
  const revision = "33333333-3333-4333-8333-333333333333";
  const writes: unknown[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes.push(JSON.parse(String(init.body)));
        return new Response(JSON.stringify({ message: "Synthetic conflict" }), { status: 409 });
      }
      const body = url.includes("permissions")
        ? { data: ["workforce.enrollment.manage"] }
        : url.endsWith("/hospitals")
          ? {
              data: [{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }],
              page: { has_more: false },
            }
          : url.includes("/hr/masters")
            ? {
                data: [
                  {
                    id: "synthetic-master",
                    revision_id: revision,
                    kind: "qualification",
                    code: "SYN-QUAL",
                    name: "Synthetic qualification",
                    active: true,
                  },
                ],
                page: { has_more: false },
              }
            : { data: [], page: { has_more: false } };
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Add staff" }));
  await user.type(screen.getByLabelText("Full name"), "Synthetic Qualification Staff");
  await user.type(screen.getByLabelText("Employee code"), "SYN-QUAL-UI");
  await user.click(screen.getByRole("button", { name: "Add qualification" }));
  await chooseOption(
    user,
    await screen.findByRole("combobox", { name: "Qualification" }),
    "Synthetic qualification · SYN-QUAL",
  );
  await user.type(screen.getByLabelText("Institution"), "Synthetic University");
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({
    qualifications: [
      { qualification: "Synthetic qualification", qualification_revision_id: revision },
    ],
  });
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[1]).toEqual(writes[0]);
});

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

it("submits versioned language, exit reason and contract type choices without losing them on conflict", async () => {
  const rows = [
    {
      kind: "language",
      id: "language-master",
      revision_id: "11111111-2222-4333-8444-555555555555",
      code: "SYN-LANG",
      name: "Synthetic language",
      active: true,
    },
    {
      kind: "exit_reason",
      id: "exit-master",
      revision_id: "22222222-2222-4333-8444-555555555555",
      code: "SYN-EXIT",
      name: "Synthetic exit reason",
      active: true,
    },
    {
      kind: "contract_type",
      id: "contract-master",
      revision_id: "33333333-2222-4333-8444-555555555555",
      code: "SYN-CONTRACT",
      name: "Synthetic contract type",
      active: true,
    },
  ];
  const writes: Record<string, unknown>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes.push(JSON.parse(String(init.body)));
        return new Response(
          JSON.stringify({ error: "conflict", message: "Staff changed. Reload before saving." }),
          { status: 409 },
        );
      }
      const body = url.includes("permissions")
        ? { data: ["workforce.enrollment.manage"] }
        : url.endsWith("/hospitals")
          ? {
              data: [{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }],
              page: { has_more: false },
            }
          : url.includes("/hr/masters")
            ? { data: rows, page: { has_more: false } }
            : { data: [], page: { has_more: false } };
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Add staff" }));
  await user.type(screen.getByLabelText("Full name"), "Synthetic classified staff");
  await user.type(screen.getByLabelText("Employee code"), "SYN-CLASSIFIED-UI");
  await chooseOption(
    user,
    await screen.findByRole("combobox", { name: /^Preferred language$/ }),
    "Synthetic language · SYN-LANG",
  );
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: /^Exit reason$/ }),
    "Synthetic exit reason · SYN-EXIT",
  );
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: /^Contract type$/ }),
    "Synthetic contract type · SYN-CONTRACT",
  );
  await user.click(screen.getByRole("button", { name: /^Save staff$/ }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({
    profile: {
      preferred_language: "Synthetic language",
      preferred_language_revision_id: rows[0]!.revision_id,
      exit_reason: "Synthetic exit reason",
      exit_reason_revision_id: rows[1]!.revision_id,
      contract_type: "Synthetic contract type",
      contract_type_revision_id: rows[2]!.revision_id,
    },
  });
  expect(await screen.findByRole("alert")).toHaveTextContent("Staff changed");
  expect(screen.getByRole("combobox", { name: /^Preferred language$/ })).toHaveTextContent(
    "Synthetic language",
  );
  expect(screen.getByRole("combobox", { name: /^Exit reason$/ })).toHaveTextContent(
    "Synthetic exit reason",
  );
  expect(screen.getByRole("combobox", { name: /^Contract type$/ })).toHaveTextContent(
    "Synthetic contract type",
  );
});

it("preserves historical profile master pins through an unrelated edit", async () => {
  const old = {
    language: "11111111-3333-4333-8444-555555555555",
    exit: "22222222-3333-4333-8444-555555555555",
    contract: "33333333-3333-4333-8444-555555555555",
  };
  const staff = {
    id: "44444444-3333-4333-8444-555555555555",
    display_name: "Synthetic pinned staff",
    employee_code: "SYN-PINNED",
    job_category: "other",
    employment_status: "active",
  };
  const profile = {
    ...EMPTY_PROFILE,
    preferred_language: "Recorded language",
    preferred_language_revision_id: old.language,
    exit_reason: "Recorded exit reason",
    exit_reason_revision_id: old.exit,
    contract_type: "Recorded contract type",
    contract_type_revision_id: old.contract,
  };
  const writes: Record<string, unknown>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes.push(JSON.parse(String(init.body)));
        return new Response(JSON.stringify({ message: "Staff changed. Reload before saving." }), {
          status: 409,
        });
      }
      const body = url.includes("permissions")
        ? { data: ["workforce.enrollment.manage"] }
        : url.endsWith("/hospitals")
          ? {
              data: [{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }],
              page: { has_more: false },
            }
          : url.includes("/enrollment?")
            ? {
                staff,
                profile_masters_version: "v1",
                qualification_masters_version: "v1",
                contact_masters_version: "v1",
                version: 7,
                profile,
                addresses: [],
                emergency_contacts: [],
                credentials: [],
                qualifications: [],
                experiences: [],
              }
            : url.includes("/hr/masters")
              ? { data: [], page: { has_more: false } }
              : { data: [staff], page: { has_more: false } };
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic pinned staff" }));
  expect(await screen.findByRole("combobox", { name: /^Preferred language$/ })).toHaveTextContent(
    "Recorded language · recorded revision",
  );
  expect(screen.getByRole("combobox", { name: /^Exit reason$/ })).toHaveTextContent(
    "Recorded exit reason · recorded revision",
  );
  expect(screen.getByRole("combobox", { name: /^Contract type$/ })).toHaveTextContent(
    "Recorded contract type · recorded revision",
  );
  await user.type(screen.getByLabelText("Preferred name"), "Unrelated name edit");
  await user.click(screen.getByRole("button", { name: /^Save staff$/ }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({
    expected_version: 7,
    profile: { ...profile, preferred_name: "Unrelated name edit" },
  });
});

it("keeps a legacy language available after explicitly selecting a current revision", async () => {
  const staff = {
    id: "55555555-3333-4333-8444-555555555555",
    display_name: "Synthetic legacy staff",
    employee_code: "SYN-LEGACY",
    job_category: "other",
    employment_status: "active",
  };
  const revision = "66666666-3333-4333-8444-555555555555";
  const writes: Record<string, unknown>[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (init?.method === "PUT") {
        writes.push(JSON.parse(String(init.body)));
        return new Response(JSON.stringify({ message: "Staff changed. Reload before saving." }), {
          status: 409,
        });
      }
      const body = url.includes("permissions")
        ? { data: ["workforce.enrollment.manage"] }
        : url.endsWith("/hospitals")
          ? {
              data: [{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }],
              page: { has_more: false },
            }
          : url.includes("/enrollment?")
            ? {
                staff,
                profile_masters_version: "v1",
                qualification_masters_version: "v1",
                contact_masters_version: "v1",
                version: 2,
                profile: { ...EMPTY_PROFILE, preferred_language: "en" },
                addresses: [],
                emergency_contacts: [],
                credentials: [],
                qualifications: [],
                experiences: [],
              }
            : url.includes("/hr/masters")
              ? {
                  data: [
                    {
                      id: "language-master",
                      revision_id: revision,
                      kind: "language",
                      code: "SYN-LANG",
                      name: "Synthetic English",
                      active: true,
                    },
                  ],
                  page: { has_more: false },
                }
              : { data: [staff], page: { has_more: false } };
      return new Response(JSON.stringify(body), { status: 200 });
    }),
  );
  const { user } = renderWithProviders(<Workforce />);
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic legacy staff" }));
  const language = await screen.findByRole("combobox", { name: /^Preferred language$/ });
  expect(language).toHaveTextContent("Recorded text: en");
  await chooseOption(user, language, "Synthetic English · SYN-LANG");
  await chooseOption(user, language, "Recorded text: en");
  await user.click(screen.getByRole("button", { name: /^Save staff$/ }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({
    profile: { preferred_language: "en", preferred_language_revision_id: "" },
  });
});
