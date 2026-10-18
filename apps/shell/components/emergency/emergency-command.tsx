"use client";

import { useRef } from "react";
import { ApiError, apiFetch, useApiMutation } from "@helivanta/api";

export function useEmergencyCommand<T, R = unknown>(
  base: string,
  path: () => string,
  method: "PUT" | "POST",
  successToast: string,
  onSuccess?: (data: R) => void,
  invalidate: unknown[][] = [["emergency", "fleet", base]],
) {
  const attempt = useRef<T | null>(null);
  const mutation = useApiMutation(
    async (values: T) => {
      attempt.current ??= structuredClone(values);
      try {
        return await apiFetch<R>(`/emergency/bases/${base}/${path()}`, {
          method,
          body: JSON.stringify(attempt.current),
        });
      } catch (error) {
        if (error instanceof ApiError && [400, 413].includes(error.status)) attempt.current = null;
        throw error;
      }
    },
    {
      successToast,
      suppressErrorToast: true,
      invalidate,
      onSuccess: (data) => {
        attempt.current = null;
        onSuccess?.(data);
      },
    },
  );
  const uncertain =
    mutation.isError &&
    !(mutation.error instanceof ApiError && [400, 413].includes(mutation.error.status));
  return { ...mutation, frozen: mutation.isPending || uncertain, uncertain };
}
export function EmergencyCommandFailure({
  message,
  uncertain,
}: {
  message?: string;
  uncertain: boolean;
}) {
  return message ? (
    <p role="alert" className="text-sm text-destructive">
      {message}
      {uncertain &&
        " Retry the original command. Refresh the current record before starting a different command."}
    </p>
  ) : null;
}
