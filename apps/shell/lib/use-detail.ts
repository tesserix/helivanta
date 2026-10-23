"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";

// A record id: anything else in ?open= is a hand edit, and nothing is fetched for it.
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * The open record of a list page, kept in the URL beside its tab, filters
 * and page (#1303 D3), so a reload or a shared link reopens it. Opening from
 * a closed list is a Back step; switching rows replaces it, so twenty bills
 * do not take twenty Backs.
 */
export function useDetail() {
  const pathname = usePathname();
  const router = useRouter();
  const params = useSearchParams();
  const raw = params.get("open") ?? "";
  const openId = uuid.test(raw) ? raw : null;
  const creating = params.get("new") === "1";

  const with_ = (change: (search: URLSearchParams) => void) => {
    const search = new URLSearchParams(params);
    search.delete("open");
    search.delete("new");
    change(search);
    const query = search.toString();
    return query ? `${pathname}?${query}` : pathname;
  };
  const isOpen = openId !== null || creating;
  const openHref = (id: string) => with_((search) => search.set("open", id));

  return {
    openId,
    creating,
    openHref,
    open: (id: string) => (isOpen ? router.replace(openHref(id)) : router.push(openHref(id))),
    create: () => {
      const href = with_((search) => search.set("new", "1"));
      return isOpen ? router.replace(href) : router.push(href);
    },
    close: () => router.replace(with_(() => {})),
  };
}
