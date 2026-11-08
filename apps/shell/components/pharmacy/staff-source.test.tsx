import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { PharmacyStaffSource } from "./staff-source";
import { useApiMutation } from "@helivanta/api";
import { chooseOption } from "@/test/select";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/pharmacy/staff",
  useSearchParams: () => new URLSearchParams(),
}));

const hospital = "11111111-1111-4111-8111-111111111111";
const tenant = "22222222-2222-4222-8222-222222222222";
const subject = "synthetic-pharmacy-staff-reader";
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
const row = {
  id: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa",
  hospital_id: hospital,
  display_name: "Synthetic canonical Pharmacist",
  employee_code: "SYN-PHARM-001",
  job_category: "pharmacist",
  employment_status: "active",
  created_at: "2026-11-02T00:00:00Z",
  updated_at: "2026-11-02T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());
// Hospital-family cases answer pharmacy employer discovery with no access.
function routed(answer: (url: string, init?: RequestInit) => Promise<Response>) {
  return vi.fn(async (url: string, init?: RequestInit) =>
    url.includes("/workforce/pharmacies?") ? Response.json(page([])) : answer(url, init),
  );
}

it("uses canonical hospital Staff in Pharma without implying outlet or dispensing authority", async () => {
  const fetcher = routed(async (url: string, init?: RequestInit) => {
    if (init?.method && init.method !== "GET") throw new Error("Staff source must be read-only");
    return Response.json(
      url.endsWith("/permissions")
        ? { tenant_id: tenant, subject, data: ["pharmacy.catalogue.read"] }
        : url.includes("/staff?")
          ? page([row])
          : page([{ id: hospital, name: "Synthetic source hospital", access_level: "reader" }]),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyStaffSource />
    </NavigationGuardProvider>,
  );
  expect(await screen.findByRole("cell", { name: row.display_name })).toBeInTheDocument();
  expect(
    screen.getByText(/does not assign someone to a pharmacy outlet or authorize dispensing/),
  ).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: /verify|approve|dispense|edit|add staff/i }),
  ).not.toBeInTheDocument();
  const request = fetcher.mock.calls.find(([url]) => url.includes("/staff?"));
  expect(request?.[0]).toContain(`/workforce/hospitals/${hospital}/staff`);
  expect(request?.[1]?.cache).toBe("no-store");
  expect(new Headers(request?.[1]?.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
  expect(new Headers(request?.[1]?.headers).get("X-Helivanta-Expected-Subject")).toBe(subject);
});

const identity = () => ({ tenant_id: tenant, subject, data: ["pharmacy.catalogue.read"] });
const sourceHospital = { id: hospital, name: "Synthetic source hospital", access_level: "reader" };
function renderSource() {
  return renderWithProviders(
    <NavigationGuardProvider>
      <PharmacyStaffSource />
    </NavigationGuardProvider>,
  );
}
function RecheckSource() {
  const recheck = useApiMutation(async () => null, {
    invalidate: [["workforce", "canonical-staff-directory"]],
  });
  return <button onClick={() => recheck.mutate()}>Recheck source in test</button>;
}

it("does not request Staff when the account has no pharmacy workspace permission", async () => {
  const fetcher = vi.fn().mockResolvedValue(Response.json({ ...identity(), data: [] }));
  vi.stubGlobal("fetch", fetcher);
  renderSource();
  expect(await screen.findByRole("alert")).toHaveTextContent("do not have permission");
  expect(fetcher.mock.calls.every(([url]) => String(url).endsWith("/permissions"))).toBe(true);
});

it("does not request Staff when pharmacy access has no hospital Staff grant", async () => {
  const fetcher = vi.fn(async (url: string) =>
    Response.json(url.endsWith("/permissions") ? identity() : page([])),
  );
  vi.stubGlobal("fetch", fetcher);
  renderSource();
  expect(await screen.findByText("No shared Staff access on this page")).toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.includes("/staff?"))).toBe(false);
});

it.each([403, 404, 409] as const)(
  "clears the entire Staff source after owner cutoff %s",
  async (status) => {
    let denied = false;
    vi.stubGlobal(
      "fetch",
      routed(async (url: string) => {
        if (url.endsWith("/permissions")) return Response.json(identity());
        if (url.includes("/staff?"))
          return denied
            ? Response.json(
                {
                  error: status === 409 ? "session_changed" : "forbidden",
                  message: "Synthetic cutoff",
                },
                { status },
              )
            : Response.json(page([row]));
        return Response.json(page([sourceHospital]));
      }),
    );
    const { user } = renderWithProviders(
      <NavigationGuardProvider>
        <PharmacyStaffSource />
        <RecheckSource />
      </NavigationGuardProvider>,
    );
    expect(await screen.findByRole("cell", { name: row.display_name })).toBeInTheDocument();
    denied = true;
    await user.click(screen.getByRole("button", { name: "Recheck source in test" }));
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent("Your account or Staff access changed"),
    );
    expect(screen.queryByText(row.display_name)).not.toBeInTheDocument();
    expect(
      screen.queryByRole("combobox", { name: "Staff source hospital" }),
    ).not.toBeInTheDocument();
  },
);

it("shows an unavailable source as a failure and never as an empty Staff directory", async () => {
  vi.stubGlobal(
    "fetch",
    routed(async (url: string) =>
      url.endsWith("/permissions")
        ? Response.json(identity())
        : url.includes("/staff?")
          ? Response.json(
              { error: "authz_unavailable", message: "Synthetic unavailable" },
              { status: 503 },
            )
          : Response.json(page([sourceHospital])),
    ),
  );
  renderSource();
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not load staff");
  expect(screen.queryByText("Your staff directory starts here")).not.toBeInTheDocument();
  expect(screen.queryByText(row.display_name)).not.toBeInTheDocument();
});

it("rejects another hospital's response instead of borrowing its Staff", async () => {
  vi.stubGlobal(
    "fetch",
    routed(async (url: string) =>
      Response.json(
        url.endsWith("/permissions")
          ? identity()
          : url.includes("/staff?")
            ? page([{ ...row, hospital_id: tenant }])
            : page([sourceHospital]),
      ),
    ),
  );
  renderSource();
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not load staff");
  expect(screen.queryByText(row.display_name)).not.toBeInTheDocument();
});

it("continues through an empty hospital page without claiming all hospital access is absent", async () => {
  const fetcher = routed(async (url: string) =>
    Response.json(
      url.endsWith("/permissions")
        ? identity()
        : url.includes("/staff?")
          ? page([row])
          : url.includes("cursor=")
            ? page([sourceHospital])
            : { data: [], page: { has_more: true, next_cursor: "next+/=" } },
    ),
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderSource();
  await user.click(await screen.findByRole("button", { name: "Next hospital page" }));
  expect(await screen.findByRole("cell", { name: row.display_name })).toBeInTheDocument();
  expect(fetcher.mock.calls.some(([url]) => url.includes("cursor=next%2B%2F%3D"))).toBe(true);
});

it("switches canonical source hospitals without retaining the previous Staff rows", async () => {
  const other = "33333333-3333-4333-8333-333333333333";
  vi.stubGlobal(
    "fetch",
    routed(async (url: string) =>
      Response.json(
        url.endsWith("/permissions")
          ? identity()
          : url.includes(`/hospitals/${other}/staff`)
            ? page([
                {
                  ...row,
                  id: "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb",
                  hospital_id: other,
                  display_name: "Synthetic second source Pharmacist",
                },
              ])
            : url.includes("/staff?")
              ? page([row])
              : page([
                  sourceHospital,
                  { ...sourceHospital, id: other, name: "Synthetic second hospital" },
                ]),
      ),
    ),
  );
  const { user } = renderSource();
  expect(await screen.findByRole("cell", { name: row.display_name })).toBeInTheDocument();
  await chooseOption(
    user,
    screen.getByRole("combobox", { name: "Staff source hospital" }),
    "Synthetic second hospital",
  );
  expect(
    await screen.findByRole("cell", { name: "Synthetic second source Pharmacist" }),
  ).toBeInTheDocument();
  expect(screen.queryByText(row.display_name)).not.toBeInTheDocument();
});

const pharmacy = "019ddddd-dddd-7ddd-8ddd-dddddddddddd";
const pharmacyRow = {
  id: "019ccccc-cccc-7ccc-8ccc-cccccccccccc",
  pharmacy_id: pharmacy,
  display_name: "Synthetic employed Pharmacist",
  employee_code: "SYN-PH-001",
  job_category: "pharmacist",
  employment_status: "active",
  created_at: "2026-11-07T00:00:00Z",
  updated_at: "2026-11-07T00:00:00Z",
};
const employerSource = (access_level: "manager" | "reader") => ({
  pharmacy_id: pharmacy,
  name: "Synthetic Standalone Pharmacy",
  access_level,
  created_at: "2026-11-07T00:00:00Z",
});
// A tenant with one standalone pharmacy employer and no hospital Staff access.
function employerTenant(
  access: "manager" | "reader",
  staff: (url: string, init?: RequestInit) => Response = () => Response.json(page([pharmacyRow])),
  permissions: string[] = [],
) {
  return vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/permissions"))
      return Response.json({ ...identity(), data: [...identity().data, ...permissions] });
    if (url.includes(`/workforce/pharmacies/${pharmacy}/staff`)) return staff(url, init);
    if (url.includes("/workforce/pharmacies?"))
      return Response.json(page([employerSource(access)]));
    if (url.includes("/workforce/hospitals?")) return Response.json(page([]));
    throw new Error(`Unexpected request ${url}`);
  });
}

it("reads a standalone pharmacy's own Staff from the pharmacy family under the expected account", async () => {
  const fetcher = employerTenant("reader");
  vi.stubGlobal("fetch", fetcher);
  renderSource();
  expect(await screen.findByRole("cell", { name: pharmacyRow.display_name })).toBeInTheDocument();
  const discovery = fetcher.mock.calls.find(([url]) => url.includes("/workforce/pharmacies?"));
  expect(discovery?.[0]).toBe("/api/v1/workforce/pharmacies?limit=50");
  const request = fetcher.mock.calls.find(([url]) => url.includes("/staff?"));
  expect(request?.[0]).toBe(`/api/v1/workforce/pharmacies/${pharmacy}/staff?limit=50`);
  expect(request?.[1]?.cache).toBe("no-store");
  expect(new Headers(request?.[1]?.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
  expect(new Headers(request?.[1]?.headers).get("X-Helivanta-Expected-Subject")).toBe(subject);
  expect(fetcher.mock.calls.some(([url]) => url.includes("/workforce/hospitals/"))).toBe(false);
  expect(screen.queryByRole("button", { name: /edit|add staff/i })).not.toBeInTheDocument();
  expect(screen.getByText("No shared Staff access on this page")).toBeInTheDocument();
});

it("lets a pharmacy Workforce manager edit a record against its current version", async () => {
  const renamed = { ...pharmacyRow, display_name: "Synthetic renamed Pharmacist" };
  const writes: RequestInit[] = [];
  const fetcher = employerTenant("manager", (url, init) => {
    if (init?.method === "PUT") {
      writes.push(init);
      return Response.json({ ...renamed, version: 3, directory_context_version: "v1" });
    }
    if (url.includes("/staff?"))
      return Response.json(page([writes.length ? renamed : pharmacyRow]));
    return Response.json({ ...pharmacyRow, version: 2, directory_context_version: "v1" });
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderSource();
  expect(await screen.findByRole("button", { name: "Add staff" })).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: `Edit ${pharmacyRow.display_name}` }));
  const name = await screen.findByLabelText("Full name");
  expect(fetcher.mock.calls.map(([url]) => url)).toContain(
    `/api/v1/workforce/pharmacies/${pharmacy}/staff/${pharmacyRow.id}?directory_context=v1`,
  );
  await user.clear(name);
  await user.type(name, renamed.display_name);
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  expect(await screen.findByRole("cell", { name: renamed.display_name })).toBeInTheDocument();
  expect(writes).toHaveLength(1);
  expect(JSON.parse(String(writes[0]?.body))).toEqual({
    employee_code: pharmacyRow.employee_code,
    display_name: renamed.display_name,
    job_category: pharmacyRow.job_category,
    employment_status: pharmacyRow.employment_status,
    expected_version: 2,
    directory_context_version: "v1",
  });
  expect(new Headers(writes[0]?.headers).get("X-Helivanta-Expected-Subject")).toBe(subject);
  await waitFor(() => expect(screen.queryByLabelText("Full name")).not.toBeInTheDocument());
});

it("keeps the draft on a version conflict and reloads the current record only after confirmation", async () => {
  let current = { ...pharmacyRow, version: 2, directory_context_version: "v1" };
  const fetcher = employerTenant("manager", (url, init) => {
    if (init?.method === "PUT")
      return Response.json(
        { error: "staff_version_conflict", message: "This Staff record changed." },
        { status: 409 },
      );
    if (url.includes("/staff?")) return Response.json(page([pharmacyRow]));
    return Response.json(current);
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderSource();
  await user.click(await screen.findByRole("button", { name: `Edit ${pharmacyRow.display_name}` }));
  const name = await screen.findByLabelText("Full name");
  await user.clear(name);
  await user.type(name, "Synthetic draft name");
  await user.click(screen.getByRole("button", { name: "Save staff" }));
  expect(await screen.findByText("This Staff record changed.")).toBeInTheDocument();
  expect(screen.getByLabelText("Full name")).toHaveValue("Synthetic draft name");
  current = { ...current, display_name: "Synthetic changed elsewhere", version: 4 };
  await user.click(screen.getByRole("button", { name: "Reload current record" }));
  await user.click(await screen.findByRole("button", { name: "Discard edits and reload" }));
  await waitFor(() =>
    expect(screen.getByLabelText("Full name")).toHaveValue("Synthetic changed elsewhere"),
  );
  const reads = fetcher.mock.calls.filter(
    ([url, init]) => url.includes(`/staff/${pharmacyRow.id}?`) && init?.method !== "PUT",
  );
  expect(reads).toHaveLength(2);
});

it("says Workforce access ended when the pharmacy directory answers 404, never an empty directory", async () => {
  vi.stubGlobal(
    "fetch",
    employerTenant("manager", () =>
      Response.json({ error: "not_found", message: "pharmacy not found" }, { status: 404 }),
    ),
  );
  renderSource();
  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("Your account or Staff access changed"),
  );
  expect(
    screen.queryByText("No Staff records are in this pharmacy's directory yet."),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add staff" })).not.toBeInTheDocument();
});

it("refuses a pharmacy page carrying another establishment's Staff", async () => {
  vi.stubGlobal(
    "fetch",
    employerTenant("reader", () =>
      Response.json(page([{ ...pharmacyRow, pharmacy_id: hospital }])),
    ),
  );
  renderSource();
  expect(await screen.findByRole("alert")).toHaveTextContent("Could not load staff");
  expect(screen.queryByText(pharmacyRow.display_name)).not.toBeInTheDocument();
});

const linkPermissions = {
  manage: "workforce.login_links.manage",
  review: "workforce.login_links.review",
};
it.each([
  ["manager", [linkPermissions.manage], true],
  ["manager", [linkPermissions.review], true],
  ["manager", [], false],
  ["reader", [linkPermissions.manage, linkPermissions.review], false],
] as const)(
  "offers the account link to a pharmacy %s holding %j: %s",
  async (access, permissions, offered) => {
    vi.stubGlobal(
      "fetch",
      employerTenant(access, () => Response.json(page([pharmacyRow])), [...permissions]),
    );
    renderSource();
    expect(await screen.findByRole("cell", { name: pharmacyRow.display_name })).toBeInTheDocument();
    const action = screen.queryByRole("button", {
      name: `Account link for ${pharmacyRow.display_name}`,
    });
    if (offered) expect(action).toBeInTheDocument();
    else expect(action).not.toBeInTheDocument();
  },
);

it("opens a pharmacy Staff record's account links on the pharmacy family", async () => {
  const fetcher = employerTenant(
    "manager",
    (url) =>
      Response.json(
        page(
          url.includes("/login-links")
            ? [
                {
                  id: "019eeeee-eeee-7eee-8eee-eeeeeeeeeeee",
                  staff_id: pharmacyRow.id,
                  subject: "synthetic-linked-account",
                  status: "active",
                  version: 2,
                  proposed_by_subject: "proposer",
                  proposal_reason: "Verified synthetic account",
                  actor_subject: "reviewer",
                  reason: "Independent check",
                  created_at: pharmacyRow.created_at,
                  updated_at: pharmacyRow.updated_at,
                },
              ]
            : [pharmacyRow],
        ),
      ),
    [linkPermissions.review],
  );
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderSource();
  await user.click(
    await screen.findByRole("button", { name: `Account link for ${pharmacyRow.display_name}` }),
  );
  expect(await screen.findByText("synthetic-linked-account")).toBeInTheDocument();
  expect(
    screen.getByText(
      `${pharmacyRow.display_name} · ${pharmacyRow.employee_code} · Synthetic Standalone Pharmacy`,
    ),
  ).toBeInTheDocument();
  expect(screen.getByText(/does not grant application access or clinical authority/)).toBeVisible();
  const read = fetcher.mock.calls.find(([url]) => url.includes("/login-links"));
  expect(read?.[0]).toBe(
    `/api/v1/workforce/pharmacies/${pharmacy}/staff/${pharmacyRow.id}/login-links?limit=50`,
  );
  expect(new Headers(read?.[1]?.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
  expect(fetcher.mock.calls.some(([url]) => url.includes("/workforce/hospitals/"))).toBe(false);
});
