"use client";
import { useEffect, useState } from "react";
import { Button } from "@tesserix/web";
import { Panel, RefreshButton, LoadMore, RequestFailure, useConfirmLeave } from "@helivanta/ui";
import {
  useApiQuery,
  usePartnerGroups,
  useTenantTransition,
  type PermissionsResponse,
  type PartnerGroup,
  type PartnerGroupScope,
} from "@helivanta/api";
import { accessChanged } from "./errors";
import { TeamProposalForm } from "./proposal-form";
import { TeamDetail } from "./team-detail";
import { MemberTeams } from "./member-teams";
import { PartnerGrantDraftWorkspace } from "./grant-drafts";

export function PartnerTeamsWorkspace() {
  const switching = useTenantTransition();
  const [attempt, setAttempt] = useState(0);
  return switching ? (
    <p role="status">Switching partner team workspace…</p>
  ) : (
    <TeamAccount key={attempt} restart={() => setAttempt((v) => v + 1)} />
  );
}
function TeamAccount({ restart }: { restart: () => void }) {
  const [nonce] = useState(() => crypto.randomUUID());
  const [changed, setChanged] = useState(false);
  const [membership, setMembership] = useState(false);
  const [supplierAccess, setSupplierAccess] = useState(false);
  const confirm = useConfirmLeave();
  const identity = useApiQuery<PermissionsResponse>(
    ["iam", "partner-team-account", nonce],
    "/iam/me/permissions",
    { gcTime: 0, staleTime: 0, refetchOnWindowFocus: false, request: { cache: "no-store" } },
  );
  if (changed)
    return (
      <section role="alert" className="space-y-3">
        <p>Partner team access or your account changed. Reopen to verify current access.</p>
        <Button onClick={restart}>Reopen partner teams</Button>
      </section>
    );
  if (identity.isPending) return <p role="status">Checking partner team account…</p>;
  if (
    identity.isError ||
    !identity.data?.tenant_id ||
    !identity.data.subject ||
    !Array.isArray(identity.data.data)
  )
    return (
      <RequestFailure
        message="Your partner team account could not be verified."
        error={identity.error}
        retry={identity.refetch}
      />
    );
  const scope = { tenantId: identity.data.tenant_id, subject: identity.data.subject };
  const canGovern = identity.data.data.includes("iam.assignment.manage");
  const canMembers = identity.data.data.includes("iam.partner_group.members.read");
  if (canGovern && supplierAccess)
    return (
      <>
        <Button variant="outline" onClick={() => confirm(() => setSupplierAccess(false))}>
          Back to hospital teams
        </Button>
        <PartnerGrantDraftWorkspace
          key={`${scope.tenantId}:${scope.subject}`}
          scope={scope}
          onChanged={() => setChanged(true)}
        />
      </>
    );
  if (!canGovern || membership)
    return <MemberTeams scope={scope} onChanged={() => setChanged(true)} />;
  return (
    <>
      <Button variant="outline" onClick={() => confirm(() => setSupplierAccess(true))}>
        Supplier access preparation
      </Button>
      {canMembers && (
        <Button variant="outline" onClick={() => confirm(() => setMembership(true))}>
          Manage team memberships
        </Button>
      )}
      <TeamList
        key={`${scope.tenantId}:${scope.subject}`}
        scope={scope}
        canReview={identity.data.data.includes("iam.assignment.review")}
        onChanged={() => setChanged(true)}
      />
    </>
  );
}
function TeamList({
  scope,
  canReview,
  onChanged,
}: {
  scope: PartnerGroupScope;
  canReview: boolean;
  onChanged: () => void;
}) {
  const teams = usePartnerGroups(scope);
  const [selection, setSelection] = useState<string | null>(null);
  const [preparing, setPreparing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const confirm = useConfirmLeave();
  useEffect(() => {
    if (accessChanged(teams.error)) onChanged();
  }, [teams.error, onChanged]);
  const saved = (group: PartnerGroup, recovered = false) => {
    setPreparing(false);
    setSelection(group.id);
    setBusy(false);
    setMessage(
      recovered
        ? "Team proposal recovered. Hospital verification and independent review still apply."
        : "Team proposal saved. Hospital verification and independent review still apply.",
    );
  };
  const rows = teams.data?.pages.flatMap((page) => page.data) ?? [];
  return (
    <section aria-label="Hospital partner teams" className="space-y-5">
      <p className="max-w-3xl text-sm text-muted-foreground">
        A team alone does not share supplier records. Each supplier relationship and its actions,
        locations, fields and expiry require their own reviewed grants.
      </p>
      {message && <p role="status">{message}</p>}
      <div className="flex flex-wrap gap-3">
        <Button
          disabled={busy || preparing}
          onClick={() =>
            confirm(() => {
              setSelection(null);
              setMessage("");
              setPreparing(true);
            })
          }
        >
          Prepare a team
        </Button>
        <RefreshButton
          error={teams.error}
          busy={busy || teams.isFetching}
          refresh={() => teams.refetch()}
        >
          Refresh teams
        </RefreshButton>
      </div>
      {preparing && (
        <TeamProposalForm
          scope={scope}
          onSaved={saved}
          onChanged={onChanged}
          onBusyChanged={setBusy}
          onCancel={() =>
            confirm(() => {
              setPreparing(false);
              setBusy(false);
            })
          }
        />
      )}
      {teams.isPending ? (
        <p role="status">Loading partner teams…</p>
      ) : teams.isError ? (
        <RequestFailure
          message="Partner teams are unavailable. Refresh before continuing."
          error={teams.error}
          retry={teams.refetch}
        />
      ) : (
        <>
          {!rows.length && <p>No partner teams have been prepared for this account.</p>}
          <ul className="grid gap-3 md:grid-cols-2">
            {rows.map((group) => (
              <Panel as="li" key={group.id} className="space-y-2">
                <p className="font-medium">{group.name}</p>
                <p className="text-sm text-muted-foreground">{group.purpose}</p>
                <Button
                  variant="outline"
                  disabled={busy}
                  onClick={() =>
                    confirm(() => {
                      setPreparing(false);
                      setMessage("");
                      setSelection(group.id);
                    })
                  }
                >
                  Open {group.name}
                </Button>
              </Panel>
            ))}
          </ul>
          <fieldset disabled={busy}>
            <LoadMore
              hasMore={teams.hasNextPage}
              isLoading={teams.isFetchingNextPage || teams.isFetching}
              onClick={() => {
                void teams.fetchNextPage({ cancelRefetch: false });
              }}
              label="Load more teams"
            />
          </fieldset>
        </>
      )}
      {selection && !preparing && !teams.isError && (
        <TeamDetail
          key={`${scope.tenantId}:${scope.subject}:${selection}`}
          scope={scope}
          id={selection}
          canReview={canReview}
          busy={busy}
          onChanged={onChanged}
          onBusyChanged={setBusy}
        />
      )}
    </section>
  );
}
