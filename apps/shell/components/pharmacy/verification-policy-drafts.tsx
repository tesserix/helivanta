"use client";

import { useEffect, useId, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  newPharmacyId,
  onTenantTransition,
  tenantTransitionPending,
  pharmacyKey,
  readVerificationPolicyDraft,
  saveVerificationPolicyDraft,
  useVerificationPolicyDrafts,
  VerificationPolicyDraftInputContract,
  type VerificationPolicyDraftInput,
  type VerificationPolicyDraftRevision,
  type VerificationPolicyScope,
} from "@helivanta/api";
import { Panel, RefreshButton, RequestFailure, SelectControl } from "@helivanta/ui";

type Attempt = { policy: string; revision: string; input: VerificationPolicyDraftInput };
export function VerificationPolicyDrafts({
  scope,
  timeZone,
}: {
  scope: VerificationPolicyScope;
  timeZone: string;
}) {
  return (
    <DraftWorkspace
      key={`${scope.tenantId}:${scope.subject}:${scope.pharmacyId}:${scope.outletId}`}
      scope={scope}
      timeZone={timeZone}
    />
  );
}
function DraftWorkspace({ scope, timeZone }: { scope: VerificationPolicyScope; timeZone: string }) {
  const id = useId();
  const heading = useRef<HTMLHeadingElement>(null);
  const client = useQueryClient();
  const revisions = useVerificationPolicyDrafts(scope);
  const [editing, setEditing] = useState(false);
  const [selected, setSelected] = useState<VerificationPolicyDraftRevision | null>(null);
  const [label, setLabel] = useState("");
  const [second, setSecond] = useState("");
  const [validity, setValidity] = useState("");
  const [response, setResponse] = useState("");
  const [reason, setReason] = useState("");
  const [attempt, setAttempt] = useState<Attempt | null>(null);
  const [error, setError] = useState("");
  const [conflict, setConflict] = useState(false);
  const [saved, setSaved] = useState(false);
  const [changed, setChanged] = useState(tenantTransitionPending);
  useEffect(
    () =>
      onTenantTransition(() => {
        if (tenantTransitionPending()) {
          setChanged(true);
          setEditing(false);
          setSelected(null);
          setAttempt(null);
          setLabel("");
          setReason("");
        }
      }),
    [],
  );
  const mutation = useMutation({
    mutationFn: ({ command, recover }: { command: Attempt; recover: boolean }) =>
      recover
        ? readVerificationPolicyDraft(scope, command.policy, command.revision)
        : saveVerificationPolicyDraft(scope, command.policy, command.revision, command.input),
    onSuccess: (row, variables) => {
      const command = variables.command;
      if (
        row.prepared_by !== scope.subject ||
        Object.entries(command.input).some(([key, value]) => row[key as keyof typeof row] !== value)
      ) {
        setError(
          "This revision could not be matched to your request. Keep this page open and retry the same revision.",
        );
        return;
      }
      setSelected(row);
      setEditing(false);
      setAttempt(null);
      setError("");
      setSaved(true);
      void client.invalidateQueries({
        queryKey: pharmacyKey(scope, "verification-policy-drafts", scope.subject),
      });
    },
    onError: (failure) => {
      const stale =
        failure instanceof ApiError &&
        failure.status === 409 &&
        failure.code === "verification_policy_draft_conflict";
      setConflict(stale);
      setError(
        stale
          ? "This revision conflicts with retained history. Recover it, or discard these unsaved changes and review the current revision before trying again."
          : "Could not confirm the saved revision. Keep this page open. Recover it or retry the same revision.",
      );
    },
  });
  function open(row: VerificationPolicyDraftRevision | null) {
    setSelected(row);
    setLabel(row?.label ?? "");
    setSecond(row ? (row.second_check_required ? "yes" : "no") : "");
    setValidity(row ? String(row.review_valid_for_seconds) : "");
    setResponse(row ? String(row.query_response_target_seconds) : "");
    setReason("");
    setError("");
    setSaved(false);
    setConflict(false);
    setEditing(true);
  }
  function save() {
    const parsed = VerificationPolicyDraftInputContract.safeParse({
      expected_version: selected?.version ?? 0,
      label,
      second_check_required: second === "" ? undefined : second === "yes",
      review_valid_for_seconds: Number(validity),
      query_response_target_seconds: Number(response),
      reason,
    });
    if (!parsed.success) {
      setError(
        "Enter a policy name, an explicit second-check choice, positive whole-second intervals and a reason.",
      );
      return;
    }
    const command: Attempt = {
      policy: selected?.policy_id ?? newPharmacyId(),
      revision: newPharmacyId(),
      input: parsed.data,
    };
    setAttempt(command);
    setError("");
    mutation.mutate({ command, recover: false });
  }
  useEffect(() => {
    if (editing) heading.current?.focus();
  }, [editing]);
  if (changed)
    return (
      <Panel>
        <p role="status">
          Your account or workspace changed. Reopen policy drafts before continuing.
        </p>
      </Panel>
    );
  const locked = mutation.isPending || attempt !== null;
  return (
    <Panel>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="text-lg font-semibold">Verification policy drafts</h3>
        {!editing && (
          <Button type="button" onClick={() => open(null)}>
            New policy draft
          </Button>
        )}
      </div>
      <p role="note" className="my-3 rounded-md border p-3 text-sm">
        Administrative proposals do not authorize clinical verification or medicine supply. A
        qualified independent approval and separate publication are required before any policy can
        take effect. Emergency supply is unavailable.
      </p>
      {saved && (
        <p role="status">Draft revision saved. Clinical authority has not been evaluated.</p>
      )}
      {editing && (
        <form
          className="my-4 grid gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (!locked) save();
          }}
          noValidate
        >
          <h4 className="font-semibold" ref={heading} tabIndex={-1}>
            {selected
              ? `Revise ${selected.label} (version ${selected.version})`
              : "New administrative policy draft"}
          </h4>
          <label htmlFor={`${id}-label`}>
            Policy name
            <Input
              id={`${id}-label`}
              value={label}
              onChange={(event) => setLabel(event.target.value)}
              disabled={locked}
            />
          </label>
          <label htmlFor={`${id}-second`}>
            Independent second check
            <SelectControl
              id={`${id}-second`}
              value={second}
              onValueChange={setSecond}
              disabled={locked}
              options={[
                { value: "", label: "Choose explicitly" },
                { value: "yes", label: "Propose a required independent second check" },
                { value: "no", label: "Propose a single review" },
              ]}
            />
          </label>
          <p className="text-sm text-muted-foreground">
            These intervals are a proposal for clinical-governance review. They are not recommended
            clinical values.
          </p>
          <label htmlFor={`${id}-validity`}>
            Review validity (seconds)
            <Input
              id={`${id}-validity`}
              type="number"
              min="1"
              step="1"
              value={validity}
              onChange={(event) => setValidity(event.target.value)}
              disabled={locked}
            />
          </label>
          <label htmlFor={`${id}-response`}>
            Prescriber response target (seconds)
            <Input
              id={`${id}-response`}
              type="number"
              min="1"
              step="1"
              value={response}
              onChange={(event) => setResponse(event.target.value)}
              disabled={locked}
            />
          </label>
          <label htmlFor={`${id}-reason`}>
            Reason for this revision
            <Input
              id={`${id}-reason`}
              value={reason}
              onChange={(event) => setReason(event.target.value)}
              disabled={locked}
            />
          </label>
          {error && <p role="alert">{error}</p>}
          {mutation.isPending && <p role="status">Checking and saving this draft revision…</p>}
          <div className="flex flex-wrap gap-3">
            {attempt ? (
              <>
                <Button
                  type="button"
                  disabled={mutation.isPending}
                  onClick={() => mutation.mutate({ command: attempt, recover: false })}
                >
                  Retry same revision
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={mutation.isPending}
                  onClick={() => mutation.mutate({ command: attempt, recover: true })}
                >
                  Recover saved revision
                </Button>
                {conflict && (
                  <RefreshButton
                    variant="outline"
                    busy={mutation.isPending}
                    error={revisions.error}
                    refresh={() => {
                      setAttempt(null);
                      setEditing(false);
                      setError("");
                      setConflict(false);
                      revisions.retry();
                    }}
                  >
                    Discard unsaved changes and refresh history
                  </RefreshButton>
                )}
              </>
            ) : (
              <>
                <Button type="submit">Save draft revision</Button>
                <Button type="button" variant="outline" onClick={() => setEditing(false)}>
                  Cancel draft changes
                </Button>
              </>
            )}
          </div>
        </form>
      )}
      <h4 className="mt-4 font-semibold">Saved revision history</h4>
      {revisions.isPending ? (
        <p role="status">Loading draft revisions…</p>
      ) : revisions.isError ? (
        <RequestFailure
          message="Could not load policy draft revisions."
          error={revisions.error}
          retry={revisions.retry}
        />
      ) : (
        <>
          {revisions.items.length === 0 ? (
            <p>No administrative policy drafts have been saved for this outlet.</p>
          ) : (
            <ul className="divide-y">
              {revisions.items.map((row) => (
                <li key={row.id} className="flex flex-wrap items-center justify-between gap-3 py-3">
                  <div>
                    <p>
                      {row.label} · version {row.version}
                    </p>
                    <p className="text-sm text-muted-foreground">
                      Draft ·{" "}
                      {new Intl.DateTimeFormat(undefined, {
                        dateStyle: "medium",
                        timeStyle: "short",
                        timeZone,
                      }).format(new Date(row.created_at))}{" "}
                      · clinical authority not evaluated
                    </p>
                    <p className="text-sm">{row.reason}</p>
                  </div>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={editing}
                    onClick={() => open(row)}
                  >
                    Revise {row.label}, version {row.version}
                  </Button>
                </li>
              ))}
            </ul>
          )}
          {revisions.hasMore && (
            <Button
              type="button"
              variant="outline"
              disabled={revisions.isFetchingMore}
              onClick={revisions.loadMore}
            >
              {revisions.isFetchingMore ? "Loading earlier revisions…" : "Load earlier revisions"}
            </Button>
          )}
        </>
      )}
    </Panel>
  );
}
