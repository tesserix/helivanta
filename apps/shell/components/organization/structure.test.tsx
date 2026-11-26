import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import { OrganizationStructure } from "./structure";

// Zone tidy (#1256/#1257/#1263): controls use the shared control class and tables the shared style.

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/settings/organization",
  useSearchParams: () => new URLSearchParams(),
}));
const hospital = "11111111-1111-4111-8111-111111111111";
const tenant = "22222222-2222-4222-8222-222222222222";
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
const root = {
  hospital_id: hospital,
  request_id: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa",
  status: "verified",
  version: 2,
  created_at: "2026-11-02T00:00:00Z",
};
const node = {
  id: "019bbbbb-bbbb-7bbb-8bbb-bbbbbbbbbbbb",
  hospital_id: hospital,
  revision_id: "019ccccc-cccc-7ccc-8ccc-cccccccccccc",
  version: 1,
  code: "PHARMA",
  name: "Synthetic Pharmacy",
  kind: "department",
  status: "active",
  effective_at: root.created_at,
  created_at: root.created_at,
};
afterEach(() => vi.unstubAllGlobals());
function mount() {
  return renderWithProviders(
    <NavigationGuardProvider>
      <OrganizationStructure />
    </NavigationGuardProvider>,
  );
}

it("shows the authorized hospital name in the reference and department context", async () => {
  const name = "Synthetic named hospital";
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/permissions"))
      return Response.json({
        tenant_id: tenant,
        subject: "synthetic-reader",
        data: ["organization.structure.read"],
      });
    if (url.startsWith("/api/v1/workforce/hospitals?")) {
      expect(new Headers(init?.headers).get("X-Helivanta-Expected-Tenant")).toBe(tenant);
      expect(new Headers(init?.headers).get("X-Helivanta-Expected-Subject")).toBe(
        "synthetic-reader",
      );
      return Response.json(page([{ id: hospital, name, access_level: "reader" }]));
    }
    return Response.json(url.includes("/departments?") ? page([node]) : page([root]));
  });
  vi.stubGlobal("fetch", fetcher);
  const { user } = mount();
  expect(
    await screen.findByText("The shared department structure used by Staff and product teams."),
  ).toBeVisible();
  expect(screen.queryByText(/Department membership and product duties/)).not.toBeInTheDocument();
  await user.click(await screen.findByRole("button", { name: `Open departments for ${name}` }));
  expect(await screen.findByRole("table", { name: `Departments at ${name}` })).toBeVisible();
  expect(screen.getByRole("cell", { name: node.name }).closest("tr")).toHaveClass("h-11");
  expect(screen.getByRole("columnheader", { name: "Name" })).not.toHaveClass("uppercase");
  // The cut sentence's rule is already stated beside "Add department or sub-unit".
  expect(
    screen.getByText(
      "These units describe the organization. They do not assign Staff or grant outlet, clinical or login access.",
    ),
  ).toBeVisible();
  expect(screen.getByText(name, { exact: true })).toBeVisible();
  expect(screen.queryByText(new RegExp(hospital))).not.toBeInTheDocument();
});

it.each([403, 503])(
  "withholds cached hospital names after directory refusal %s",
  async (status) => {
    let refused = false;
    vi.stubGlobal("fetch", async (url: string) => {
      if (url.endsWith("/permissions"))
        return Response.json({
          tenant_id: tenant,
          subject: "synthetic-reader",
          data: ["organization.structure.read"],
        });
      if (url.startsWith("/api/v1/workforce/hospitals?"))
        return refused
          ? Response.json(
              { error: "unavailable", message: "Synthetic directory refusal" },
              { status },
            )
          : Response.json(
              page([
                { id: hospital, name: "Previously permitted hospital", access_level: "reader" },
              ]),
            );
      return Response.json(page([root]));
    });
    const { user } = mount();
    expect(await screen.findByText("Previously permitted hospital")).toBeVisible();
    refused = true;
    await user.click(screen.getByRole("button", { name: "Refresh hospital references" }));
    expect(await screen.findByText("Hospital name unavailable")).toBeVisible();
    expect(screen.queryByText("Previously permitted hospital")).not.toBeInTheDocument();
    expect(screen.queryByText(new RegExp(hospital))).not.toBeInTheDocument();
  },
);

it("loads additional permitted hospital names only when requested", async () => {
  const directoryCalls: string[] = [];
  vi.stubGlobal("fetch", async (url: string) => {
    if (url.endsWith("/permissions"))
      return Response.json({
        tenant_id: tenant,
        subject: "synthetic-reader",
        data: ["organization.structure.read"],
      });
    if (url.startsWith("/api/v1/workforce/hospitals?")) {
      directoryCalls.push(url);
      return Response.json(
        url.includes("cursor=names-next")
          ? page([{ id: hospital, name: "Later permitted hospital", access_level: "reader" }])
          : {
              ...page([]),
              page: { has_more: true, next_cursor: "names-next" },
            },
      );
    }
    return Response.json(page([root]));
  });
  const { user } = mount();
  await screen.findByText("Hospital name unavailable");
  expect(directoryCalls).toHaveLength(1);
  await user.click(await screen.findByRole("button", { name: /Load more hospital names/ }));
  expect(await screen.findByText("Later permitted hospital")).toBeVisible();
  expect(directoryCalls).toHaveLength(2);
  expect(directoryCalls[1]).toContain("cursor=names-next");
  expect(screen.queryByText(new RegExp(hospital))).not.toBeInTheDocument();
});
function stubHospitalFetch(fetcher: (url: string, init?: RequestInit) => Promise<Response>) {
  vi.stubGlobal("fetch", (url: string, init?: RequestInit) =>
    url.startsWith("/api/v1/workforce/hospitals?")
      ? Promise.resolve(
          Response.json(
            page([{ id: hospital, name: "Synthetic source hospital", access_level: "reader" }]),
          ),
        )
      : fetcher(url, init),
  );
}

it("denies an unprivileged account before any structural metadata fetch", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    Response.json({
      tenant_id: tenant,
      subject: "synthetic-staff",
      data: ["workforce.staff.read"],
    }),
  );
  stubHospitalFetch(fetcher);
  mount();
  expect(await screen.findByText(/organization structure access required/i)).toBeInTheDocument();
  expect(fetcher.mock.calls.every(([url]) => (url as string).endsWith("/permissions"))).toBe(true);
});
it("shows canonical departments read-only to a governance reader", async () => {
  stubHospitalFetch(
    vi.fn(async (url: string) =>
      Response.json(
        url.endsWith("/permissions")
          ? {
              tenant_id: tenant,
              subject: "synthetic-reader",
              data: ["organization.structure.read"],
            }
          : url.includes("/departments?")
            ? page([node])
            : page([root]),
      ),
    ),
  );
  const { user } = mount();
  await user.click(
    await screen.findByRole("button", { name: "Open departments for Synthetic source hospital" }),
  );
  expect(await screen.findByRole("cell", { name: node.name })).toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Add department or sub-unit" }),
  ).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: `Edit ${node.name}` })).not.toBeInTheDocument();
});
it("requires basic fields and reason, then creates a canonical department once", async () => {
  let saved: typeof node | null = null;
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/permissions"))
      return Response.json({
        tenant_id: tenant,
        subject: "synthetic-manager",
        data: ["organization.structure.read", "organization.structure.manage"],
      });
    if (init?.method === "PUT") {
      const body = JSON.parse(init.body as string);
      saved = {
        ...node,
        code: body.code,
        name: body.name,
        kind: body.kind,
        status: body.status,
        id: url.split("/").at(-3)!,
        revision_id: url.split("/").at(-1)!,
      };
      return Response.json(saved);
    }
    return Response.json(url.includes("/departments?") ? page(saved ? [saved] : []) : page([root]));
  });
  stubHospitalFetch(fetcher);
  const { user } = mount();
  await user.click(
    await screen.findByRole("button", { name: "Open departments for Synthetic source hospital" }),
  );
  await user.click(await screen.findByRole("button", { name: "Add department or sub-unit" }));
  await user.click(screen.getByRole("button", { name: "Save department revision" }));
  expect(await screen.findByText(/enter a name/i)).toBeInTheDocument();
  expect(
    screen.getByText("Enter a reason for this change (1–500 characters)."),
  ).toBeInTheDocument();
  expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
  await user.type(screen.getByLabelText("Department code"), "PHARMA");
  await user.type(screen.getByLabelText("Name"), node.name);
  await user.type(screen.getByLabelText("Reason for change"), "Synthetic structure QA");
  await user.click(screen.getByRole("button", { name: "Save department revision" }));
  await waitFor(() =>
    expect(fetcher.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(1),
  );
  expect(await screen.findByRole("cell", { name: node.name })).toBeInTheDocument();
});

it("edits with the current version and retains the same command when an unchanged save is retried", async () => {
  let current = { ...node };
  const commands: string[] = [];
  stubHospitalFetch(
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/permissions"))
        return Response.json({
          tenant_id: tenant,
          subject: "synthetic-manager",
          data: ["organization.structure.read", "organization.structure.manage"],
        });
      if (init?.method === "PUT") {
        commands.push(url);
        if (commands.length === 1)
          return Response.json(
            { error: "unavailable", message: "Synthetic temporary publication failure" },
            { status: 503 },
          );
        const body = JSON.parse(init.body as string);
        expect(body.expected_version).toBe(1);
        current = { ...node, name: body.name, version: 2, revision_id: url.split("/").at(-1)! };
        return Response.json(current);
      }
      return Response.json(url.includes("/departments?") ? page([current]) : page([root]));
    }),
  );
  const { user } = mount();
  await user.click(
    await screen.findByRole("button", { name: "Open departments for Synthetic source hospital" }),
  );
  await user.click(await screen.findByRole("button", { name: `Edit ${node.name}` }));
  await user.clear(screen.getByLabelText("Name"));
  await user.type(screen.getByLabelText("Name"), "Updated synthetic Pharmacy");
  await user.type(
    screen.getByLabelText("Reason for change"),
    "Synthetic rename with retained retry",
  );
  await user.click(screen.getByRole("button", { name: "Save department revision" }));
  expect(await screen.findByText(/synthetic temporary publication failure/i)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Save department revision" }));
  expect(
    await screen.findByRole("cell", { name: "Updated synthetic Pharmacy" }),
  ).toBeInTheDocument();
  expect(commands).toHaveLength(2);
  expect(commands[0]).toBe(commands[1]);
});
it("hides old departments and editing after the owner refuses a refreshed read", async () => {
  let denied = false;
  stubHospitalFetch(
    vi.fn(async (url: string) => {
      if (url.endsWith("/permissions"))
        return Response.json({
          tenant_id: tenant,
          subject: "synthetic-manager",
          data: ["organization.structure.read", "organization.structure.manage"],
        });
      if (url.includes("/departments?"))
        return denied
          ? Response.json(
              { error: "forbidden", message: "Synthetic permission revoked" },
              { status: 403 },
            )
          : Response.json(page([node]));
      return Response.json(page([root]));
    }),
  );
  const { user } = mount();
  await user.click(
    await screen.findByRole("button", { name: "Open departments for Synthetic source hospital" }),
  );
  expect(await screen.findByRole("cell", { name: node.name })).toBeInTheDocument();
  denied = true;
  await user.click(screen.getByRole("button", { name: "Refresh departments" }));
  expect(
    await screen.findByText("This wasn't found, or you don't have access to it."),
  ).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Try again" })).toBeNull();
  expect(screen.queryByRole("button", { name: "Refresh departments" })).toBeNull();
  expect(screen.queryByRole("cell", { name: node.name })).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Add department or sub-unit" })).toBeDisabled();
});
