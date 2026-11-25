import { useApiMutation, apiFetch, type RosterChangeResult } from "@helivanta/api";
import { Panel } from "@helivanta/ui";

// One save failure across Workforce; re-exported so roster imports stay local.
export { SaveError } from "../leave-balances/years-panel";

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
    <Panel as="div">
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
    </Panel>
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
