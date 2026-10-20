import { useApiMutation, apiFetch, type RosterChangeResult } from "@helivanta/api";

export function ReviewCheck({
  id,
  error,
  children,
  register,
}: {
  id: string;
  error?: string;
  children: React.ReactNode;
  register: object;
}) {
  return (
    <div className="rounded-xl border bg-muted/30 p-4">
      <label className="flex items-start gap-3 text-sm">
        <input
          type="checkbox"
          className="mt-0.5 size-4 shrink-0 accent-(--hms-accent)"
          aria-describedby={error ? `${id}-error` : undefined}
          {...register}
        />
        {children}
      </label>
      {error && (
        <p id={`${id}-error`} className="mt-2 text-xs text-destructive">
          {error}
        </p>
      )}
    </div>
  );
}

export function SaveError({ message }: { message: string }) {
  return (
    <p
      role="alert"
      className="rounded-xl border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive"
    >
      {message} Your input is preserved.
    </p>
  );
}

// Every roster write returns the new head and revision and refreshes every roster query for the hospital.
export function useRosterWrite<T>(
  path: string,
  hospitalID: string,
  successToast: string,
  onSuccess: (result: RosterChangeResult) => void,
) {
  return useApiMutation(
    (input: T) =>
      apiFetch<RosterChangeResult>(path, { method: "POST", body: JSON.stringify(input) }),
    {
      invalidate: [["workforce", "rosters", hospitalID]],
      successToast,
      suppressErrorToast: true,
      onSuccess,
    },
  );
}
