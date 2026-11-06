"use client";

import { useState } from "react";
import { Button } from "@tesserix/web";
import { RequestFailure, useConfirmLeave } from "@helivanta/ui";
import { useApiQuery, useTenantTransition, type PermissionsResponse } from "@helivanta/api";
import { DepartmentTeamRoster } from "./department-team-roster";
import { StaffPostingWorkspace } from "./posting-workspace";
import { StaffPostingReviewWorkspace } from "./posting-review-workspace";
import { StaffPostingCutoffWorkspace } from "./posting-cutoff-workspace";

const tasks = [
  { key: "team", label: "View current department teams" },
  { key: "prepare", label: "Prepare staff placements" },
  { key: "review", label: "Review heads and supervisors" },
  { key: "end", label: "End responsibilities" },
] as const;
type Task = (typeof tasks)[number]["key"];

export function DepartmentTeamWorkspace() {
  const switching = useTenantTransition();
  const [attempt, setAttempt] = useState(0);
  return switching ? (
    <p role="status">Switching department team workspace…</p>
  ) : (
    <TeamAccount key={attempt} restart={() => setAttempt((value) => value + 1)} />
  );
}

function TeamAccount({ restart }: { restart: () => void }) {
  const [nonce] = useState(() => crypto.randomUUID());
  const [task, setTask] = useState<Task | null>(null);
  const [busy, setBusy] = useState(false);
  const [changed, setChanged] = useState(false);
  const confirmLeave = useConfirmLeave();
  const identity = useApiQuery<PermissionsResponse>(
    ["workforce", "department-team-account", nonce],
    "/iam/me/permissions",
    { gcTime: 0, staleTime: 0, refetchOnWindowFocus: false, request: { cache: "no-store" } },
  );

  if (changed)
    return (
      <section role="alert" className="space-y-4">
        <p>Department team access or your account changed. Reopen to verify current access.</p>
        <Button onClick={restart}>Reopen department teams</Button>
      </section>
    );
  if (identity.isPending) return <p role="status">Checking department team account…</p>;
  if (identity.isError || !identity.data?.tenant_id || !identity.data.subject)
    return (
      <RequestFailure
        error={identity.error}
        message="Your department team account could not be verified."
        retry={identity.refetch}
      />
    );

  const scope = { tenantId: identity.data.tenant_id, subject: identity.data.subject };
  const onContextChanged = () => setChanged(true);
  return (
    <section className="space-y-6" aria-label="Department team management">
      <div role="group" aria-label="Department team tasks" className="flex flex-wrap gap-3">
        {tasks.map((item) => (
          <Button
            key={item.key}
            variant={task === item.key ? "default" : "outline"}
            aria-pressed={task === item.key}
            disabled={busy}
            onClick={() => {
              if (task !== item.key) confirmLeave(() => setTask(item.key));
            }}
          >
            {item.label}
          </Button>
        ))}
      </div>
      <p className="max-w-3xl text-sm text-muted-foreground">
        Choose a task to check your permitted hospitals and departments. Leadership responsibilities
        require independent review and do not grant clinical or access-administration privileges.
      </p>
      {!task && (
        <p>
          Prepare a staff placement, review a department head or supervisor appointment, or end an
          existing responsibility.
        </p>
      )}
      {task === "team" && (
        <DepartmentTeamRoster scope={scope} onContextChanged={onContextChanged} />
      )}
      {task === "prepare" && (
        <StaffPostingWorkspace scope={scope} onContextChanged={onContextChanged} />
      )}
      {task === "review" && (
        <StaffPostingReviewWorkspace
          scope={scope}
          onContextChanged={onContextChanged}
          onBusyChanged={setBusy}
        />
      )}
      {task === "end" && (
        <StaffPostingCutoffWorkspace
          scope={scope}
          onContextChanged={onContextChanged}
          onBusyChanged={setBusy}
        />
      )}
      <p className="max-w-3xl text-sm text-muted-foreground">
        Moving staff requires a reviewed replacement placement. Ending a responsibility alone does
        not complete a transfer.
      </p>
    </section>
  );
}
