import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { useApiPagedQuery } from "./paged";

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

afterEach(() => vi.unstubAllGlobals());

describe("useApiPagedQuery", () => {
  it("appends the next page and stops when has_more is false", async () => {
    const pages = [
      { data: [{ id: "1" }, { id: "2" }], page: { next_cursor: "c1", has_more: true } },
      { data: [{ id: "3" }], page: { next_cursor: null, has_more: false } },
    ];
    const fetchMock = vi
      .fn()
      .mockImplementation((url: string) =>
        Promise.resolve(jsonResponse(url.includes("cursor=c1") ? pages[1] : pages[0])),
      );
    vi.stubGlobal("fetch", fetchMock);

    const { result } = renderHook(() => useApiPagedQuery<{ id: string }>(["rows"], "/rows"), {
      wrapper,
    });

    await waitFor(() => expect(result.current.items).toHaveLength(2));
    expect(result.current.hasMore).toBe(true);

    act(() => result.current.loadMore());

    await waitFor(() => expect(result.current.items).toHaveLength(3));
    expect(result.current.items.map((r) => r.id)).toEqual(["1", "2", "3"]);
    expect(result.current.hasMore).toBe(false);
  });

  it("does not duplicate rows when loadMore is called twice in a row", async () => {
    // A double-click on a slow connection. The second call must not fetch
    // the same cursor again and append the same page twice — which is
    // exactly the duplicate-row symptom #816 exists to remove, arriving
    // from the client side instead of the query.
    let resolveSecond: (v: unknown) => void = () => {};
    const secondInFlight = new Promise((r) => (resolveSecond = r));

    const fetchMock = vi.fn().mockImplementation(async (url: string) => {
      if (url.includes("cursor=c1")) {
        await secondInFlight;
        return jsonResponse({ data: [{ id: "3" }], page: { next_cursor: null, has_more: false } });
      }
      return jsonResponse({
        data: [{ id: "1" }, { id: "2" }],
        page: { next_cursor: "c1", has_more: true },
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    const { result } = renderHook(() => useApiPagedQuery<{ id: string }>(["rows"], "/rows"), {
      wrapper,
    });
    await waitFor(() => expect(result.current.items).toHaveLength(2));

    act(() => result.current.loadMore());
    act(() => result.current.loadMore()); // second click while the first is in flight
    act(() => resolveSecond(null));

    await waitFor(() => expect(result.current.hasMore).toBe(false));
    expect(result.current.items.map((r) => r.id)).toEqual(["1", "2", "3"]);

    const cursorCalls = fetchMock.mock.calls.filter(([url]) => String(url).includes("cursor=c1"));
    expect(cursorCalls).toHaveLength(1);
  });

  it("reports no more pages when the final page is exactly full", async () => {
    // The exact-boundary case: a full final page (length === limit) must
    // still report hasMore=false, because has_more is server-driven, not
    // inferred from "was the last page full". A full final page and a
    // truncated one are indistinguishable by length alone — that
    // indistinguishability is the defect #816 exists to fix, so
    // getNextPageParam must never re-derive it from page length.
    const fullFinalPage = {
      data: Array.from({ length: 50 }, (_, i) => ({ id: String(i) })),
      page: { next_cursor: null, has_more: false },
    };
    const fetchMock = vi.fn().mockResolvedValue(jsonResponse(fullFinalPage));
    vi.stubGlobal("fetch", fetchMock);

    const { result } = renderHook(() => useApiPagedQuery<{ id: string }>(["rows"], "/rows"), {
      wrapper,
    });

    await waitFor(() => expect(result.current.items).toHaveLength(50));
    expect(result.current.hasMore).toBe(false);
  });
});
