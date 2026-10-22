"use client";

import { keepPreviousData } from "@tanstack/react-query";
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
 * more. The previous rows stay on screen while the next page loads.
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
  const query = useApiQuery<Page<T>>([...key, path, limit, cursor], url, {
    poll: opts.poll,
    placeholderData: keepPreviousData,
  });
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
