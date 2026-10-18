import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { PartyRegistration } from "./party-registration";
const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  subject: "TEST-owner",
  partyId: "0199e000-0000-7000-8000-000000000001",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
beforeEach(() => localStorage.clear());

it("shows a server-calculated expiry notice in the saved premise timezone and focuses renewal", async () => {
  const initial = fixture();
  const licence = {
    id: "0199e000-0000-7000-8000-000000000003",
    version: 1,
    kind: "WHOLESALE",
    number: "TEST-RENEWAL",
    issuing_authority: "TEST Authority",
    jurisdiction: "TEST-Sindh",
    premise_id: "0199e000-0000-7000-8000-000000000002",
    issued_on: "2026-01-01",
    expires_on: "2026-10-18",
    product_categories: ["otc"],
    renewal: {
      date_status: "expires_today",
      days_remaining: 0,
      local_date: "2026-10-18",
      time_zone: "Asia/Karachi",
      observed_at: "2026-10-18T18:45:00Z",
      premise_status: "active",
    },
  };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("/licenses")) return new Response(JSON.stringify(page([licence])));
      return initial(url, init);
    }),
  );
  HTMLElement.prototype.scrollIntoView = vi.fn();
  renderWithProviders(<PartyRegistration scope={scope} country="PK" roles={["administrator"]} />);
  expect(await screen.findByText("Expires today · renew now")).toBeInTheDocument();
  expect(screen.getByText(/2026-10-18 · Asia\/Karachi/)).toBeInTheDocument();
  expect(screen.getByText(/Date notice only/)).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Edit licence TEST-RENEWAL" }));
  await waitFor(() => expect(screen.getByLabelText("Licence type code")).toHaveFocus());
});

it("clears renewal notices during refresh and after a failed request", async () => {
  const initial = fixture();
  let reads = 0;
  let finish: (response: Response) => void = () => {};
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("/licenses")) {
      reads++;
      if (reads > 1)
        return new Promise<Response>((resolve) => {
          finish = resolve;
        });
      return new Response(
        JSON.stringify(
          page([
            {
              id: "0199e000-0000-7000-8000-000000000003",
              number: "TEST-EXPIRED",
              kind: "WHOLESALE",
              renewal: {
                date_status: "expired",
                days_remaining: -1,
                local_date: "2026-10-19",
                time_zone: "Asia/Kolkata",
                observed_at: "2026-10-18T18:45:00Z",
                premise_status: "inactive",
              },
            },
          ]),
        ),
      );
    }
    return initial(url, init);
  });
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<PartyRegistration scope={scope} country="IN" roles={["auditor"]} />);
  expect(await screen.findByText("Expired 1 day ago · renew before review")).toBeInTheDocument();
  expect(screen.getByText("Premise inactive · review operating details")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Refresh licence notices" }));
  expect(await screen.findByText("Loading licences…")).toBeInTheDocument();
  expect(screen.queryByText("Expired 1 day ago · renew before review")).not.toBeInTheDocument();
  finish(
    new Response(JSON.stringify({ error: "unavailable", message: "TEST unavailable" }), {
      status: 503,
    }),
  );
  expect(
    await screen.findByText("Could not load private licence declarations."),
  ).toBeInTheDocument();
  expect(screen.queryByText("TEST-EXPIRED")).not.toBeInTheDocument();
  expect(screen.queryByText(/Expired 1 day/)).not.toBeInTheDocument();
});
function fixture(
  permissions = [
    "supply.premise.read",
    "supply.premise.write",
    "supply.license.read",
    "supply.license.write",
  ],
) {
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({ tenant_id: scope.tenantId, subject: scope.subject, data: permissions }),
      );
    if (init?.method === "PUT")
      return new Response(
        JSON.stringify({ message: "TEST registry unavailable", error: "unavailable" }),
        { status: 503 },
      );
    return new Response(JSON.stringify(page([])));
  });
  vi.stubGlobal("fetch", fetcher);
  return fetcher;
}
it("does not query private registrations without the required permissions", async () => {
  const fetcher = fixture([]);
  renderWithProviders(<PartyRegistration scope={scope} country="IN" roles={["administrator"]} />);
  await waitFor(() =>
    expect(fetcher.mock.calls.some(([url]) => url.endsWith("permissions"))).toBe(true),
  );
  expect(
    fetcher.mock.calls.some(([url]) => url.includes("/premises") || url.includes("/licenses")),
  ).toBe(false);
});
it("keeps auditors read-only even when global write permissions are present", async () => {
  fixture();
  renderWithProviders(<PartyRegistration scope={scope} country="IN" roles={["auditor"]} />);
  expect(await screen.findByText("No premises recorded.")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add premise" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Declare licence" })).not.toBeInTheDocument();
});
it("focuses a new independent premise and preserves the ID after a failed save", async () => {
  const fetcher = fixture();
  const user = userEvent.setup();
  HTMLElement.prototype.scrollIntoView = vi.fn();
  renderWithProviders(<PartyRegistration scope={scope} country="PK" roles={["administrator"]} />);
  await user.click(await screen.findByRole("button", { name: "Add premise" }));
  await waitFor(() => expect(screen.getByLabelText("Premise code")).toHaveFocus());
  await user.type(screen.getByLabelText("Premise code"), "TEST-DEPOT");
  await user.type(screen.getByLabelText("Premise name"), "TEST Depot");
  await user.type(screen.getByLabelText("Address line 1"), "TEST Office");
  await user.type(screen.getByLabelText("City"), "TEST Karachi");
  await user.type(screen.getByLabelText("State or province"), "TEST Sindh");
  await user.type(screen.getByLabelText("Postal code"), "TEST-001");
  await user.click(screen.getByRole("button", { name: "Save premise" }));
  expect(await screen.findByText("TEST registry unavailable")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Save premise" }));
  await waitFor(() =>
    expect(fetcher.mock.calls.filter(([, init]) => init?.method === "PUT")).toHaveLength(2),
  );
  const writes = fetcher.mock.calls.filter(([, init]) => init?.method === "PUT");
  expect(writes[0][0]).toBe(writes[1][0]);
  expect(JSON.parse(writes[0][1]?.body as string)).toMatchObject({
    country_code: "PK",
    time_zone: "Asia/Karachi",
    expected_version: 0,
  });
  expect(JSON.parse(writes[0][1]?.body as string)).not.toHaveProperty("hospital_id");
});
it("resolves an edited licence's premise even when it is outside the loaded page", async () => {
  const premise = {
    id: "0199e000-0000-7000-8000-000000000002",
    code: "TEST-OLDER-DEPOT",
    name: "TEST Older Depot",
    status: "active",
  };
  const licence = {
    id: "0199e000-0000-7000-8000-000000000003",
    version: 1,
    kind: "WHOLESALE",
    number: "TEST-001",
    issuing_authority: "TEST Authority",
    jurisdiction: "TEST-Sindh",
    premise_id: premise.id,
    issued_on: "2026-01-01",
    expires_on: "2030-12-31",
    product_categories: ["otc"],
  };
  const initial = fixture();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith(`/premises/${premise.id}`)) return new Response(JSON.stringify(premise));
      if (url.includes("/licenses")) return new Response(JSON.stringify(page([licence])));
      return initial(url, init);
    }),
  );
  const user = userEvent.setup();
  renderWithProviders(<PartyRegistration scope={scope} country="PK" roles={["administrator"]} />);
  await user.click(await screen.findByRole("button", { name: "Edit licence TEST-001" }));
  await waitFor(() =>
    expect(screen.getByRole("combobox", { name: "Licensed premise" })).toHaveTextContent(
      "TEST-OLDER-DEPOT · TEST Older Depot",
    ),
  );
});
it("shows the actual licence validity in each retained revision", async () => {
  const id = "0199e000-0000-7000-8000-000000000003";
  const licence = {
    id,
    kind: "TEST",
    number: "TEST-LIC",
    issued_on: "2026-01-01",
    expires_on: "2031-12-31",
  };
  const initial = fixture();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes(`/licenses/${id}/revisions`))
        return new Response(
          JSON.stringify(
            page([
              {
                id: "TEST-revision-2",
                version: 2,
                actor_subject: "TEST-owner",
                created_at: "2026-10-16T00:00:00Z",
                snapshot: { ...licence, product_categories: ["otc"] },
              },
              {
                id: "TEST-revision-1",
                version: 1,
                actor_subject: "TEST-owner",
                created_at: "2026-10-15T00:00:00Z",
                snapshot: { ...licence, expires_on: "2030-12-31", product_categories: ["otc"] },
              },
            ]),
          ),
        );
      if (url.includes("/licenses")) return new Response(JSON.stringify(page([licence])));
      return initial(url, init);
    }),
  );
  const user = userEvent.setup();
  renderWithProviders(<PartyRegistration scope={scope} country="PK" roles={["auditor"]} />);
  await user.click(await screen.findByRole("button", { name: "History for licence TEST-LIC" }));
  const history = await screen.findByRole("region", { name: "Registration revision history" });
  expect(history).toHaveTextContent("Expires 2031-12-31");
  expect(history).toHaveTextContent("Expires 2030-12-31");
});

it("marks existing evidence historical immediately after its licence is revised", async () => {
  const premise = {
    id: "0199e000-0000-7000-8000-000000000002",
    code: "TEST-DEPOT",
    name: "TEST Depot",
    kind: "warehouse",
    status: "active",
  };
  let licence = {
    id: "0199e000-0000-7000-8000-000000000003",
    version: 1,
    kind: "TEST",
    number: "TEST-LIC",
    issuing_authority: "TEST Authority",
    jurisdiction: "TEST Sindh",
    premise_id: premise.id,
    issued_on: "2026-01-01",
    expires_on: "2030-12-31",
    product_categories: ["otc"],
  };
  const permissions = [
    "supply.premise.read",
    "supply.license.read",
    "supply.license.write",
    "supply.evidence.read",
  ];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({ tenant_id: scope.tenantId, subject: scope.subject, data: permissions }),
        );
      if (url.includes("/evidence"))
        return new Response(
          JSON.stringify(
            page([
              {
                id: "0199e000-0000-7000-8000-000000000004",
                party_id: scope.partyId,
                license_id: licence.id,
                license_version: 1,
                filename: "TEST old.png",
                content_type: "image/png",
                size: 1,
                status: "revoked",
                scan_attempt: 1,
                uploaded_at: "2026-10-16T00:00:00Z",
                scanned_at: "2026-10-16T00:00:00Z",
                failure_code: null,
                uploader_subject: "TEST",
                created_at: "2026-10-16T00:00:00Z",
                updated_at: "2026-10-16T00:00:00Z",
                expires_at: "2026-10-16T00:30:00Z",
              },
            ]),
          ),
        );
      if (init?.method === "PUT") {
        licence = { ...licence, ...JSON.parse(init.body as string), version: 2 };
        return new Response(JSON.stringify(licence));
      }
      if (url.includes("/licenses")) return new Response(JSON.stringify(page([licence])));
      return new Response(JSON.stringify(page([premise])));
    }),
  );
  HTMLElement.prototype.scrollIntoView = vi.fn();
  const user = userEvent.setup();
  renderWithProviders(<PartyRegistration scope={scope} country="PK" roles={["administrator"]} />);
  await user.click(await screen.findByRole("button", { name: "Evidence for licence TEST-LIC" }));
  expect(await screen.findByText("TEST old.png")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Edit licence TEST-LIC" }));
  await user.clear(screen.getByLabelText("Expiry date"));
  await user.type(screen.getByLabelText("Expiry date"), "2032-12-31");
  await user.click(screen.getByRole("button", { name: "Save licence declaration" }));
  expect(await screen.findByText(/Current revision 2/)).toBeInTheDocument();
  expect(await screen.findByText(/Revision 1.*historical/)).toBeInTheDocument();
});

it("keeps declaration labels separate from the independently loaded review status", async () => {
  const initial = fixture();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) =>
      url.endsWith("/licenses")
        ? new Response(
            JSON.stringify(
              page([
                {
                  id: "0199e000-0000-7000-8000-000000000009",
                  version: 1,
                  kind: "WHOLESALE",
                  number: "TEST-REVIEW-LABEL",
                  issuing_authority: "TEST Authority",
                  jurisdiction: "TEST-Sindh",
                  issued_on: "2026-01-01",
                  expires_on: "2030-12-31",
                },
              ]),
            ),
          )
        : initial(url, init),
    ),
  );
  renderWithProviders(<PartyRegistration scope={scope} country="PK" roles={["administrator"]} />);
  expect(
    await screen.findByRole("button", { name: "Review for licence TEST-REVIEW-LABEL" }),
  ).toBeInTheDocument();
  expect(screen.queryByText("Declared · verification pending")).not.toBeInTheDocument();
  expect(screen.getByText("Declaration recorded")).toBeInTheDocument();
});

it("opens owned warehouse bins without copying the premises master", async () => {
  const premise = {
    id: "0199e000-0000-7000-8000-000000000002",
    code: "TEST-WAREHOUSE",
    name: "TEST Warehouse",
    kind: "warehouse",
    status: "active",
  };
  const initial = fixture(["supply.premise.read", "supply.bin.read", "supply.bin.write"]);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) =>
      url.includes("/premises") && !url.includes("/bins")
        ? new Response(JSON.stringify(page([premise])))
        : initial(url, init),
    ),
  );
  const user = userEvent.setup();
  renderWithProviders(
    <PartyRegistration scope={scope} country="IN" roles={["administrator"]} owned />,
  );
  await user.click(await screen.findByRole("button", { name: "Bins for premise TEST-WAREHOUSE" }));
  expect(await screen.findByRole("region", { name: "Warehouse bins" })).toHaveTextContent(
    "TEST Warehouse",
  );
  expect(await screen.findByText("No bins recorded for this premise.")).toBeInTheDocument();
});

it("selects an owned branch for a warehouse through the reusable selector", async () => {
  const branch = {
    id: "0199e000-0000-7000-8000-000000000041",
    code: "TEST-BRANCH",
    name: "TEST Central branch",
    kind: "branch",
    status: "active",
  };
  const fetcher = fixture();
  fetcher.mockImplementation(async (url: string, init?: RequestInit) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({ data: ["supply.premise.read", "supply.premise.write"] }),
      );
    if (init?.method === "PUT")
      return new Response(
        JSON.stringify({ error: "unavailable", message: "TEST retry warehouse" }),
        { status: 503 },
      );
    return new Response(JSON.stringify(page(url.endsWith("/premises") ? [branch] : [])));
  });
  const { user } = renderWithProviders(
    <PartyRegistration scope={scope} country="IN" roles={["administrator"]} owned />,
  );
  await user.click(await screen.findByRole("button", { name: "Add premise" }));
  await user.click(screen.getByRole("combobox", { name: "Operating branch" }));
  await user.click(screen.getByRole("option", { name: "TEST-BRANCH · TEST Central branch" }));
  for (const [name, value] of [
    ["Premise code", "TEST-WH"],
    ["Premise name", "TEST Warehouse"],
    ["Address line 1", "TEST Building"],
    ["City", "TEST Mumbai"],
    ["State or province", "TEST Maharashtra"],
    ["Postal code", "400001"],
  ])
    await user.type(screen.getByLabelText(name), value);
  await user.click(screen.getByRole("button", { name: "Save premise" }));
  await screen.findByText("TEST retry warehouse");
  const put = fetcher.mock.calls.find(([, init]) => init?.method === "PUT");
  expect(JSON.parse(put?.[1]?.body as string)).toMatchObject({ branch_premise_id: branch.id });
  expect(screen.getByRole("combobox", { name: "Operating branch" })).toHaveTextContent(
    "TEST Central branch",
  );
});

const linkedWarehouse = {
  id: "0199e000-0000-7000-8000-000000000051",
  code: "TEST-LINKED-WH",
  name: "TEST Linked warehouse",
  kind: "warehouse",
  status: "active",
  version: 2,
  line1: "TEST Building",
  line2: "",
  city: "TEST City",
  region: "TEST Region",
  postal_code: "TEST-001",
  country_code: "IN",
  time_zone: "Asia/Kolkata",
  branch_premise_id: "0199e000-0000-7000-8000-000000000052",
};

it("resolves a branch outside the loaded premise page and explicitly unlinks it", async () => {
  const fetcher = fixture();
  fetcher.mockImplementation(async (url: string, init?: RequestInit) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({ data: ["supply.premise.read", "supply.premise.write"] }),
      );
    if (init?.method === "PUT")
      return new Response(
        JSON.stringify({ error: "unavailable", message: "TEST preserve unlink draft" }),
        { status: 503 },
      );
    if (url.endsWith(linkedWarehouse.branch_premise_id))
      return new Response(
        JSON.stringify({
          id: linkedWarehouse.branch_premise_id,
          code: "TEST-OLD-BRANCH",
          name: "TEST Previous branch",
          kind: "branch",
          status: "inactive",
        }),
      );
    return new Response(JSON.stringify(page(url.endsWith("/premises") ? [linkedWarehouse] : [])));
  });
  const { user } = renderWithProviders(
    <PartyRegistration scope={scope} country="IN" roles={["administrator"]} owned />,
  );
  await user.click(await screen.findByRole("button", { name: "Edit premise TEST-LINKED-WH" }));
  await waitFor(() =>
    expect(screen.getByRole("combobox", { name: "Operating branch" })).toHaveTextContent(
      "TEST Previous branch · Inactive",
    ),
  );
  await user.click(screen.getByRole("combobox", { name: "Operating branch" }));
  await user.click(screen.getByRole("option", { name: "Standalone location" }));
  await user.click(screen.getByRole("button", { name: "Save premise" }));
  await screen.findByText("TEST preserve unlink draft");
  const put = fetcher.mock.calls.find(([, init]) => init?.method === "PUT");
  expect(JSON.parse(put?.[1]?.body as string)).toMatchObject({
    expected_version: 2,
    branch_premise_id: null,
  });
  expect(screen.getByRole("combobox", { name: "Operating branch" })).toHaveTextContent(
    "Standalone location",
  );
});

it("blocks saving and offers recovery when the selected private branch is unavailable", async () => {
  const fetcher = fixture();
  fetcher.mockImplementation(async (url: string) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({ data: ["supply.premise.read", "supply.premise.write"] }),
      );
    if (url.endsWith(linkedWarehouse.branch_premise_id))
      return new Response(
        JSON.stringify({ error: "not_found", message: "TEST inaccessible branch" }),
        { status: 404 },
      );
    return new Response(JSON.stringify(page(url.endsWith("/premises") ? [linkedWarehouse] : [])));
  });
  const { user } = renderWithProviders(
    <PartyRegistration scope={scope} country="IN" roles={["administrator"]} owned />,
  );
  await user.click(await screen.findByRole("button", { name: "Edit premise TEST-LINKED-WH" }));
  await screen.findByText("Could not load the selected branch. Refresh before saving.");
  expect(screen.getByRole("button", { name: "Save premise" })).toBeDisabled();
  expect(screen.getByRole("combobox", { name: "Operating branch" })).not.toHaveTextContent(
    "TEST Previous branch",
  );
  expect(fetcher.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
});
