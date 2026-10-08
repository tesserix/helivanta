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

  it("never shows a page appended to a stale first page when Load more races a refetch (#833)", async () => {
    // A visit is created (the mutation invalidates the list, so page one is
    // refetched) and the clinician clicks Load more while that refetch is
    // still in flight. TanStack's fetchNextPage defaults to
    // cancelRefetch: true, which CANCELS the in-flight refetch, discards
    // the fresh page one, and fetches the next page from the STALE page
    // one's cursor. The newest row is then never rendered on any page,
    // which is exactly the pagination.spec.ts flake.
    //
    // Before the create: page one is [b, c] -> cursor c1 -> [d].
    // After it:          page one is [a, b] -> cursor c2 -> [c, d].
    let created = false;
    let releaseRefetch: () => void = () => {};
    const refetchGate = new Promise<void>((r) => (releaseRefetch = r));
    const fetchMock = vi.fn().mockImplementation(async (url: string) => {
      if (url.includes("cursor=c1")) {
        return jsonResponse({ data: [{ id: "d" }], page: { next_cursor: null, has_more: false } });
      }
      if (url.includes("cursor=c2")) {
        return jsonResponse({
          data: [{ id: "c" }, { id: "d" }],
          page: { next_cursor: null, has_more: false },
        });
      }
      if (!created) {
        return jsonResponse({
          data: [{ id: "b" }, { id: "c" }],
          page: { next_cursor: "c1", has_more: true },
        });
      }
      await refetchGate; // the refetch of page one is slow
      return jsonResponse({
        data: [{ id: "a" }, { id: "b" }],
        page: { next_cursor: "c2", has_more: true },
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result } = renderHook(() => useApiPagedQuery<{ id: string }>(["rows"], "/rows"), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    });
    await waitFor(() => expect(result.current.items.map((r) => r.id)).toEqual(["b", "c"]));

    // The create lands and invalidates the list: page one starts refetching.
    created = true;
    act(() => void client.invalidateQueries({ queryKey: ["rows"] }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));

    // Load more is clicked while that refetch is still in flight.
    act(() => result.current.loadMore());
    act(() => releaseRefetch());

    await waitFor(() => expect(result.current.hasMore).toBe(false));
    // Every row exactly once, newest first: the fresh page one, then the
    // page that follows IT. The stale cursor c1 is never followed.
    expect(result.current.items.map((r) => r.id)).toEqual(["a", "b", "c", "d"]);
    expect(fetchMock.mock.calls.some(([url]) => String(url).includes("cursor=c1"))).toBe(false);
  });
});
