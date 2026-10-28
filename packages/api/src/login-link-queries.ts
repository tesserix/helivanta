"use client";
import { useInfiniteQuery } from "@tanstack/react-query";
import {
  fetchLoginLinks,
  fetchLoginLinkHistory,
  loginLinksKey,
  type LoginLinkScope,
} from "./login-links";

export function useLoginLinks(scope: LoginLinkScope, subject: string) {
  return useInfiniteQuery({
    queryKey: [...loginLinksKey(scope), subject, "list"],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) => fetchLoginLinks(scope, { cursor: pageParam, signal }),
    getNextPageParam: (page) => (page.page.has_more ? page.page.next_cursor : undefined),
    staleTime: 0,
    gcTime: 0,
  });
}

export function useLoginLinkHistory(scope: LoginLinkScope, id: string, subject: string) {
  return useInfiniteQuery({
    queryKey: [...loginLinksKey(scope), subject, id, "history"],
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam, signal }) =>
      fetchLoginLinkHistory(scope, id, { cursor: pageParam, signal }),
    getNextPageParam: (page) => (page.page.has_more ? page.page.next_cursor : undefined),
    staleTime: 0,
    gcTime: 0,
  });
}
