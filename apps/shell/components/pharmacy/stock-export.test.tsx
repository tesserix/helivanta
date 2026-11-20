import { act, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useState } from "react";
import { renderWithProviders } from "@helivanta/api/testing";
import { StockExport } from "./stock-export";

const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const filters = "as_of=2026-10-18T00%3A00%3A00Z&expiry=all&balance=all";
const csvResponse = () =>
  new Response("sku,on_hand_units\r\nTEST,1\r\n", {
    headers: {
      "Content-Type": "text/csv; charset=utf-8",
      "Content-Disposition": 'attachment; filename="pharmacy-stock-report.csv"',
    },
  });
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

function downloads() {
  const create = vi.fn(() => "blob:test-export");
  const revoke = vi.fn();
  vi.stubGlobal(
    "URL",
    Object.assign(class extends URL {}, { createObjectURL: create, revokeObjectURL: revoke }),
  );
  const click = vi.spyOn(HTMLAnchorElement.prototype, "click").mockImplementation(() => {});
  return { create, revoke, click };
}

it("downloads the displayed stocktake revision through its validated register endpoint", async () => {
  const { click, create } = downloads();
  const take = {
    id: "0199e000-0000-7000-8000-000000000002",
    number: 1,
    version: 3,
    snapshot_rows: 1,
  };
  const bytes = new TextEncoder().encode("TEST register\r\n");
  const digest = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", bytes)), (b) =>
    b.toString(16).padStart(2, "0"),
  ).join("");
  const fetch = vi.fn().mockResolvedValue(
    new Response(bytes, {
      headers: {
        "Content-Type": "text/csv; charset=utf-8",
        "Content-Disposition": 'attachment; filename="stocktake-1-register.csv"',
        "Cache-Control": "private, no-store",
        "X-Stocktake-Tenant-Id": scope.tenantId,
        "X-Stocktake-Outlet-Id": scope.outletId,
        "X-Stocktake-Id": take.id,
        "X-Stocktake-Version": "3",
        "X-Export-Row-Count": "1",
        "X-Export-Sha256": digest,
      },
    }),
  );
  vi.stubGlobal("fetch", fetch);
  const { user } = renderWithProviders(
    <StockExport scope={scope} stocktake={take} disabled={false} />,
  );
  await user.click(screen.getByRole("button", { name: "Download stocktake register" }));
  await waitFor(() => expect(click).toHaveBeenCalledOnce());
  expect(create).toHaveBeenCalledWith(expect.any(Blob));
  expect(fetch.mock.calls[0]?.[0]).toContain(`/stocktakes/${take.id}/register.csv`);
});

it("shows workspace-change guidance without creating a CSV download", async () => {
  const { click, create } = downloads();
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          error: "session_changed",
          message: "Your workspace changed. Refresh the page before continuing.",
        }),
        { status: 409 },
      ),
    ),
  );
  const { user } = renderWithProviders(
    <StockExport scope={scope} filters={filters} disabled={false} />,
  );
  await user.click(screen.getByRole("button", { name: "Download CSV" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Your workspace changed. Refresh the page before continuing.",
  );
  expect(click).not.toHaveBeenCalled();
  expect(create).not.toHaveBeenCalled();
  expect(screen.getByRole("status", { name: "Stock CSV export" })).toBeEmptyDOMElement();
  expect(screen.getByRole("button", { name: "Download CSV" })).toBeEnabled();
});

it("shows an explicit oversize error and allows a fresh successful retry", async () => {
  const { click } = downloads();
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          error: "stock_export_too_large",
          message: "Stock export exceeds 1000 rows; narrow the filters.",
        }),
        { status: 413 },
      ),
    )
    .mockResolvedValueOnce(csvResponse());
  vi.stubGlobal("fetch", fetcher);
  const { user } = renderWithProviders(
    <StockExport scope={scope} filters={filters} disabled={false} />,
  );
  await user.click(screen.getByRole("button", { name: "Download CSV" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("narrow the filters");
  expect(click).not.toHaveBeenCalled();
  await user.click(screen.getByRole("button", { name: "Download CSV" }));
  await waitFor(() => expect(click).toHaveBeenCalledOnce());
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(screen.getByRole("status", { name: "Stock CSV export" })).toHaveTextContent(
    "CSV download started",
  );
});

it.each(["outlet", "filters"])(
  "cancels a pending export on %s change and never downloads a late response",
  async (change) => {
    const { click, create } = downloads();
    let resolve!: (response: Response) => void;
    const fetcher = vi.fn<(path: string, init?: RequestInit) => Promise<Response>>(
      () =>
        new Promise<Response>((r) => {
          resolve = r;
        }),
    );
    vi.stubGlobal("fetch", fetcher);
    function Workspace() {
      const [changed, setChanged] = useState(false);
      const currentScope =
        change === "outlet" && changed
          ? { ...scope, outletId: "0199e000-0000-7000-8000-000000000002" }
          : scope;
      const currentFilters =
        change === "filters" && changed
          ? filters + "&product_id=0199e000-0000-7000-8000-000000000003"
          : filters;
      return (
        <>
          <button onClick={() => setChanged(true)}>Change scope</button>
          <StockExport
            key={`${currentScope.outletId}:${currentFilters}`}
            scope={currentScope}
            filters={currentFilters}
            disabled={false}
          />
        </>
      );
    }
    const { user } = renderWithProviders(<Workspace />);
    await user.click(screen.getByRole("button", { name: "Download CSV" }));
    expect(screen.getByRole("button", { name: "Preparing CSV…" })).toBeDisabled();
    expect(fetcher).toHaveBeenCalledOnce();
    await user.click(screen.getByRole("button", { name: "Change scope" }));
    expect(fetcher.mock.calls[0]![1]?.signal?.aborted).toBe(true);
    await act(async () => resolve(csvResponse()));
    expect(click).not.toHaveBeenCalled();
    expect(create).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Download CSV" })).toBeEnabled();
    expect(screen.getByRole("status", { name: "Stock CSV export" })).toBeEmptyDOMElement();
  },
);

it("keeps transport details out of user-facing errors", async () => {
  vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("private network details")));
  const { user } = renderWithProviders(
    <StockExport scope={scope} filters={filters} disabled={false} />,
  );
  await user.click(screen.getByRole("button", { name: "Download CSV" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Could not download the stock CSV. Try again.",
  );
  expect(screen.queryByText("private network details")).not.toBeInTheDocument();
});
