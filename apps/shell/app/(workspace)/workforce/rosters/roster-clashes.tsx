"use client";
import { Button } from "@tesserix/web";
import {
  ApiError,
  useApiPagedQuery,
  type Hospital,
  type Roster,
  type RosterCredentialClash,
} from "@helivanta/api";
import { LoadMore } from "@helivanta/ui";
import { Failure } from "../roster-policy/policy-ui";

const STANDING = { expired: "Expired", not_yet_valid: "Not yet valid" } as const;

export function useRosterClashes(path: string, hospital: Hospital, roster: Roster, enabled = true) {
  return useApiPagedQuery<RosterCredentialClash>(
    ["workforce", "rosters", hospital.id, roster.id, "credential-clashes", roster.version],
    `${path}/credential-clashes?limit=100`,
    { enabled },
  );
}

// outstandingClashes reads the count from a freeze refused for unacknowledged clashes.
export function outstandingClashes(error: unknown): number | null {
  if (!(error instanceof ApiError) || error.detail.reason !== "credential_clashes") return null;
  const n = error.detail.outstanding;
  return typeof n === "number" && Number.isInteger(n) && n > 0 ? n : null;
}

export function ClashTable({ clashes }: { clashes: ReturnType<typeof useRosterClashes> }) {
  if (clashes.isPending) return <p role="status">Checking reviewed credentials…</p>;
  if (clashes.isError)
    return <Failure message="Could not check reviewed credentials." retry={clashes.retry} />;
  if (clashes.items.length === 0)
    return (
      <p className="text-sm text-muted-foreground">
        No current shift falls outside a reviewed credential&apos;s validity.
      </p>
    );
  return (
    <div className="space-y-3">
      <div className="overflow-x-auto">
        <table className="min-w-full text-left text-sm">
          <caption className="sr-only">Shifts outside reviewed credential validity</caption>
          <thead className="text-xs text-muted-foreground">
            <tr>
              <th scope="col" className="p-2">
                Staff
              </th>
              <th scope="col" className="p-2">
                Date
              </th>
              <th scope="col" className="p-2">
                Credential
              </th>
            </tr>
          </thead>
          <tbody>
            {clashes.items.map((clash) => (
              <tr key={clash.allocation_id} className="border-t align-top">
                <th scope="row" className="p-2 font-medium">
                  {clash.display_name} · {clash.employee_code}
                </th>
                <td className="p-2">{clash.roster_date}</td>
                <td className="p-2">
                  <ul className="space-y-1">
                    {clash.credentials.map((c) => (
                      <li key={c.credential_id}>
                        {c.type}: {STANDING[c.standing]} (valid {c.valid_from} to {c.valid_until})
                      </li>
                    ))}
                  </ul>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <LoadMore
        hasMore={clashes.hasMore}
        isLoading={clashes.isFetchingMore}
        onClick={clashes.loadMore}
      />
    </div>
  );
}

export function ClashPanel({
  path,
  hospital,
  roster,
  onClose,
}: {
  path: string;
  hospital: Hospital;
  roster: Roster;
  onClose: () => void;
}) {
  const clashes = useRosterClashes(path, hospital, roster);
  return (
    <section aria-label="Credential clashes" className="space-y-4 border-b p-5">
      <p className="text-sm text-muted-foreground">
        Shifts on dates outside a reviewed licence or registration. People stay booked; decide with
        them whether they work.
      </p>
      <ClashTable clashes={clashes} />
      <div className="flex justify-end">
        <Button variant="ghost" onClick={onClose}>
          Close clashes
        </Button>
      </div>
    </section>
  );
}
