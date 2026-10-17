import { act, renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useNow } from "./use-now";

afterEach(() => vi.useRealTimers());

it("starts at the current time and advances on each refresh", () => {
  vi.useFakeTimers({ now: new Date("2026-10-17T00:00:00Z") });
  const { result } = renderHook(() => useNow(1_000));
  expect(result.current).toBe(Date.parse("2026-10-17T00:00:00Z"));
  act(() => vi.advanceTimersByTime(1_000));
  expect(result.current).toBe(Date.parse("2026-10-17T00:00:01Z"));
});
