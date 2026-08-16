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
    // suppressErrorToast opts a caller OUT of the automatic
    // toast.error(error.message) below. Default false, because that
    // toast is the right behaviour for almost every mutation in this
    // codebase: it needs no per-component error UI to work.
    //
    // The one class of exception is a mutation that ALREADY renders its
    // own dedicated, more specific error surface next to the fields it
    // concerns — apps/shell/app/login/page.tsx's credential check is the
    // motivating case: it shows the API's shared refusal message in a
    // `role="alert"` paragraph next to the email/password fields (spec
    // D5/D6), which is what a screen reader user focused on the form
    // actually expects. Leaving the toast on top of that is not wrong,
    // exactly, but it is the same message shown twice through two
    // different channels for no reason, and review flagged it as
    // confusing rather than helpful. This flag exists so that caller can
    // suppress the toast WITHOUT losing every other useApiMutation
    // behaviour (success toast, cache invalidation, onSuccess) or
    // falling back to a hand-rolled useMutation that would drift from
    // this hook's shared contract.
    suppressErrorToast?: boolean;
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
      if (!opts.suppressErrorToast) toast.error(error.message);
    },
  });
}
