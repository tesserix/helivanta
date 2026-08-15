"use client";

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryOptions,
  type UseQueryResult,
} from "@tanstack/react-query";
import { toast } from "sonner";
import { POLL_INTERVAL_MS, apiFetch } from "./client";

// Borrowed from React Query rather than restated: it guards `placeholderData`
// with a conditional type it does not export, so a hand-written `T` is
// rejected at the call site. Reading the option's type off `UseQueryOptions`
// keeps this in step with the library. For every response shape this package
// deals in (JSON objects and arrays) it resolves to plain `T`.
type PlaceholderValue<T> = NonNullable<UseQueryOptions<T, Error, T, unknown[]>["placeholderData"]>;

export function useApiQuery<T>(
  key: unknown[],
  path: string,
  opts: { poll?: boolean; placeholderData?: PlaceholderValue<T>; enabled?: boolean } = {},
): UseQueryResult<T, Error> {
  return useQuery({
    queryKey: key,
    queryFn: () => apiFetch<T>(path),
    refetchInterval: opts.poll ? POLL_INTERVAL_MS : undefined,
    // placeholderData, not initialData: it is shown while the request is
    // in flight but never written to the query cache, so a locally cached
    // value can never be mistaken for a server response or served to a
    // later consumer of the same key.
    placeholderData: opts.placeholderData,
    // enabled defaults to true (react-query's own default) so every
    // existing call site is unaffected. Added for callers that can only
    // decide whether a session exists to query against after render —
    // components/session-renewal.tsx on /login, specifically — where the
    // alternative (calling the hook conditionally) would break the Rules
    // of Hooks.
    enabled: opts.enabled,
  });
}

export function useApiMutation<TData, TVars = void>(
  fn: (vars: TVars) => Promise<TData>,
  opts: {
    successToast?: string;
    invalidate?: unknown[][];
    onSuccess?: (data: TData) => void;
  } = {},
): UseMutationResult<TData, Error, TVars> {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: fn,
    onSuccess: (data) => {
      if (opts.successToast) toast.success(opts.successToast);
      for (const key of opts.invalidate ?? []) {
        void queryClient.invalidateQueries({ queryKey: key });
      }
      opts.onSuccess?.(data);
    },
    onError: (error) => {
      toast.error(error.message);
    },
  });
}
