import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { useLoginLinks } from "./login-link-queries";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  hospitalId: "22222222-2222-4222-8222-222222222222",
  staffId: "33333333-3333-4333-8333-333333333333",
};
const changed = "44444444-4444-4444-8444-444444444444";
const link = {
  id: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa",
  staff_id: scope.staffId,
  subject: "private-original-account",
  status: "pending",
  version: 1,
  proposed_by_subject: "proposer",
  actor_subject: "proposer",
  reason: "Verified account",
  proposal_reason: "Verified account",
  created_at: "2026-10-26T00:00:00Z",
  updated_at: "2026-10-26T00:00:00Z",
};
afterEach(() => vi.unstubAllGlobals());

it.each(["tenantId", "hospitalId", "staffId", "subject"] as const)(
  "does not display cached private links after %s changes",
  async (field) => {
    let respond: ((response: Response) => void) | undefined;
    const envelope = (row: typeof link) => ({
      data: [row],
      page: { has_more: false, next_cursor: null },
    });
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValueOnce(Response.json(envelope(link)))
        .mockImplementation(
          () =>
            new Promise<Response>((resolve) => {
              respond = resolve;
            }),
        ),
    );
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    );
    const { result, rerender, unmount } = renderHook(
      ({ scope, subject }) => useLoginLinks(scope, subject),
      {
        wrapper,
        initialProps: { scope, subject: "reviewer" },
      },
    );
    await waitFor(() =>
      expect(result.current.data?.pages[0]?.data[0]?.subject).toBe("private-original-account"),
    );
    const nextScope = field === "subject" ? scope : { ...scope, [field]: changed };
    rerender({ scope: nextScope, subject: field === "subject" ? "another-reviewer" : "reviewer" });
    expect(result.current.data).toBeUndefined();
    await waitFor(() => expect(respond).toBeTypeOf("function"));
    respond!(
      Response.json(
        envelope({ ...link, staff_id: nextScope.staffId, subject: "newly-authorized-account" }),
      ),
    );
    await waitFor(() =>
      expect(result.current.data?.pages[0]?.data[0]?.subject).toBe("newly-authorized-account"),
    );
    unmount();
    client.clear();
  },
);
