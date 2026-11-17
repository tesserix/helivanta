import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { releaseSnapshotForTest } from "./testing";
import { useReleaseCapabilities } from "./use-release-capabilities";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  localStorage.clear();
});

it.each(["healthy", "missed_poll", "outage"])(
  "refreshes aged snapshots and fails closed during an outage: %s",
  async (mode) => {
    vi.useFakeTimers();
    const scope = { tenantId: "11111111-1111-4111-8111-111111111111", subject: "synthetic-owner" };
    let reads = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        if (url.endsWith("/iam/me/permissions"))
          return Response.json({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["workforce.staff.read"],
          });
        if (url.endsWith("/workspace/release-capabilities")) {
          reads++;
          if ((reads === 2 && mode !== "healthy") || (reads > 2 && mode === "outage"))
            return Response.json({ error: "unavailable" }, { status: 503 });
          const value = releaseSnapshotForTest(scope, ["team_accounts", "staff_core"]);
          value.issued_at = new Date(Date.now() - 35_000).toISOString();
          value.expires_at = new Date(Date.now() + 25_000).toISOString();
          return Response.json(value);
        }
        throw new Error("Unexpected request");
      }),
    );
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { result, unmount } = renderHook(useReleaseCapabilities, {
      wrapper: ({ children }: { children: ReactNode }) => (
        <QueryClientProvider client={client}>{children}</QueryClientProvider>
      ),
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(result.current.available("staff_core")).toBe(true);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(26_000);
    });
    expect(reads).toBe(mode === "healthy" ? 2 : 3);
    expect(result.current.available("staff_core")).toBe(mode !== "outage");
    unmount();
    client.clear();
  },
);
