import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useDetail } from "./use-detail";
import { useStepBackWhenEmpty, useUrlPage } from "./use-url-page";

const nav = vi.hoisted(() => ({ search: "", push: vi.fn(), replace: vi.fn() }));
vi.mock("next/navigation", () => ({
  usePathname: () => "/pharmacy/accounts",
  useSearchParams: () => new URLSearchParams(nav.search),
  useRouter: () => ({ push: nav.push, replace: nav.replace }),
}));

afterEach(() => {
  nav.search = "";
  nav.push.mockReset();
  nav.replace.mockReset();
});

const id = "0199e000-0000-7000-8000-000000000020";
const other = "0199e000-0000-7000-8000-000000000021";

describe("useDetail", () => {
  // #1303 E3: a stock position is a lot at a location; both name it.
  it("names a record by two ids when asked, and clears both together", () => {
    nav.search = `tab=all&open=${id}&at=${other}`;
    const both = renderHook(() => useDetail({ also: "at" })).result.current;
    expect(both.openId).toBe(`${id}:${other}`);
    expect(both.closeHref).toBe("/pharmacy/accounts?tab=all");
    expect(both.openHref(`${other}:${id}`)).toBe(
      `/pharmacy/accounts?tab=all&open=${other}&at=${id}`,
    );
    // Half a name is no record: nothing is fetched for it.
    nav.search = `open=${id}&at=nope`;
    expect(renderHook(() => useDetail({ also: "at" })).result.current.openId).toBeNull();
  });

  it("reads the open record and create from the URL", () => {
    nav.search = `tab=all&open=${id}`;
    expect(renderHook(() => useDetail()).result.current).toMatchObject({
      openId: id,
      creating: false,
    });
    nav.search = "tab=all&new=1";
    expect(renderHook(() => useDetail()).result.current).toMatchObject({
      openId: null,
      creating: true,
    });
  });

  // A hand-edited id is not a record: nothing is fetched for it.
  it("ignores an open value that is not a record id", () => {
    nav.search = "tab=all&open=../../etc";
    expect(renderHook(() => useDetail()).result.current.openId).toBeNull();
  });

  // #1303 D3: opening from a closed list is a Back step; switching rows is not.
  it("pushes when opening from closed and replaces when switching", () => {
    nav.search = "tab=overdue&page=2&cursor=c2";
    renderHook(() => useDetail()).result.current.open(id);
    expect(nav.push).toHaveBeenCalledWith(
      `/pharmacy/accounts?tab=overdue&page=2&cursor=c2&open=${id}`,
    );
    nav.search = `tab=overdue&page=2&cursor=c2&open=${id}`;
    renderHook(() => useDetail()).result.current.open(other);
    expect(nav.replace).toHaveBeenCalledWith(
      `/pharmacy/accounts?tab=overdue&page=2&cursor=c2&open=${other}`,
    );
  });

  it("creates as a Back step and closes keeping the list's place", () => {
    nav.search = "tab=all&page=2&cursor=c2";
    renderHook(() => useDetail()).result.current.create();
    expect(nav.push).toHaveBeenCalledWith("/pharmacy/accounts?tab=all&page=2&cursor=c2&new=1");
    nav.search = `tab=all&page=2&cursor=c2&open=${id}`;
    expect(renderHook(() => useDetail()).result.current.closeHref).toBe(
      "/pharmacy/accounts?tab=all&page=2&cursor=c2",
    );
    renderHook(() => useDetail()).result.current.close();
    expect(nav.replace).toHaveBeenCalledWith("/pharmacy/accounts?tab=all&page=2&cursor=c2");
  });
});

describe("paging with an open record", () => {
  // Changing tab, filter or page is a new list: the open record closes.
  it("drops the open record from another list's link", () => {
    nav.search = `tab=all&open=${id}`;
    const { result } = renderHook(() => useUrlPage({ tab: "all" }));
    expect(result.current.hrefFor({ tab: "week" })).toBe("/pharmacy/accounts?tab=week");
    expect(result.current.nextHref("c2")).toBe("/pharmacy/accounts?tab=all&page=2&cursor=c2");
  });

  // The list stepping back by itself is not the person leaving the record.
  it("keeps the open record when an emptied page steps back", () => {
    nav.search = `tab=all&page=2&cursor=c2&open=${id}`;
    renderHook(() => {
      const url = useUrlPage({ tab: "all" });
      return useStepBackWhenEmpty({ isPending: false, isError: false, items: [] }, url);
    });
    expect(nav.replace).toHaveBeenCalledWith(`/pharmacy/accounts?tab=all&open=${id}`);
  });
});
