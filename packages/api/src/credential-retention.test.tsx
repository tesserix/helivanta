import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { expect, it, vi } from "vitest";
import { useApiMutation } from "./hooks";

it("removes a finished credential mutation as soon as its observer resets", async () => {
  vi.useFakeTimers();
  const client = new QueryClient();
  try {
    const options = { gcTime: 0, suppressErrorToast: true };
    const { result, unmount } = renderHook(
      () => useApiMutation(async (proof: { code: string }) => proof.code.length > 0, options),
      {
        wrapper: ({ children }: { children: ReactNode }) => (
          <QueryClientProvider client={client}>{children}</QueryClientProvider>
        ),
      },
    );
    await act(async () => {
      await result.current.mutateAsync({ code: "synthetic-native-proof" });
    });
    act(() => result.current.reset());
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(client.getMutationCache().getAll()).toHaveLength(0);
    unmount();
  } finally {
    client.clear();
    vi.useRealTimers();
  }
});
