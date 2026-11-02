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

it("uses canonical hospital Staff in Pharma without implying outlet or dispensing authority", async () => {
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
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
      vi.fn(async (url: string) => {
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
    vi.fn(async (url: string) =>
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
    vi.fn(async (url: string) =>
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
  const fetcher = vi.fn(async (url: string) =>
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
    vi.fn(async (url: string) =>
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
