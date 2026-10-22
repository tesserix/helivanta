"use client";

import { useApiQuery } from "./hooks";
import type { Page } from "./paged";

export const PAGE_SIZE = 25;

export type UseApiPageQueryResult<T> = {
  items: T[];
  /** The server's total for a counted list; null for an uncounted one. */
  total: number | null;
  nextCursor: string | null;
  isPending: boolean;
  isError: boolean;
  error: Error | null;
  retry: () => void;
};

/**
 * useApiPageQuery reads ONE page of a cursor-paginated list (#1295 D6), for
 * Previous / Next paging. useApiPagedQuery stays for lists still on Load
 * more. While a page loads it is pending: no rows from the page before.
 */
export function useApiPageQuery<T>(
  key: unknown[],
  path: string,
  cursor: string | null,
  opts: { poll?: boolean; limit?: number } = {},
): UseApiPageQueryResult<T> {
  const limit = opts.limit ?? PAGE_SIZE;
  const url =
    `${path}${path.includes("?") ? "&" : "?"}limit=${limit}` +
    (cursor ? `&cursor=${encodeURIComponent(cursor)}` : "");
  // No placeholder from the previous page: the range is computed from the
  // page number, so carried-over rows would sit under the wrong range.
  const query = useApiQuery<Page<T>>([...key, path, limit, cursor], url, { poll: opts.poll });
  return {
    items: query.data?.data ?? [],
    total: query.data?.page.total ?? null,
    nextCursor: query.data?.page.has_more ? query.data.page.next_cursor : null,
    isPending: query.isPending,
    isError: query.isError,
    error: query.error,
    retry: () => {
      void query.refetch();
    },
  };
}
