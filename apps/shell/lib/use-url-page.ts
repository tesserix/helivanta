"use client";

import { useEffect, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

const query = (list: Record<string, string>) =>
  new URLSearchParams(Object.entries(list).filter(([, value]) => value !== ""));

/**
 * The page, cursor and links of a URL-paged list (#1295 D5). `list` is every
 * URL value that defines the list, its tab and filters: every link keeps
 * them, and the cursors visited are kept per list, since a cursor from one
 * filter means nothing in another. Empty values are left out of the URL.
 */
export function useUrlPage(list: Record<string, string>) {
  const pathname = usePathname();
  const params = useSearchParams();
  const page = Math.max(1, Number.parseInt(params.get("page") ?? "1", 10) || 1);
  const cursor = page > 1 ? params.get("cursor") : null;

  // The cursor that opened each page this visit. Not persisted: after a
  // reload the way back is unknown, and the pager says so. Recorded with
  // React's set-state-during-render pattern, so the record is in step with
  // the page being rendered.
  const key = query(list).toString();
  const [visited, setVisited] = useState<Record<string, (string | null)[]>>({});
  const pages = visited[key] ?? [null];
  if (pages[page - 1] !== cursor) {
    const next = [...pages];
    next[page - 1] = cursor;
    setVisited({ ...visited, [key]: next });
  }

  const href = (p: number, c: string | null) => {
    const search = query(list);
    if (p > 1 && c) {
      search.set("page", String(p));
      search.set("cursor", c);
    }
    return `${pathname}?${search}`;
  };
  const first = href(1, null);
  const back = page > 1 ? pages[page - 2] : undefined;
  return {
    page,
    cursor,
    first,
    /** Null after a reload on a later page: the Pager offers First page. */
    previousHref: back === undefined ? null : back === null ? first : href(page - 1, back),
    nextHref: (next: string | null) => (next ? href(page + 1, next) : null),
    /** Another list (a tab or a new filter), at its first page. */
    hrefFor: (other: Record<string, string>) => `${pathname}?${query(other)}`,
  };
}

/**
 * A later page can empty while open (its last row resolved elsewhere). Step
 * back rather than show "of N" over nothing (#1295 D6). True while stepping.
 */
export function useStepBackWhenEmpty(
  page: { isPending: boolean; isError: boolean; items: unknown[] },
  url: ReturnType<typeof useUrlPage>,
) {
  const router = useRouter();
  const params = useSearchParams();
  const emptied = !page.isPending && !page.isError && page.items.length === 0 && url.page > 1;
  // The list moving back by itself is not the person leaving an open record
  // (#1303 D4): carry it to the page stepped back to.
  const keep = new URLSearchParams();
  for (const key of ["open", "new"]) {
    const value = params.get(key);
    if (value) keep.set(key, value);
  }
  const back = url.previousHref ?? url.first;
  const target = keep.size ? `${back}${back.includes("?") ? "&" : "?"}${keep}` : back;
  useEffect(() => {
    if (emptied) router.replace(target);
  }, [emptied, target, router]);
  return emptied;
}
