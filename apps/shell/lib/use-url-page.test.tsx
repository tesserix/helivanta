import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useStepBackWhenEmpty, useUrlPage } from "./use-url-page";

const nav = vi.hoisted(() => ({ search: "", replace: vi.fn() }));
vi.mock("next/navigation", () => ({
  usePathname: () => "/pharmacy/accounts",
  useSearchParams: () => new URLSearchParams(nav.search),
  useRouter: () => ({ replace: nav.replace }),
}));

afterEach(() => {
  nav.search = "";
  nav.replace.mockReset();
});

describe("useUrlPage", () => {
  // #1303 E8: a list's links stay at the pharmacy and outlet in the address.
  it("keeps the pharmacy and outlet in every link", () => {
    nav.search = "tab=all&pharmacy=P1&outlet=O1&page=2&cursor=c2";
    const { result } = renderHook(() => useUrlPage({ tab: "all" }));
    expect(result.current.hrefFor({ tab: "overdue" })).toBe(
      "/pharmacy/accounts?tab=overdue&pharmacy=P1&outlet=O1",
    );
    expect(result.current.first).toBe("/pharmacy/accounts?tab=all&pharmacy=P1&outlet=O1");
    expect(result.current.nextHref("c3")).toBe(
      "/pharmacy/accounts?tab=all&pharmacy=P1&outlet=O1&page=3&cursor=c3",
    );
  });

  it("reads page 1 with no cursor by default", () => {
    const { result } = renderHook(() => useUrlPage({ tab: "all" }));
    expect(result.current.page).toBe(1);
    expect(result.current.cursor).toBeNull();
    expect(result.current.first).toBe("/pharmacy/accounts?tab=all");
    expect(result.current.previousHref).toBeNull();
  });

  it("links the next page with its cursor, keeping the list", () => {
    const { result } = renderHook(() => useUrlPage({ tab: "overdue", q: "" }));
    expect(result.current.nextHref("c2")).toBe("/pharmacy/accounts?tab=overdue&page=2&cursor=c2");
    expect(result.current.nextHref(null)).toBeNull();
  });

  // Visited this session: Previous steps back to the cursor that opened the
  // page before (#1295 D4).
  it("steps back through the pages visited", () => {
    const { result, rerender } = renderHook(() => useUrlPage({ tab: "all" }));
    nav.search = "tab=all&page=2&cursor=c2";
    rerender();
    expect(result.current.previousHref).toBe("/pharmacy/accounts?tab=all");
    nav.search = "tab=all&page=3&cursor=c3";
    rerender();
    expect(result.current.previousHref).toBe("/pharmacy/accounts?tab=all&page=2&cursor=c2");
  });

  // After a reload the way back is unknown: the pager offers First page (D5).
  // Page 1 needs no cursor, so from page 2 Previous is still known.
  it("has no Previous after a reload beyond page 2", () => {
    nav.search = "tab=all&page=3&cursor=c3";
    const { result } = renderHook(() => useUrlPage({ tab: "all" }));
    expect(result.current.page).toBe(3);
    expect(result.current.cursor).toBe("c3");
    expect(result.current.previousHref).toBeNull();
  });

  it("opens another list at page 1, leaving out empty values", () => {
    nav.search = "tab=all&page=2&cursor=c2";
    const { result } = renderHook(() => useUrlPage({ tab: "all" }));
    expect(result.current.hrefFor({ tab: "week", q: "" })).toBe("/pharmacy/accounts?tab=week");
  });

  it("ignores a cursor on page 1", () => {
    nav.search = "tab=all&cursor=c9";
    const { result } = renderHook(() => useUrlPage({ tab: "all" }));
    expect(result.current.cursor).toBeNull();
  });
});

describe("useStepBackWhenEmpty", () => {
  it("steps back when an open later page empties", () => {
    nav.search = "tab=all&page=2&cursor=c2";
    renderHook(() => {
      const url = useUrlPage({ tab: "all" });
      return useStepBackWhenEmpty({ isPending: false, isError: false, items: [] }, url);
    });
    expect(nav.replace).toHaveBeenCalledWith("/pharmacy/accounts?tab=all");
  });

  it("leaves an empty first page alone", () => {
    renderHook(() => {
      const url = useUrlPage({ tab: "all" });
      return useStepBackWhenEmpty({ isPending: false, isError: false, items: [] }, url);
    });
    expect(nav.replace).not.toHaveBeenCalled();
  });
});
