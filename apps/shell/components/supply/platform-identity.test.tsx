import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { useState } from "react";
import { renderWithProviders } from "@helivanta/api/testing";
import { PlatformIdentity } from "./platform-identity";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "TEST-reader",
  partyId: "0199e000-0000-7000-8000-000000000020",
};
const proof = () => ({
  party_id: scope.partyId,
  party_version: 1,
  trading_approved: false,
  identity: {
    schema_version: 1,
    tenant_id: scope.tenantId,
    product_key: "TEST-product",
    organization_id: "TEST-source-org",
    project_id: "TEST-source-project",
    grant_id: "TEST-source-grant",
    state: "active",
    verified_at: "2026-10-17T00:00:00Z",
    source_revisions: { organization: "101", metadata: "102", project: "103", grant: "104" },
  },
});

it.each(["tenant", "organisation", "revision", "inactive", "trading"])(
  "does not display a mismatched %s receipt",
  async (change) => {
    const value = proof();
    if (change === "tenant") value.identity.tenant_id = "22222222-2222-4222-8222-222222222222";
    if (change === "organisation") value.party_id = "0199e000-0000-7000-8000-000000000021";
    if (change === "revision") value.party_version = 2;
    if (change === "inactive") value.identity.state = "inactive";
    if (change === "trading") value.trading_approved = true;
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify(value))),
    );
    renderWithProviders(<PlatformIdentity scope={scope} partyVersion={1} />);
    await userEvent.click(screen.getByRole("button", { name: "Check workspace identity" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("could not be confirmed");
    expect(screen.queryByText("Workspace identity confirmed")).not.toBeInTheDocument();
    expect(screen.queryByText("TEST-source-org")).not.toBeInTheDocument();
  },
);

it("hides the previous confirmation while refreshing and supports retry after failure", async () => {
  let finish: (value: Response) => void = () => {};
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(new Response(JSON.stringify(proof())))
    .mockImplementationOnce(
      () =>
        new Promise<Response>((resolve) => {
          finish = resolve;
        }),
    )
    .mockResolvedValueOnce(new Response(JSON.stringify(proof())));
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<PlatformIdentity scope={scope} partyVersion={1} />);
  await userEvent.click(screen.getByRole("button", { name: "Check workspace identity" }));
  expect(await screen.findByText("Workspace identity confirmed")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Check again" }));
  expect(await screen.findByRole("status")).toHaveTextContent("Checking workspace identity");
  expect(screen.queryByText("TEST-source-org")).not.toBeInTheDocument();
  finish(
    new Response(
      JSON.stringify({ error: "organization_identity_unavailable", message: "TEST unavailable" }),
      { status: 503 },
    ),
  );
  expect(await screen.findByRole("alert")).toHaveTextContent("could not be confirmed");
  expect(screen.queryByText("Workspace identity confirmed")).not.toBeInTheDocument();
  const retry = screen.getByRole("button", { name: "Try again" });
  retry.focus();
  await userEvent.keyboard("{Enter}");
  expect(await screen.findByText("Workspace identity confirmed")).toBeInTheDocument();
  expect(fetcher).toHaveBeenCalledTimes(3);
});

it("clears confirmation when the workspace scope changes", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response(JSON.stringify(proof()))),
  );
  function WorkspaceSwitcher() {
    const [current, setCurrent] = useState(scope);
    return (
      <>
        <button
          onClick={() => setCurrent({ ...scope, tenantId: "22222222-2222-4222-8222-222222222222" })}
        >
          Switch workspace
        </button>
        <PlatformIdentity scope={current} partyVersion={1} />
      </>
    );
  }
  renderWithProviders(<WorkspaceSwitcher />);
  await userEvent.click(screen.getByRole("button", { name: "Check workspace identity" }));
  expect(await screen.findByText("Workspace identity confirmed")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Switch workspace" }));
  await waitFor(() => expect(screen.queryByText("TEST-source-org")).not.toBeInTheDocument());
  expect(screen.getByRole("button", { name: "Check workspace identity" })).toBeEnabled();
});
afterEach(() => vi.unstubAllGlobals());

it("checks workspace identity on request without treating it as permission to trade", async () => {
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    expect(url).toContain(`/parties/${scope.partyId}/platform-identity`);
    expect(init?.cache).toBe("no-store");
    return new Response(JSON.stringify(proof()));
  });
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<PlatformIdentity scope={scope} partyVersion={1} />);
  expect(fetcher).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "Check workspace identity" }));
  expect(await screen.findByText("Workspace identity confirmed")).toBeInTheDocument();
  expect(screen.getByText("Trading approval is separate")).toBeInTheDocument();
  const headers = new Headers(fetcher.mock.calls[0]?.[1]?.headers);
  expect(headers.get("X-Helivanta-Expected-Tenant")).toBe(scope.tenantId);
  expect(screen.getByRole("button", { name: "Check again" })).toBeEnabled();
});
