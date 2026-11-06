"use client";

import { useEffect, useState } from "react";
import { Button } from "@tesserix/web";
import { Panel, RefreshButton, RequestFailure, useConfirmLeave } from "@helivanta/ui";
import {
  useStaffPostingCutoffScopes,
  useStaffPostingProposalInbox,
  useStaffPostingCutoffContext,
  useStaffPostingCutoffHistory,
  type EnrollmentRequestScope,
  type StaffPostingInboxScope,
  type StaffPostingProposalScope,
  type StaffPostingInboxItem,
  type StaffPostingProposal,
  type StaffPostingCutoff,
} from "@helivanta/api";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "./staff-access-cutoff";
import { PostingCutoffForm } from "./posting-cutoff-form";

type Props = {
  scope: EnrollmentRequestScope;
  onContextChanged: () => void;
  onBusyChanged?: (busy: boolean) => void;
};

export function StaffPostingCutoffWorkspace(props: Props) {
  return <CutoffAccount key={`${props.scope.tenantId}:${props.scope.subject}`} {...props} />;
}

function CutoffAccount({ scope, onContextChanged, onBusyChanged }: Props) {
  const [busy, setBusy] = useState(false);
  const [cursor, setCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const hospitals = useStaffPostingCutoffScopes(scope, cursor, busy);
  const cutoff = useStaffAccessCutoff([hospitals.error], onContextChanged);
  useEffect(() => {
    onBusyChanged?.(busy);
  }, [busy, onBusyChanged]);
  useEffect(() => () => onBusyChanged?.(false), [onBusyChanged]);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (hospitals.isPending || (hospitals.isFetching && !busy))
    return <p role="status">Checking posting cutoff hospitals…</p>;
  if (!hospitals.data)
    return (
      <RequestFailure
        message="Posting cutoff access could not be verified."
        retry={hospitals.refetch}
        error={hospitals.error}
      />
    );
  const hospital = hospitals.data.data.find((row) => row.hospital_id === selected);
  return (
    <section className="space-y-5" aria-label="Posting cutoffs">
      <p className="max-w-3xl text-sm text-muted-foreground">
        End or shorten a departmental posting while preserving its approval history. This does not
        end employment or appoint a replacement.
      </p>
      {hospitals.isError && (
        <RequestFailure
          message="Hospital admission could not be rechecked. The unconfirmed command is retained."
          retry={hospitals.refetch}
          error={hospitals.error}
        />
      )}
      <div className="flex flex-wrap gap-3">
        {hospitals.data.data.map((row) => (
          <Button
            key={row.hospital_id}
            disabled={busy}
            variant={selected === row.hospital_id ? "default" : "outline"}
            aria-label={`Open posting cutoffs for ${row.name}`}
            onClick={() => setSelected(row.hospital_id)}
          >
            {row.name}
          </Button>
        ))}
      </div>
      {hospitals.data.data.length === 0 && (
        <p>No approved posting cutoff hospitals on this page.</p>
      )}
      {selected && !hospital && (
        <p role="alert">This hospital is no longer available. Reopen posting cutoffs.</p>
      )}
      {hospital && (
        <HospitalCutoffs
          key={hospital.hospital_id}
          scope={{ ...scope, hospitalId: hospital.hospital_id, access: "scoped" }}
          busy={busy}
          onBusy={setBusy}
          onContextChanged={onContextChanged}
          admissionAvailable={hospitals.isSuccess && !hospitals.isFetching}
        />
      )}
      <div className="flex flex-wrap gap-2">
        {hospitals.data.page.has_more && (
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => {
              setSelected(null);
              setCursor(hospitals.data.page.next_cursor);
            }}
          >
            More cutoff hospitals
          </Button>
        )}
        {cursor && (
          <Button
            variant="ghost"
            disabled={busy}
            onClick={() => {
              setSelected(null);
              setCursor(null);
            }}
          >
            First cutoff hospitals
          </Button>
        )}
        <RefreshButton
          busy={busy}
          error={hospitals.error}
          refresh={() => {
            setSelected(null);
            return hospitals.refetch();
          }}
        >
          Refresh cutoff access
        </RefreshButton>
      </div>
    </section>
  );
}

function HospitalCutoffs({
  scope,
  busy,
  onBusy,
  onContextChanged,
  admissionAvailable,
}: {
  scope: StaffPostingInboxScope;
  busy: boolean;
  onBusy: (value: boolean) => void;
  onContextChanged: () => void;
  admissionAvailable: boolean;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<StaffPostingInboxItem | null>(null);
  const inbox = useStaffPostingProposalInbox(scope, "approved", cursor, busy);
  const cutoff = useStaffAccessCutoff([inbox.error], onContextChanged);
  const confirmLeave = useConfirmLeave();
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (inbox.isPending || (inbox.isFetching && !busy))
    return <p role="status">Loading approved postings…</p>;
  if (!inbox.data)
    return (
      <RequestFailure
        message="Approved postings could not be verified."
        retry={inbox.refetch}
        error={inbox.error}
      />
    );
  const current = busy
    ? selected
    : selected &&
      inbox.data.data.find(
        (row) =>
          row.proposal.id === selected.proposal.id &&
          row.proposal.revision_id === selected.proposal.revision_id &&
          row.staff.id === selected.staff.id,
      );
  return (
    <Panel className="space-y-5">
      <h2 className="font-semibold">Approved postings</h2>
      {inbox.isError && (
        <p role="alert">
          Posting admission could not be rechecked. The unconfirmed command is retained.
        </p>
      )}
      <ul className="grid gap-3 md:grid-cols-2">
        {inbox.data.data.map((item) => (
          <Panel as="li" key={item.proposal.id} className="space-y-3">
            <div>
              <h3 className="font-medium">{item.staff.display_name}</h3>
              <p className="text-sm text-muted-foreground">
                {item.staff.employee_code} · Approved posting
              </p>
            </div>
            <p className="text-sm">
              {new Date(item.proposal.starts_at).toISOString()} ·{" "}
              {item.proposal.ends_at ? new Date(item.proposal.ends_at).toISOString() : "Ongoing"}{" "}
              (UTC)
            </p>
            <Button
              variant="outline"
              disabled={busy}
              aria-label={`Manage cutoff for ${item.staff.display_name}`}
              onClick={() => setSelected(item)}
            >
              Manage cutoff
            </Button>
          </Panel>
        ))}
      </ul>
      {inbox.data.data.length === 0 && <p>No approved postings on this page.</p>}
      {current && (
        <CutoffDetail
          key={`${current.proposal.id}:${current.proposal.revision_id}`}
          scope={{ ...scope, staffId: current.staff.id }}
          item={current}
          busy={busy}
          onBusy={onBusy}
          onContextChanged={onContextChanged}
          admissionAvailable={admissionAvailable && inbox.isSuccess && !inbox.isFetching}
          onClose={() => confirmLeave(() => setSelected(null))}
        />
      )}
      <div className="flex flex-wrap gap-2">
        {inbox.data.page.has_more && (
          <Button
            variant="outline"
            disabled={busy}
            onClick={() => {
              setSelected(null);
              setCursor(inbox.data.page.next_cursor);
            }}
          >
            More approved postings
          </Button>
        )}
        {cursor && (
          <Button
            variant="ghost"
            disabled={busy}
            onClick={() => {
              setSelected(null);
              setCursor(null);
            }}
          >
            First approved postings
          </Button>
        )}
        <RefreshButton
          busy={busy}
          error={inbox.error}
          refresh={() => {
            setSelected(null);
            return inbox.refetch();
          }}
        >
          Refresh approved postings
        </RefreshButton>
      </div>
    </Panel>
  );
}

function CutoffDetail({
  scope,
  item,
  busy,
  onBusy,
  onContextChanged,
  admissionAvailable,
  onClose,
}: {
  scope: StaffPostingProposalScope;
  item: StaffPostingInboxItem;
  busy: boolean;
  onBusy: (value: boolean) => void;
  onContextChanged: () => void;
  admissionAvailable: boolean;
  onClose: () => void;
}) {
  const context = useStaffPostingCutoffContext(scope, item.proposal, busy);
  const cutoff = useStaffAccessCutoff([context.error], onContextChanged);
  const [notice, setNotice] = useState("");
  const [history, setHistory] = useState(false);
  const available = context.isSuccess && !context.isFetching;
  return (
    <Panel className="space-y-4" aria-label="Selected posting cutoff">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="font-semibold">{item.staff.display_name} · Posting cutoff</h3>
        <Button variant="ghost" onClick={onClose}>
          Close cutoff
        </Button>
      </div>
      {notice && <p role="status">{notice}</p>}
      {!cutoff && (
        <RefreshButton refresh={context.refetch} error={context.error} busy={context.isFetching}>
          Recheck posting cutoff
        </RefreshButton>
      )}
      {cutoff ? (
        <p role="alert">{STAFF_ACCESS_CHANGED}</p>
      ) : !available ? (
        <div role={context.isError ? "alert" : "status"}>
          {context.isError
            ? "The posting cutoff context could not be verified."
            : "Checking current cutoff history…"}
        </div>
      ) : (
        <>
          <dl className="grid gap-4 text-sm sm:grid-cols-2">
            <div className="min-w-0">
              <dt className="text-muted-foreground">Approved posting reference</dt>
              <dd className="break-all">{context.data.proposal.id}</dd>
            </div>
            <div className="min-w-0">
              <dt className="text-muted-foreground">Approved revision</dt>
              <dd className="break-all">{context.data.proposal.revision_id}</dd>
            </div>
            <div>
              <dt className="text-muted-foreground">Checked at (UTC)</dt>
              <dd>{context.data.checked_at}</dd>
            </div>
          </dl>
          {context.data.cutoff ? (
            <CutoffReceipt row={context.data.cutoff} />
          ) : (
            <p>No cutoff has been recorded.</p>
          )}
        </>
      )}
      {context.data && !cutoff && (
        <PostingCutoffForm
          scope={scope}
          proposal={context.data.proposal}
          current={context.data.cutoff}
          checkedAt={context.data.checked_at}
          available={available && admissionAvailable}
          onBusy={onBusy}
          onContextChanged={onContextChanged}
          onRecheck={() => void context.refetch()}
          onRecorded={() => {
            setNotice("Posting cutoff recorded. Approval history is preserved.");
            void context.refetch();
          }}
        />
      )}
      {!cutoff && (
        <Button
          variant="outline"
          disabled={busy || !available}
          onClick={() => setHistory((value) => !value)}
        >
          {history ? "Hide cutoff history" : "View cutoff history"}
        </Button>
      )}
      {history && available && !cutoff && (
        <CutoffHistory
          scope={scope}
          proposal={context.data.proposal}
          latestVersion={context.data.cutoff?.version ?? 0}
          onContextChanged={onContextChanged}
        />
      )}
    </Panel>
  );
}

function CutoffReceipt({ row }: { row: StaffPostingCutoff }) {
  return (
    <div className="space-y-2 text-sm">
      <p className="font-medium">
        Cutoff v{row.version} · {row.mode === "immediate" ? "Immediate" : "Scheduled"}
      </p>
      <p>Effective cutoff (UTC): {row.cutoff_at}</p>
      <p className="break-words">{row.reason}</p>
      <p className="text-muted-foreground">Recorded {row.recorded_at} (UTC)</p>
    </div>
  );
}

function CutoffHistory({
  scope,
  proposal,
  latestVersion,
  onContextChanged,
}: {
  scope: StaffPostingProposalScope;
  proposal: StaffPostingProposal;
  latestVersion: number;
  onContextChanged: () => void;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const history = useStaffPostingCutoffHistory(scope, proposal, cursor, latestVersion);
  const cutoff = useStaffAccessCutoff([history.error], onContextChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (history.isPending || history.isFetching) return <p role="status">Loading cutoff history…</p>;
  if (history.isError)
    return (
      <RequestFailure
        message="Cutoff history could not be verified."
        retry={history.refetch}
        error={history.error}
      />
    );
  return (
    <section aria-label="Cutoff history" className="space-y-3">
      <ol className="space-y-3">
        {history.data.data.map((row) => (
          <Panel as="li" key={row.id}>
            <CutoffReceipt row={row} />
          </Panel>
        ))}
      </ol>
      {history.data.data.length === 0 && <p>No cutoff history on this page.</p>}
      <div className="flex flex-wrap gap-2">
        {history.data.page.has_more && (
          <Button variant="outline" onClick={() => setCursor(history.data.page.next_cursor)}>
            More cutoff history
          </Button>
        )}
        {cursor && (
          <Button variant="ghost" onClick={() => setCursor(null)}>
            First cutoff history
          </Button>
        )}
      </div>
    </section>
  );
}
