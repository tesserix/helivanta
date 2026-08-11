"use client";

import {
  useMutation,
  useQuery,
  useQueryClient,
  type UseMutationResult,
  type UseQueryResult,
} from "@tanstack/react-query";
import { toast } from "sonner";
import { POLL_INTERVAL_MS, apiFetch } from "./client";

// React Query distinguishes a placeholder *value* from a placeholder
// *factory* by rejecting function-typed values, and expresses that with an
// internal conditional type it does not export. Mirroring the condition
// exactly is what lets a value of this type flow into `placeholderData`
// while `T` is still generic; for every response shape this package deals
// in (JSON objects and arrays) it resolves to plain `T`. The bare
// `Function` is deliberate and load-bearing: TypeScript only relates two
// unresolved conditional types when their branches are *identical*, so a
// narrower `(...args: never[]) => unknown` is rejected at the call site.
// eslint-disable-next-line @typescript-eslint/no-unsafe-function-type
type PlaceholderValue<T> = T extends Function ? never : T;

export function useApiQuery<T>(
  key: unknown[],
  path: string,
  opts: { poll?: boolean; placeholderData?: PlaceholderValue<T> } = {},
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
