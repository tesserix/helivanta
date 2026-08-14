import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithProviders } from "@hms/api/testing";
import { PingPanel } from "./ping-panel";

function jsonResponse(status: number, body: unknown) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function page(data: unknown[], opts: { hasMore?: boolean; nextCursor?: string | null } = {}) {
  return { data, page: { next_cursor: opts.nextCursor ?? null, has_more: opts.hasMore ?? false } };
}

afterEach(() => vi.unstubAllGlobals());

describe("PingPanel", () => {
  it("shows the empty state, then sends a ping and lists it", async () => {
    let sent = false;
    const fetchMock = vi.fn().mockImplementation((url: string, init?: RequestInit) => {
      if (init?.method === "POST") {
        sent = true;
        return Promise.resolve(jsonResponse(202, { id: "p-1" }));
      }
      return Promise.resolve(
        jsonResponse(
          200,
          page(sent ? [{ id: "p-1", message: "OPD ping", created_at: "2026-08-04T04:00:00Z" }] : []),
        ),
      );
    });
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<PingPanel department="OPD" />);
    expect(await screen.findByText(/No activity yet/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Ping from OPD" }));
    await waitFor(() => expect(screen.getByText("OPD ping")).toBeInTheDocument());
  });

  it("loads the next page on click, then hides Load more once the list is complete", async () => {
    const first = { id: "p-1", message: "first ping", created_at: "2026-08-04T04:00:00Z" };
    const second = { id: "p-2", message: "second ping", created_at: "2026-08-04T03:00:00Z" };
    const fetchMock = vi.fn().mockImplementation((url: string) => {
      if (url.includes("cursor=")) {
        return Promise.resolve(jsonResponse(200, page([second])));
      }
      return Promise.resolve(jsonResponse(200, page([first], { hasMore: true, nextCursor: "c1" })));
    });
    vi.stubGlobal("fetch", fetchMock);

    const { user } = renderWithProviders(<PingPanel department="OPD" />);
    await screen.findByText("first ping");
    expect(screen.queryByText("second ping")).not.toBeInTheDocument();

    const loadMore = screen.getByRole("button", { name: "Load more" });
    await user.click(loadMore);

    await waitFor(() => expect(screen.getByText("second ping")).toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });
});
