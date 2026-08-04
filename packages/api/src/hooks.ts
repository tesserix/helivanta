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

export function useApiQuery<T>(
  key: unknown[],
  path: string,
  opts: { poll?: boolean } = {},
): UseQueryResult<T, Error> {
  return useQuery({
    queryKey: key,
    queryFn: () => apiFetch<T>(path),
    refetchInterval: opts.poll ? POLL_INTERVAL_MS : undefined,
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
