"use client";

import { useState } from "react";
import { Button } from "@tesserix/web";
import { Panel, RequestFailure, RefreshButton, formatDateTime } from "@helivanta/ui";
import {
  useGovernanceNotices,
  governanceTemplates,
  useGovernanceGrant,
  type GovernanceRequestScope,
  type GovernanceGrantReceipt,
} from "@helivanta/api";

const statusLabels: Record<GovernanceGrantReceipt["status"], string> = {
  pending_review: "Awaiting independent review",
  provisional_active: "Provisional; independent review required",
  active: "Independently approved",
  rejected: "Rejected",
  revoked: "Revoked",
};

export function GovernanceInbox({ scope }: { scope: GovernanceRequestScope }) {
  return <Inbox key={`${scope.tenantId}:${scope.subject}`} scope={scope} />;
}

function Inbox({ scope }: { scope: GovernanceRequestScope }) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const query = useGovernanceNotices(scope, cursor);
  return (
    <Panel className="my-5 space-y-4">
      <h3 className="text-lg font-semibold">Governance notices</h3>
      <p>
        Review changes to staff reviewer authority. A notice records a past change; current access
        is checked separately.
      </p>
      {query.isPending ? (
        <p role="status">Loading governance notices…</p>
      ) : query.isError ? (
        <RequestFailure
          message="Could not verify your governance inbox."
          error={query.error}
          retry={query.refetch}
        />
      ) : (
        <>
          {query.data.data.length === 0 ? (
            <p>No governance notices.</p>
          ) : (
            <ul className="space-y-3">
              {query.data.data.map((notice) => (
                <Panel as="li" key={notice.id} className="space-y-2">
                  <p>Notice recorded for grant version {notice.grant_version}</p>
                  <p className="text-sm text-muted-foreground">
                    {formatDateTime(notice.created_at)}
                  </p>
                  <Button
                    variant="outline"
                    disabled={query.isFetching}
                    onClick={() => setSelected(notice.grant_id)}
                  >
                    View current grant
                  </Button>
                </Panel>
              ))}
            </ul>
          )}
          <div className="flex gap-2">
            <RefreshButton
              error={query.error}
              busy={query.isFetching}
              refresh={() => {
                setSelected(null);
                if (cursor !== null) setCursor(null);
                else void query.refetch();
              }}
            >
              Refresh notices
            </RefreshButton>
            {query.data.page.has_more && (
              <Button
                variant="outline"
                disabled={query.isFetching}
                onClick={() => {
                  setSelected(null);
                  setCursor(query.data.page.next_cursor);
                }}
              >
                Next notices
              </Button>
            )}
          </div>
        </>
      )}
      {selected && !query.isError && <CurrentGrant key={selected} scope={scope} id={selected} />}
    </Panel>
  );
}

function CurrentGrant({ scope, id }: { scope: GovernanceRequestScope; id: string }) {
  const query = useGovernanceGrant(scope, id);
  if (query.isPending) return <p role="status">Checking current grant…</p>;
  if (query.isError)
    return (
      <RequestFailure
        message="Could not verify this grant."
        error={query.error}
        retry={query.refetch}
      />
    );
  const grant = query.data;
  return (
    <Panel as="section" aria-label="Current governance grant" className="space-y-2">
      <h4 className="font-semibold">{statusLabels[grant.status]}</h4>
      <p>Current version {grant.version}</p>
      <dl className="grid gap-2 sm:grid-cols-2">
        <dt>Role</dt>
        <dd>
          {governanceTemplates.find((template) => template.key === grant.input.template_key)?.name}
        </dd>
        <dt>Account</dt>
        <dd>{grant.input.subject}</dd>
        <dt>Scope</dt>
        <dd>{grant.input.scope_kind}</dd>
        {grant.input.scope_id && (
          <>
            <dt>Employer reference</dt>
            <dd>{grant.input.scope_id}</dd>
          </>
        )}
        <dt>Reason</dt>
        <dd>{grant.input.reason}</dd>
        <dt>Starts</dt>
        <dd>{formatDateTime(grant.input.valid_from)}</dd>
        <dt>Expires</dt>
        <dd>{formatDateTime(grant.input.expires_at)}</dd>
      </dl>
      <p className="text-sm">
        Stored approval does not establish effective staff or pharmacy access.
      </p>
      <RefreshButton error={query.error} busy={query.isFetching} refresh={query.refetch}>
        Refresh current grant
      </RefreshButton>
    </Panel>
  );
}
