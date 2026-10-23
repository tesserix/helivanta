import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { useApiPageQuery } from "./page-query";

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

afterEach(() => vi.unstubAllGlobals());

describe("useApiPageQuery", () => {
  it("asks for 25 rows from the cursor and reads the total", async () => {
    const fetcher = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          data: [{ id: "a" }],
          page: { has_more: true, next_cursor: "next", total: 41, offset: 25 },
        }),
        { headers: { "Content-Type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetcher);
    const { result } = renderHook(
      () => useApiPageQuery<{ id: string }>(["rows"], "/rows?x=1", "abc"),
      { wrapper },
    );
    await waitFor(() => expect(result.current.items).toEqual([{ id: "a" }]));
    expect(result.current.total).toBe(41);
    expect(result.current.offset).toBe(25);
    expect(result.current.nextCursor).toBe("next");
    expect(String(fetcher.mock.calls[0]![0])).toContain("/rows?x=1&limit=25&cursor=abc");
  });

  it("has no next cursor on the last page and no total when the list is uncounted", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ data: [], page: { has_more: false, next_cursor: null } }), {
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );
    const { result } = renderHook(() => useApiPageQuery(["rows"], "/rows", null), { wrapper });
    await waitFor(() => expect(result.current.isPending).toBe(false));
    expect(result.current.nextCursor).toBeNull();
    expect(result.current.total).toBeNull();
  });

  // The range is computed from the page number, so rows from the previous
  // page must never be shown under the next page's range (#1295).
  it("shows no rows from the previous page while the next one loads", async () => {
    let release: (r: Response) => void = () => {};
    const fetcher = vi
      .fn()
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            data: [{ id: "p1" }],
            page: { has_more: true, next_cursor: "c1", total: 2 },
          }),
          { headers: { "Content-Type": "application/json" } },
        ),
      )
      .mockImplementationOnce(() => new Promise<Response>((resolve) => (release = resolve)));
    vi.stubGlobal("fetch", fetcher);
    const { result, rerender } = renderHook(
      ({ cursor }: { cursor: string | null }) =>
        useApiPageQuery<{ id: string }>(["rows"], "/rows", cursor),
      { wrapper, initialProps: { cursor: null as string | null } },
    );
    await waitFor(() => expect(result.current.items).toEqual([{ id: "p1" }]));
    rerender({ cursor: "c1" });
    await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(2));
    expect(result.current.items).toEqual([]);
    expect(result.current.isPending).toBe(true);
    release(
      new Response(
        JSON.stringify({
          data: [{ id: "p2" }],
          page: { has_more: false, next_cursor: null, total: 2 },
        }),
        {
          headers: { "Content-Type": "application/json" },
        },
      ),
    );
    await waitFor(() => expect(result.current.items).toEqual([{ id: "p2" }]));
  });
});
