"use client";

import { useInfiniteQuery } from "@tanstack/react-query";
import { useRef } from "react";
import { POLL_INTERVAL_MS, apiFetch } from "./client";

/** The paginated envelope every collection endpoint returns (#816). */
export type Page<T> = {
  data: T[];
  page: { next_cursor: string | null; has_more: boolean };
};

export type UseApiPagedQueryResult<T> = {
  items: T[];
  hasMore: boolean;
  loadMore: () => void;
  isPending: boolean;
  isFetchingMore: boolean;
};

/**
 * useApiPagedQuery reads a cursor-paginated collection, accumulating pages
 * as the caller asks for more.
 *
 * A sibling of useApiQuery rather than a replacement: useApiQuery is a thin
 * useQuery wrapper used by every panel and by permissions.tsx, and reshaping
 * it to useInfiniteQuery would change the contract for all of them.
 *
 * `hasMore` comes from the server's explicit `has_more` flag, never from
 * "was the last page full" — a full final page is indistinguishable from a
 * truncated one by length alone, which is the defect #816 exists to fix.
 *
 * `opts.poll`, like useApiQuery's, refetches at POLL_INTERVAL_MS instead of
 * a hand-written setInterval — queue screens (dispense, lab orders) rely on
 * this to stay current without a manual refresh. An infinite query's poll
 * refetches every page already fetched, not just the first, so a clinician
 * who has paged past page one still sees live updates on every row they're
 * looking at.
 */
export function useApiPagedQuery<T>(
  key: unknown[],
  path: string,
  opts: { poll?: boolean } = {},
): UseApiPagedQueryResult<T> {
  const query = useInfiniteQuery({
    queryKey: key,
    initialPageParam: null as string | null,
    queryFn: ({ pageParam }) =>
      apiFetch<Page<T>>(pageParam ? `${path}?cursor=${encodeURIComponent(pageParam)}` : path),
    getNextPageParam: (last) => (last.page.has_more ? last.page.next_cursor : undefined),
    refetchInterval: opts.poll ? POLL_INTERVAL_MS : undefined,
  });

  // A ref, not query.isFetchingNextPage, guards the double-call: React's
  // state update from the first fetchNextPage() is not necessarily flushed
  // by the time a second click (or a fast double loadMore()) runs, so a
  // guard reading query.isFetchingNextPage can observe stale "false" on
  // both calls and let the same next page fetch twice. The ref is set
  // synchronously inside loadMore itself, independent of React's render
  // cycle, so the second call always sees the first one's in-flight state.
  const fetchingRef = useRef(false);

  return {
    items: query.data?.pages.flatMap((p) => p.data) ?? [],
    hasMore: query.hasNextPage,
    loadMore: () => {
      if (!query.hasNextPage || fetchingRef.current) return;
      fetchingRef.current = true;
      void appendNextPage(query.fetchNextPage, query.data?.pages.length ?? 0).finally(() => {
        fetchingRef.current = false;
      });
    },
    isPending: query.isPending,
    isFetchingMore: query.isFetchingNextPage,
  };
}

/** How many times appendNextPage will wait out a refetch before giving up. */
const MAX_JOINED_REFETCHES = 3;

/**
 * appendNextPage fetches the page after the CURRENT first pages, never after
 * a stale copy of them (#833).
 *
 * A create invalidates the list, so the pages already shown are refetched.
 * TanStack's fetchNextPage defaults to `cancelRefetch: true`. A Load more
 * click during that refetch therefore cancelled it, threw away the fresh
 * page one, and fetched the next page from the stale page one's cursor. The
 * newest row was then on no page at all: a clinician could page through a
 * whole ward list and never see the patient just registered.
 *
 * With `cancelRefetch: false`, a fetch already in flight is JOINED: the call
 * resolves when the refetch does, without fetching a next page. So the page
 * count is compared. If no page was added and there is still more, the
 * refetch has just finished with fresh pages, and asking again now follows
 * the fresh cursor. The loop is bounded: a refetch landing between every
 * attempt would need a poll faster than a page fetch, and on that path the
 * click simply has no effect rather than showing stale rows.
 */
async function appendNextPage(
  fetchNextPage: (opts: {
    cancelRefetch: boolean;
  }) => Promise<{ data?: { pages: unknown[] }; hasNextPage: boolean; isError: boolean }>,
  pagesBefore: number,
): Promise<void> {
  for (let attempt = 0; attempt < MAX_JOINED_REFETCHES; attempt++) {
    const result = await fetchNextPage({ cancelRefetch: false });
    const added = (result.data?.pages.length ?? 0) > pagesBefore;
    if (added || !result.hasNextPage || result.isError) return;
  }
}
