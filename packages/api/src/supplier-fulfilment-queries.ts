import { useQuery } from "@tanstack/react-query";
import { POLL_INTERVAL_MS } from "./client";

export function useSupplierWorkflowQuery<T>(
  key: readonly unknown[],
  read: (signal: AbortSignal) => Promise<T>,
  poll = false,
) {
  return useQuery({
    queryKey: key,
    queryFn: ({ signal }) => read(signal),
    staleTime: 0,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: true,
    refetchInterval: poll ? POLL_INTERVAL_MS : false,
  });
}
