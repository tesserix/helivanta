"use client";

import {
  useCallback,
  useSyncExternalStore,
  useId,
  useRef,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Button, Input } from "@tesserix/web";
import { AccountName, Field, Panel, RequestFailure, useZodForm } from "@helivanta/ui";
import {
  ApiError,
  capabilityForPanel,
  StaffMFAResetInputContract,
  cancelStaffMFARecovery,
  newStaffMFARecoveryCommandId,
  readStaffMFARecovery,
  readStaffMFAReviewContext,
  resetStaffMFA,
  useApiQuery,
  useReleaseCapabilities,
  useTenantTransition,
  type StaffMFARecoveryReceipt,
  type StaffMFARecoveryScope,
  type StaffMFAResetInput,
} from "@helivanta/api";

type Link = { id: string; version: number; subject: string };
type Props = { scope: StaffMFARecoveryScope; link?: Link };
type Identity = { tenant_id: string; subject: string; data: string[] };
const reasonSchema = StaffMFAResetInputContract.pick({ reason: true });
function storageKey(scope: StaffMFARecoveryScope) {
  return (
    "staff-mfa-request:v1:" +
    JSON.stringify([
      scope.tenantId,
      scope.subject,
      scope.employerKind,
      scope.employerId,
      scope.staffId,
    ])
  );
}
function storedCommand(key: string) {
  const value = sessionStorage.getItem(key);
  return value === null ? null : StaffMFAResetInputContract.shape.commandId.parse(value);
}

export function StaffMFARecoveryPanel(props: Props) {
  const release = useReleaseCapabilities();
  const switching = useTenantTransition();
  if (switching) return null;
  return (
    <SelectedRequest
      key={storageKey(props.scope)}
      {...props}
      enabled={release.available(capabilityForPanel("staff.accounts.mfa_recovery"))}
    />
  );
}
const markerEvent = "helivanta:staff-mfa-request-updated";
function subscribeMarker(notify: () => void) {
  window.addEventListener(markerEvent, notify);
  window.addEventListener("storage", notify);
  return () => {
    window.removeEventListener(markerEvent, notify);
    window.removeEventListener("storage", notify);
  };
}
function refreshMarker() {
  window.dispatchEvent(new Event(markerEvent));
}
function serverMarker() {
  return null;
}
function SelectedRequest(props: Props & { enabled: boolean }) {
  const key = storageKey(props.scope);
  const getMarker = useCallback(() => {
    try {
      return storedCommand(key);
    } catch {
      return "unavailable";
    }
  }, [key]);
  const marker = useSyncExternalStore(subscribeMarker, getMarker, serverMarker);
  const selected = marker === "unavailable" ? null : marker;
  const storageError = marker === "unavailable";
  const [receipt, setReceipt] = useState<StaffMFARecoveryReceipt | null>(null);
  if (!props.enabled && !selected && !receipt) return null;
  return <AuthorizedRequest {...props} {...{ selected, receipt, setReceipt, storageError }} />;
}

function AuthorizedRequest(
  props: Props & {
    enabled: boolean;
    selected: string | null;
    receipt: StaffMFARecoveryReceipt | null;
    setReceipt: Dispatch<SetStateAction<StaffMFARecoveryReceipt | null>>;
    storageError: boolean;
  },
) {
  const identity = useApiQuery<Identity>(["iam", "me", "permissions"], "/iam/me/permissions");
  const value = identity.data;
  const permission =
    props.scope.employerKind === "hospital"
      ? "workforce.login_links.manage"
      : "workforce.independent_login.manage";
  if (
    identity.isError ||
    identity.isPending ||
    !value ||
    value.tenant_id !== props.scope.tenantId ||
    value.subject !== props.scope.subject ||
    !value.data.includes("iam.member.manage") ||
    !value.data.includes(permission)
  )
    return null;
  return <RecoveryRequest {...props} />;
}
function RecoveryRequest({
  scope,
  link,
  enabled,
  selected,
  storageError,
  receipt,
  setReceipt,
}: Props & {
  enabled: boolean;
  selected: string | null;
  receipt: StaffMFARecoveryReceipt | null;
  setReceipt: Dispatch<SetStateAction<StaffMFARecoveryReceipt | null>>;
  storageError: boolean;
}) {
  const heading = useId();
  const [editing, setEditing] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [storageFailed, setStorageFailed] = useState(storageError);
  const intent = useRef<StaffMFAResetInput | null>(null);
  const key = storageKey(scope);
  function accept(result: StaffMFARecoveryReceipt) {
    setReceipt(result);
    setError(null);
    setEditing(false);
    if (["cancelled", "completed"].includes(result.state)) {
      try {
        if (storedCommand(key) === result.commandId) sessionStorage.removeItem(key);
        refreshMarker();
      } catch {
        setStorageFailed(true);
      }
    }
  }
  const write = useMutation({
    mutationFn: async () => {
      if (!intent.current) throw new Error("No selected recovery request.");
      return resetStaffMFA(scope, intent.current);
    },
    onSuccess: accept,
    onError: setError,
    onSettled: () => {
      intent.current = null;
    },
    retry: false,
    gcTime: 0,
  });
  const read = useMutation({
    mutationFn: () => {
      if (!selected) throw new Error("No selected recovery request.");
      return readStaffMFARecovery(scope, selected);
    },
    onSuccess: accept,
    onError: setError,
    retry: false,
    gcTime: 0,
  });
  const cancel = useMutation({
    mutationFn: () => {
      if (!selected) throw new Error("No selected recovery request.");
      return cancelStaffMFARecovery(scope, selected);
    },
    onSuccess: accept,
    onError: setError,
    retry: false,
    gcTime: 0,
  });
  const busy = write.isPending || read.isPending || cancel.isPending;
  function submit(input: StaffMFAResetInput) {
    try {
      const existing = storedCommand(key);
      if (existing) {
        refreshMarker();
        setEditing(false);
        return;
      }
      sessionStorage.setItem(key, input.commandId);
      if (storedCommand(key) !== input.commandId)
        throw new Error("Request selection was not retained.");
    } catch {
      setStorageFailed(true);
      return;
    }
    intent.current = input;
    refreshMarker();
    setReceipt(null);
    setError(null);
    setEditing(false);
    write.mutate();
  }
  if (
    !selected &&
    !receipt &&
    (!enabled || !link || link.subject === scope.subject) &&
    !storageFailed
  )
    return null;
  const states: Record<StaffMFARecoveryReceipt["state"], string> = {
    prepared: "This request is prepared. You can cancel it before the authenticator is reset.",
    processing:
      "The identity service is processing this request. Check its status before continuing.",
    unconfirmed:
      "The identity service has not confirmed the outcome. Check this request or contact your identity administrator.",
    awaiting_enrollment:
      "The authenticator reset was acknowledged. Ask the Staff member to sign in and set up a new authenticator app or security key.",
    completed: "This recovery request is complete.",
    cancelled: "This recovery request was cancelled.",
  };
  return (
    <Panel as="section" aria-labelledby={heading} className="mt-5 space-y-4">
      <div>
        <h4 id={heading} className="font-semibold">
          Authenticator recovery
        </h4>
        {link && !selected && !receipt && !error && (
          <p className="text-sm">
            <AccountName subject={link.subject} />
          </p>
        )}
      </div>
      {storageFailed && (
        <p role="alert">
          This browser could not retain the recovery request safely. Use another tab or contact your
          administrator.
        </p>
      )}
      {error != null && (
        <p role="alert">
          {error instanceof ApiError && error.status === 401
            ? "Sign in again before administering authenticator recovery."
            : error instanceof ApiError && [403, 404].includes(error.status)
              ? "This request cannot be accessed from your current account. Refresh the Staff panel or contact your administrator."
              : "The outcome is not confirmed. Check this request before starting another."}
        </p>
      )}
      {error instanceof ApiError && error.status === 401 && (
        <a className="text-sm underline" href="/login">
          Sign in again
        </a>
      )}
      {receipt && <p role="status">{states[receipt.state]}</p>}
      {selected ? (
        <>
          {busy && <p role="status">Checking the selected recovery request…</p>}
          {!receipt && !error && !busy && (
            <p role="status">
              A recovery request is retained for this Staff member. Check its status before
              continuing.
            </p>
          )}
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => {
                setError(null);
                read.mutate();
              }}
            >
              Check recovery status
            </Button>
            {(!receipt || receipt.state === "prepared") && (
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => {
                  setError(null);
                  cancel.mutate();
                }}
              >
                Cancel if still prepared
              </Button>
            )}
          </div>
        </>
      ) : receipt ? (
        <Button variant="outline" disabled={storageFailed} onClick={() => setReceipt(null)}>
          Done
        </Button>
      ) : (
        enabled &&
        link &&
        !storageFailed &&
        (editing ? (
          <ResetForm {...{ scope, link }} onSubmit={submit} onClose={() => setEditing(false)} />
        ) : (
          <Button variant="outline" onClick={() => setEditing(true)}>
            Reset authenticator
          </Button>
        ))
      )}
    </Panel>
  );
}
function ResetForm({
  scope,
  link,
  onSubmit,
  onClose,
}: Props & { link: Link; onSubmit: (input: StaffMFAResetInput) => void; onClose: () => void }) {
  const id = useId();
  const [confirmed, setConfirmed] = useState(false);
  const form = useZodForm(reasonSchema, { reason: "" });
  const context = useQuery({
    queryKey: [
      "staff-mfa-review",
      scope.tenantId,
      scope.subject,
      scope.employerKind,
      scope.employerId,
      scope.staffId,
    ],
    queryFn: ({ signal }) => readStaffMFAReviewContext(scope, signal),
    staleTime: 0,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
  });
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit(({ reason }) => {
        if (!confirmed || !context.data?.active || link.subject === scope.subject) return;
        onSubmit(
          StaffMFAResetInputContract.parse({
            commandId: newStaffMFARecoveryCommandId(),
            expectedLinkId: link.id,
            expectedLinkVersion: link.version,
            expectedStaffVersion: context.data.staffVersion,
            reason,
          }),
        );
      })}
      className="space-y-4"
    >
      <p className="text-sm">
        Verify the Staff member before resetting their authenticator. They will need to sign in and
        set up a new authenticator app or security key.
      </p>
      {context.isPending && <p role="status">Checking the current Staff record…</p>}
      {context.isError && (
        <RequestFailure
          message="Could not verify the current Staff record."
          retry={context.refetch}
          error={context.error}
        />
      )}
      {context.data && !context.data.active && (
        <p role="alert">Only active Staff can receive an authenticator reset.</p>
      )}
      <fieldset disabled={!context.data?.active} className="space-y-4">
        <Field
          id={`${id}-reason`}
          label="Reason for authenticator reset"
          error={form.formState.errors.reason?.message}
        >
          <Input id={`${id}-reason`} {...form.register("reason")} />
        </Field>
        <label className="flex items-start gap-2 text-sm">
          <input
            type="checkbox"
            checked={confirmed}
            onChange={(event) => setConfirmed(event.target.checked)}
          />
          I verified the Staff member and authorize this reset.
        </label>
        <Button
          type="submit"
          variant="outline"
          disabled={!confirmed || !form.watch("reason").trim()}
        >
          Confirm authenticator reset
        </Button>
      </fieldset>
      <Button type="button" variant="ghost" onClick={onClose}>
        Close reset form
      </Button>
    </form>
  );
}
