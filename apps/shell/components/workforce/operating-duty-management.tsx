"use client";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button } from "@tesserix/web";
import { Panel, RequestFailure, RefreshButton, useConfirmLeave } from "@helivanta/ui";
import {
  listOperatingDutyInbox,
  operatingDutyInboxKey,
  POLL_INTERVAL_MS,
  usePermissions,
  type OperatingDutyInboxScope,
} from "@helivanta/api";
import { OperatingDutyReview } from "./independent-operating-duties";

type Props = {
  scope: OperatingDutyInboxScope;
  busy: boolean;
  setBusy: (value: boolean) => void;
  onBack: () => void;
  onClose: () => void;
  accessSuspended?: boolean;
};

export function OperatingDutyManagement({
  scope,
  busy,
  setBusy,
  onBack,
  onClose,
  accessSuspended = false,
}: Props) {
  const [cursor, setCursor] = useState<string | null>(null);
  const confirmLeave = useConfirmLeave();
  const { can, isLoading: loading } = usePermissions();
  const permitted = can("iam.governance.operating.manage");
  const query = useQuery({
    queryKey: [...operatingDutyInboxKey(scope), cursor],
    queryFn: ({ signal }) => listOperatingDutyInbox(scope, cursor, signal),
    enabled: !busy && !accessSuspended && !loading && permitted,
    retry: false,
    staleTime: 0,
    refetchInterval: busy ? false : POLL_INTERVAL_MS,
  });
  const suspended = accessSuspended || !permitted || query.isError;
  const changePage = (value: string | null) => {
    if (!busy) void confirmLeave(() => setCursor(value));
  };
  return (
    <Panel className="space-y-4">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-semibold">Existing operating duties</h2>
        <div className="flex gap-3">
          <Button variant="outline" disabled={busy} onClick={onBack}>
            Choose reviewed Staff
          </Button>
          <Button variant="outline" disabled={busy} onClick={onClose}>
            Change employer
          </Button>
        </div>
      </header>
      <p className="text-sm text-muted-foreground">
        Manage retained duties, including Staff whose eligibility has ended. These records do not
        establish current Staff eligibility. Use reviewed Staff to assign or approve a new duty.
      </p>
      {loading ? (
        <p role="status">Checking current management access…</p>
      ) : suspended && !busy ? (
        query.isError ? (
          <RequestFailure
            message="Existing duties could not be verified."
            error={query.error}
            retry={query.refetch}
          />
        ) : (
          <p role="alert">
            Current employer management access is unavailable. Refresh your employer access before
            continuing.
          </p>
        )
      ) : (
        <>
          <RefreshButton busy={busy} refresh={query.refetch} error={query.error}>
            Refresh existing duties
          </RefreshButton>
          {!query.data ? (
            <p role="status">Loading existing duties…</p>
          ) : (
            <>
              {query.data.data.length === 0 && <p>No retained duties on this page.</p>}
              {query.data.data.map((row) => (
                <section key={row.id} className="space-y-2">
                  <OperatingDutyReview
                    scope={{ ...scope, staffId: row.input.identity.staff_id }}
                    row={row}
                    busy={busy}
                    setBusy={setBusy}
                    accessSuspended={suspended}
                    canReview={false}
                    canPrepare={!suspended}
                    canRevoke={!suspended}
                  />
                </section>
              ))}
              <nav aria-label="Existing duty pages" className="flex gap-3">
                {cursor && (
                  <Button variant="outline" disabled={busy} onClick={() => changePage(null)}>
                    First existing duties page
                  </Button>
                )}
                {query.data.page.has_more && (
                  <Button
                    variant="outline"
                    disabled={busy}
                    onClick={() => changePage(query.data.page.next_cursor)}
                  >
                    Next existing duties page
                  </Button>
                )}
              </nav>
            </>
          )}
        </>
      )}
    </Panel>
  );
}
