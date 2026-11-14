"use client";

import { useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { Button } from "@tesserix/web";
import {
  Field,
  Panel,
  RequestFailure,
  RefreshButton,
  SelectControl,
  formatDateTime,
} from "@helivanta/ui";
import {
  listGovernanceAssignmentScopes,
  listGovernanceAssignmentTemplates,
  listGovernanceAssignments,
  getGovernanceAssignment,
  listGovernanceAssignmentHistory,
  listGovernanceAssignmentDecisions,
  governanceAssignmentScopesKey,
  governanceAssignmentsKey,
  staffAccessTemplates,
  type GovernanceAssignmentHospital,
  type GovernanceAssignmentScope,
  type StaffAssignmentScope,
  type StaffAssignmentFamily,
  type StaffAssignmentDecision,
} from "@helivanta/api";
import { AssignmentDecisionForm } from "./assignment-forms";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "../staff-access-cutoff";
import { useNow } from "@/lib/use-now";

export function GovernanceAssignments({ scope }: { scope: StaffAssignmentScope }) {
  const [hospital, setHospital] = useState<GovernanceAssignmentHospital | null>(null);
  const [cutoff, setCutoff] = useState(false);
  const hospitals = useInfiniteQuery({
    queryKey: governanceAssignmentScopesKey(scope),
    queryFn: ({ pageParam, signal }) => listGovernanceAssignmentScopes(scope, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.page.next_cursor ?? undefined,
    staleTime: 0,
    gcTime: 0,
    retry: false,
    enabled: hospital === null && !cutoff,
  });
  const accessLost = useStaffAccessCutoff([hospitals.error], () => setCutoff(true));
  if (cutoff || accessLost) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (hospital)
    return (
      <Panel className="space-y-4">
        <h2 className="text-lg font-semibold">{hospital.name} · Assignment reviews</h2>
        <HospitalAssignments
          key={hospital.hospital_id}
          scope={{ ...scope, hospitalId: hospital.hospital_id }}
          hospital={hospital}
          onAccessLost={() => setCutoff(true)}
          onCloseHospital={() => setHospital(null)}
        />
      </Panel>
    );
  return (
    <Panel className="space-y-4">
      <h2 className="text-lg font-semibold">Hospital assignment reviews</h2>
      {hospitals.isPending ? (
        <p role="status">Checking authorized hospitals…</p>
      ) : hospitals.isError ? (
        <RequestFailure
          message="Could not verify hospital access."
          error={hospitals.error}
          retry={hospitals.refetch}
        />
      ) : (
        <>
          {hospitals.data.pages.every((page) => page.data.length === 0) && (
            <p>No hospitals are available on this page.</p>
          )}
          {hospitals.data.pages
            .flatMap((page) => page.data)
            .map((row) => (
              <div key={row.hospital_id}>
                <Button variant="outline" onClick={() => setHospital(row)}>
                  Open {row.name}
                </Button>
              </div>
            ))}
          {hospitals.hasNextPage && (
            <Button
              disabled={hospitals.isFetchingNextPage}
              onClick={() => void hospitals.fetchNextPage()}
            >
              More hospitals
            </Button>
          )}
        </>
      )}
    </Panel>
  );
}

function HospitalAssignments({
  scope,
  hospital,
  onAccessLost,
  onCloseHospital,
}: {
  scope: GovernanceAssignmentScope;
  hospital: GovernanceAssignmentHospital;
  onAccessLost: () => void;
  onCloseHospital: () => void;
}) {
  const [family, setFamily] = useState<StaffAssignmentFamily>("leave");
  const [selected, setSelected] = useState<string | null>(null);
  const templates = useQuery({
    queryKey: [...governanceAssignmentsKey(scope), "templates"],
    queryFn: ({ signal }) => listGovernanceAssignmentTemplates(scope, signal),
    staleTime: 0,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
    enabled: selected === null,
  });
  const records = useInfiniteQuery({
    queryKey: [...governanceAssignmentsKey(scope), "list", family],
    queryFn: ({ pageParam, signal }) =>
      listGovernanceAssignments(scope, { family, cursor: pageParam, signal }),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.page.next_cursor ?? undefined,
    staleTime: 0,
    gcTime: 0,
    retry: false,
    enabled: selected === null && templates.isSuccess && !templates.isFetching,
  });
  const cutoff = useStaffAccessCutoff([records.error, templates.error], onAccessLost);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (selected)
    return (
      <AssignmentReview
        key={selected}
        scope={scope}
        id={selected}
        hospital={hospital}
        onAccessLost={onAccessLost}
        onClose={() => setSelected(null)}
      />
    );
  if (templates.isPending || templates.isFetching)
    return <p role="status">Checking the hospital assignment catalog…</p>;
  if (templates.isError)
    return (
      <RequestFailure
        message="Could not verify assignment templates."
        error={templates.error}
        retry={templates.refetch}
      />
    );
  const families = [...new Set(templates.data.map((template) => template.family))];
  return (
    <div className="space-y-4">
      <Button variant="outline" onClick={onCloseHospital}>
        Choose another hospital
      </Button>
      <Field id="governance-assignment-family" label="Assignment area">
        <SelectControl
          id="governance-assignment-family"
          value={family}
          onValueChange={(value) => setFamily(value as StaffAssignmentFamily)}
          options={families.map((value) => ({ value, label: value.replaceAll("_", " ") }))}
        />
      </Field>
      <RefreshButton
        busy={records.isFetching || templates.isFetching}
        error={records.error ?? templates.error}
        refresh={() => Promise.all([templates.refetch(), records.refetch()])}
      >
        Refresh hospital assignments
      </RefreshButton>
      {records.isPending || records.isFetching ? (
        <p role="status">Checking current assignments…</p>
      ) : records.isError ? (
        <RequestFailure
          message="Could not verify assignments."
          error={records.error}
          retry={records.refetch}
        />
      ) : (
        <>
          {records.data.pages.every((page) => page.data.length === 0) && (
            <p>No assignments on this page.</p>
          )}
          {records.data.pages
            .flatMap((page) => page.data)
            .map((item) => (
              <section key={item.id} className="space-y-2 border-t border-border py-3">
                <h3 className="font-semibold">
                  {
                    staffAccessTemplates.find(
                      (template) => template.key === item.input.template_key,
                    )?.name
                  }{" "}
                  · {item.input.subject}
                </h3>
                <p>
                  {item.status.replaceAll("_", " ")} · Version {item.version}
                </p>
                <Button variant="outline" onClick={() => setSelected(item.id)}>
                  Review assignment
                </Button>
              </section>
            ))}
          {records.hasNextPage && (
            <Button
              disabled={records.isFetchingNextPage}
              onClick={() => void records.fetchNextPage()}
            >
              More assignments
            </Button>
          )}
        </>
      )}
    </div>
  );
}

function AssignmentReview({
  scope,
  id,
  hospital,
  onAccessLost,
  onClose,
}: {
  scope: GovernanceAssignmentScope;
  id: string;
  hospital: GovernanceAssignmentHospital;
  onAccessLost: () => void;
  onClose: () => void;
}) {
  const [decision, setDecision] = useState<StaffAssignmentDecision["decision"] | null>(null);
  const [history, setHistory] = useState(false);
  const current = useQuery({
    queryKey: [...governanceAssignmentsKey(scope), "detail", id],
    queryFn: ({ signal }) => getGovernanceAssignment(scope, id, signal),
    enabled: decision === null,
    refetchOnWindowFocus: false,
    staleTime: 0,
    gcTime: 0,
    retry: false,
  });
  const cutoff = useStaffAccessCutoff([current.error], onAccessLost);
  const now = useNow();
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (current.isPending || current.isFetching)
    return <p role="status">Rechecking the selected assignment…</p>;
  if (current.isError)
    return (
      <RequestFailure
        message="Could not verify the selected assignment."
        error={current.error}
        retry={current.refetch}
      />
    );
  const item = current.data;
  const independent = scope.subject !== item.proposed_by && scope.subject !== item.input.subject;
  const reviewable =
    hospital.can_review &&
    independent &&
    item.status === "pending_review" &&
    Date.parse(item.input.expires_at) > now;
  const withdrawable =
    hospital.can_manage &&
    scope.subject === item.proposed_by &&
    !["withdrawn", "rejected", "scope_rejected"].includes(item.status);
  return (
    <section className="space-y-4" aria-label="Current assignment review">
      <h3 className="font-semibold">
        {staffAccessTemplates.find((template) => template.key === item.input.template_key)?.name}
      </h3>
      <p>Account: {item.input.subject}</p>
      <p>Proposed by: {item.proposed_by}</p>
      <p>{item.input.reason}</p>
      <p>
        {item.status.replaceAll("_", " ")} · Version {item.version}
      </p>
      {item.input.department_id && <p>Department reference: {item.input.department_id}</p>}
      <p>
        {formatDateTime(item.input.valid_from)} – {formatDateTime(item.input.expires_at)}
      </p>
      {decision ? (
        <AssignmentDecisionForm
          scope={scope}
          nativeScope={scope}
          item={item}
          decision={decision}
          onAccessLost={onAccessLost}
          onClose={() => setDecision(null)}
          onSaved={() => {
            setDecision(null);
          }}
        />
      ) : (
        <div className="flex flex-wrap gap-2">
          {reviewable && (
            <>
              <Button onClick={() => setDecision("approve")}>Approve</Button>
              <Button variant="outline" onClick={() => setDecision("reject")}>
                Reject
              </Button>
            </>
          )}
          {withdrawable && (
            <Button variant="outline" onClick={() => setDecision("withdraw")}>
              Withdraw
            </Button>
          )}
          <Button variant="outline" onClick={() => setHistory(!history)}>
            {history ? "Close history" : "History"}
          </Button>
          <RefreshButton busy={current.isFetching} error={current.error} refresh={current.refetch}>
            Refresh selected assignment
          </RefreshButton>
          <Button variant="ghost" onClick={onClose}>
            Back to assignments
          </Button>
        </div>
      )}
      {history && <AssignmentHistory scope={scope} id={id} onAccessLost={onAccessLost} />}
    </section>
  );
}

function AssignmentHistory({
  scope,
  id,
  onAccessLost,
}: {
  scope: GovernanceAssignmentScope;
  id: string;
  onAccessLost: () => void;
}) {
  const history = useInfiniteQuery({
    queryKey: [...governanceAssignmentsKey(scope), "history", id],
    queryFn: ({ pageParam, signal }) =>
      listGovernanceAssignmentHistory(scope, id, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.page.next_cursor ?? undefined,
    staleTime: 0,
    gcTime: 0,
    retry: false,
  });
  const decisions = useInfiniteQuery({
    queryKey: [...governanceAssignmentsKey(scope), "decisions", id],
    queryFn: ({ pageParam, signal }) =>
      listGovernanceAssignmentDecisions(scope, id, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.page.next_cursor ?? undefined,
    staleTime: 0,
    gcTime: 0,
    retry: false,
  });
  const cutoff = useStaffAccessCutoff([history.error, decisions.error], onAccessLost);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (history.isPending || decisions.isPending)
    return <p role="status">Checking assignment history…</p>;
  if (history.isError || decisions.isError)
    return (
      <RequestFailure
        message="Could not verify assignment history."
        error={history.error ?? decisions.error}
        retry={() => Promise.all([history.refetch(), decisions.refetch()])}
      />
    );
  return (
    <section aria-label="Assignment history" className="space-y-3">
      <h4 className="font-semibold">Assignment history</h4>
      {history.data.pages
        .flatMap((page) => page.data)
        .map((row) => (
          <p key={row.version}>
            Version {row.version} · {row.status.replaceAll("_", " ")} ·{" "}
            {formatDateTime(row.created_at)}
          </p>
        ))}
      {history.hasNextPage && (
        <Button disabled={history.isFetchingNextPage} onClick={() => void history.fetchNextPage()}>
          More history
        </Button>
      )}
      {decisions.data.pages
        .flatMap((page) => page.data)
        .map((row) => (
          <p key={row.id}>
            {row.actor_subject} · {row.decision} · {row.reason} · Version {row.result_version}
          </p>
        ))}
      {decisions.hasNextPage && (
        <Button
          disabled={decisions.isFetchingNextPage}
          onClick={() => void decisions.fetchNextPage()}
        >
          More decisions
        </Button>
      )}
    </section>
  );
}
