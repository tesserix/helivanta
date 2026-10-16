import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { PartyRegistration } from "./party-registration";
const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  subject: "TEST-owner",
  partyId: "0199e000-0000-7000-8000-000000000001",
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
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
