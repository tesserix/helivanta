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
          page: { has_more: true, next_cursor: "next", total: 41 },
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
});
