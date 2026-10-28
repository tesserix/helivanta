"use client";

import { useState, useSyncExternalStore, useEffect, useRef } from "react";
import { Button } from "@tesserix/web";
import { Field, PageHeading, Panel, SelectControl, useZodForm } from "@helivanta/ui";
import {
  ApiError,
  apiFetch,
  formatHalfDays,
  leaveDecisionInputContract,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  useTenantTransition,
  type LeaveDecisionInput,
  type LeaveRequestDetail,
  type LeaveReviewRequest,
  type LeaveReviewScope,
  type PermissionsResponse,
} from "@helivanta/api";
import { inputClass } from "../roster-policy/policy-ui";

const subscribeHydration = () => () => {};
const noteSchema = leaveDecisionInputContract.pick({ note: true });
type Identity = { tenant: string; subject: string };

export default function ReviewLeave() {
  const hydrated = useSyncExternalStore(
    subscribeHydration,
    () => true,
    () => false,
  );
  const switching = useTenantTransition();
  const identity = useApiQuery<PermissionsResponse>(
    ["iam", "me", "permissions"],
    "/iam/me/permissions",
    { enabled: !switching, request: { cache: "no-store" } },
  );
  return (
    <>
      <PageHeading description="Review pending requests in hospitals where you have current approval access." />
      {!hydrated || switching || identity.isPending ? (
        <p role="status">Checking your workspace…</p>
      ) : identity.isError || !identity.data?.tenant_id || !identity.data.subject ? (
        <p role="alert">Could not verify your workspace. Refresh this page to try again.</p>
      ) : (
        <ReviewWorkspace
          key={`${identity.data.tenant_id}:${identity.data.subject}`}
          identity={{ tenant: identity.data.tenant_id, subject: identity.data.subject }}
        />
      )}
    </>
  );
}

function ReviewWorkspace({ identity }: { identity: Identity }) {
  const headers = { "X-Helivanta-Expected-Tenant": identity.tenant };
  const scopes = useApiPagedQuery<LeaveReviewScope>(
    ["workforce", "leave-review-scopes", identity.tenant, identity.subject],
    "/workforce/leave-review-scopes",
    { request: { cache: "no-store", headers } },
  );
  const [selected, setSelected] = useState("");
  const hospital = scopes.items.find((scope) => scope.hospital_id === selected) ?? scopes.items[0];
  return (
    <div className="space-y-5">
      <Button variant="outline" disabled={scopes.isRefreshing} onClick={scopes.retry}>
        Refresh access
      </Button>
      {scopes.isPending ? (
        <p role="status">Loading review access…</p>
      ) : scopes.isError ? (
        <p role="alert">Could not load your review access. Refresh access to try again.</p>
      ) : (
        <>
          {hospital ? (
            <>
              <Field id="review-hospital" label="Hospital">
                <SelectControl
                  id="review-hospital"
                  value={hospital.hospital_id}
                  onValueChange={setSelected}
                  options={scopes.items.map((scope) => ({
                    value: scope.hospital_id,
                    label: scope.name,
                  }))}
                />
              </Field>
              <ReviewQueue key={hospital.hospital_id} identity={identity} hospital={hospital} />
            </>
          ) : (
            <p>No hospitals are currently available for leave review.</p>
          )}
          {scopes.hasMore && (
            <Button variant="outline" disabled={scopes.isFetchingMore} onClick={scopes.loadMore}>
              Load more hospitals
            </Button>
          )}
        </>
      )}
    </div>
  );
}

function ReviewQueue({ identity, hospital }: { identity: Identity; hospital: LeaveReviewScope }) {
  const base = `/workforce/hospitals/${hospital.hospital_id}`;
  const key = [
    "workforce",
    "leave-review-requests",
    identity.tenant,
    identity.subject,
    hospital.hospital_id,
  ];
  const records = useApiPagedQuery<LeaveReviewRequest>(key, `${base}/leave-review-requests`, {
    request: { cache: "no-store", headers: { "X-Helivanta-Expected-Tenant": identity.tenant } },
  });
  const [selected, setSelected] = useState<LeaveReviewRequest | null>(null);
  const [notice, setNotice] = useState("");
  const current = selected;
  const [locked, setLocked] = useState(false);
  const denied =
    records.error instanceof ApiError && [401, 403, 404, 409].includes(records.error.status);
  const refresh = () => {
    if (locked)
      setNotice(
        "An earlier decision result is unconfirmed. Ask a staff administrator to verify it.",
      );
    setLocked(false);
    setSelected(null);
    records.retry();
  };
  return (
    <Panel className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <h3 className="font-semibold">Pending leave requests</h3>
        <Button
          variant="outline"
          disabled={records.isRefreshing || (locked && !denied)}
          onClick={refresh}
        >
          Refresh queue
        </Button>
      </div>
      {notice && <p role="status">{notice}</p>}
      {records.isPending ? (
        <p role="status">Loading pending requests…</p>
      ) : records.isError ? (
        <p role="alert">
          Could not load pending requests. Your access may have changed. Refresh access or the queue
          to try again.
        </p>
      ) : (
        <>
          {records.items.length === 0 ? (
            <p>
              {current
                ? "No other pending requests are shown."
                : "No pending requests need your review."}
            </p>
          ) : (
            <ul className="divide-y">
              {records.items.map((row) => (
                <li key={row.id} className="flex flex-wrap items-center justify-between gap-3 p-4">
                  <div>
                    <p className="font-medium">
                      {row.display_name} · {row.employee_code}
                    </p>
                    <p className="text-sm text-muted-foreground">
                      {row.name} · {row.start_on}–{row.end_on} ·{" "}
                      {row.unit === "short_leave"
                        ? `${row.minutes} minutes`
                        : formatHalfDays(row.half_days)}
                    </p>
                  </div>
                  <Button
                    variant="outline"
                    disabled={records.isRefreshing || locked}
                    onClick={() => {
                      setNotice("");
                      setSelected(row);
                    }}
                  >
                    Review leave for {row.display_name}
                  </Button>
                </li>
              ))}
            </ul>
          )}
          {records.hasMore && (
            <Button
              variant="outline"
              disabled={records.isFetchingMore || locked}
              onClick={records.loadMore}
            >
              Load more requests
            </Button>
          )}
        </>
      )}
      {current && !denied && (
        <ReviewDetail
          key={`${current.id}:${current.version}`}
          identity={identity}
          hospital={hospital.hospital_id}
          base={base}
          row={current}
          locked={locked}
          onLock={setLocked}
          onClose={() => setSelected(null)}
          onDone={() => {
            setLocked(false);
            setSelected(null);
            setNotice("Decision recorded.");
          }}
        />
      )}
    </Panel>
  );
}

function ReviewDetail({
  identity,
  hospital,
  base,
  row,
  locked,
  onLock,
  onClose,
  onDone,
}: {
  identity: Identity;
  hospital: string;
  base: string;
  row: LeaveReviewRequest;
  locked: boolean;
  onLock: (locked: boolean) => void;
  onClose: () => void;
  onDone: () => void;
}) {
  const detail = useApiQuery<LeaveRequestDetail>(
    [
      "workforce",
      "leave-review-detail",
      identity.tenant,
      identity.subject,
      hospital,
      row.id,
      row.version,
    ],
    `${base}/staff/${row.staff_id}/leave-review-requests/${row.id}`,
    {
      gcTime: 0,
      staleTime: 0,
      request: { cache: "no-store", headers: { "X-Helivanta-Expected-Tenant": identity.tenant } },
    },
  );
  const [blocked, setBlocked] = useState("");
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => heading.current?.focus(), []);
  return (
    <Panel aria-label="Review leave request" className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <h4 ref={heading} tabIndex={-1} className="font-semibold outline-none">
          {row.display_name} · {row.name}
        </h4>
        <Button variant="ghost" disabled={locked} onClick={onClose}>
          Close request
        </Button>
      </div>
      {blocked ? (
        <p role="alert">{blocked}</p>
      ) : detail.isPending ? (
        <p role="status">Loading request details…</p>
      ) : detail.isError ? (
        <p role="alert">
          Could not load this request. Close it and refresh the queue; your access or the request
          may have changed.
        </p>
      ) : (
        <>
          {detail.data.request.reason && <p>Reason: {detail.data.request.reason}</p>}
          <section aria-label="Days counted">
            <h5 className="font-medium">Days counted</h5>
            <ul>
              {detail.data.days.map((day) => (
                <li key={day.day}>
                  {day.day} · {day.holiday ? "Holiday, not counted" : formatHalfDays(day.half_days)}
                </li>
              ))}
            </ul>
          </section>
          <section aria-label="Roster clashes">
            <h5 className="font-medium">Roster clashes</h5>
            {detail.data.roster_clashes.length === 0 ? (
              <p>No roster clashes were found for these dates.</p>
            ) : (
              <ul>
                {detail.data.roster_clashes.map((clash) => (
                  <li key={`${clash.roster_id}:${clash.starts_at}`}>
                    {clash.roster_name} · {clash.starts_at}–{clash.ends_at}
                  </li>
                ))}
              </ul>
            )}
          </section>
          <ReviewDecision
            identity={identity}
            hospital={hospital}
            path={`${base}/staff/${row.staff_id}/leave-requests/${row.id}/decision`}
            version={detail.data.request.version}
            onLock={onLock}
            onBlocked={setBlocked}
            onDone={onDone}
          />
        </>
      )}
    </Panel>
  );
}

function ReviewDecision({
  identity,
  hospital,
  path,
  version,
  onLock,
  onBlocked,
  onDone,
}: {
  identity: Identity;
  hospital: string;
  path: string;
  version: number;
  onLock: (locked: boolean) => void;
  onBlocked: (message: string) => void;
  onDone: () => void;
}) {
  const form = useZodForm(noteSchema, { note: "" });
  const [attempt, setAttempt] = useState<LeaveDecisionInput | null>(null);
  const save = useApiMutation(
    async (input: LeaveDecisionInput) => {
      try {
        return await apiFetch<LeaveRequestDetail>(path, {
          method: "POST",
          headers: { "X-Helivanta-Expected-Tenant": identity.tenant },
          body: JSON.stringify(input),
        });
      } catch (error) {
        if (error instanceof ApiError && [401, 403, 404, 409].includes(error.status)) {
          onLock(false);
          onBlocked(
            error.status === 409
              ? "The request or workspace changed. Close this request and refresh the queue before deciding."
              : "Your access or this request changed. Close this request and refresh access before continuing.",
          );
        } else if (error instanceof ApiError && [400, 422].includes(error.status)) {
          onLock(false);
        }
        throw error;
      }
    },
    {
      suppressErrorToast: true,
      invalidate: [
        ["workforce", "leave-review-requests", identity.tenant, identity.subject, hospital],
      ],
      onSuccess: onDone,
    },
  );
  const editable = save.error instanceof ApiError && [400, 422].includes(save.error.status);
  const uncertain = save.isError && !editable;
  const frozen = save.isPending || uncertain;
  const submit = (decision: "approve" | "reject") =>
    void form.handleSubmit(({ note }) => {
      if (decision === "reject" && !note)
        return form.setError("note", { message: "Add a note explaining the decision." });
      const input = { expected_version: version, decision, note };
      setAttempt(input);
      onLock(true);
      save.mutate(input);
    })();
  return (
    <form
      aria-label="Decide leave request"
      className="space-y-4"
      onSubmit={(event) => event.preventDefault()}
      noValidate
    >
      <fieldset disabled={frozen}>
        <Field id="review-note" label="Decision note" error={form.formState.errors.note?.message}>
          <textarea
            id="review-note"
            rows={3}
            className={inputClass}
            aria-invalid={!!form.formState.errors.note}
            {...form.register("note")}
          />
        </Field>
      </fieldset>
      {save.isError && (
        <p role="alert">
          {uncertain
            ? "The result could not be confirmed. Retry the same decision before making changes."
            : save.error.message}
        </p>
      )}
      {uncertain && attempt ? (
        <Button type="button" disabled={save.isPending} onClick={() => save.mutate(attempt)}>
          Retry same decision
        </Button>
      ) : (
        <div className="flex justify-end gap-3">
          <Button
            type="button"
            variant="outline"
            disabled={frozen}
            onClick={() => submit("reject")}
          >
            Reject leave
          </Button>
          <Button type="button" disabled={frozen} onClick={() => submit("approve")}>
            {save.isPending ? "Saving decision…" : "Approve leave"}
          </Button>
        </div>
      )}
    </form>
  );
}
