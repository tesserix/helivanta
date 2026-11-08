"use client";
import { useEffect, useState } from "react";
import { Button } from "@tesserix/web";
import {
  Panel,
  RefreshButton,
  LoadMore,
  RequestFailure,
  useConfirmLeave,
  formatDateTime,
} from "@helivanta/ui";
import {
  usePartnerMemberHospitals,
  useHospitalPartnerGroups,
  usePartnerMembers,
  usePartnerMemberCapabilities,
  type PartnerGroupScope,
  type PartnerGroup,
  type PartnerGroupMemberProposal,
  type PartnerMemberCommandInput,
} from "@helivanta/api";
import { accessChanged } from "./errors";
import { MemberDecision } from "./member-decision";
import { MemberNomination } from "./member-nomination";

type Props = { scope: PartnerGroupScope; onChanged: () => void };
type MemberProps = Props & { canPrepare: boolean; canReview: boolean };
export function MemberTeams(props: Props) {
  const { onChanged } = props;
  const hospitals = usePartnerMemberHospitals(props.scope);
  const [busy, setBusy] = useState(false);
  const [hospital, setHospital] = useState<{ hospital_id: string; name: string } | null>(null);
  const confirm = useConfirmLeave();
  useEffect(() => {
    if (accessChanged(hospitals.error)) onChanged();
  }, [hospitals.error, onChanged]);
  if (hospitals.isPending) return <p role="status">Checking current Staff hospitals…</p>;
  if (hospitals.isError)
    return (
      <RequestFailure
        message="Current Staff hospitals are unavailable."
        error={hospitals.error}
        retry={hospitals.refetch}
      />
    );
  if (hospital)
    return (
      <section className="space-y-4">
        <h2 className="text-lg font-semibold">{hospital.name}</h2>
        <Button variant="outline" disabled={busy} onClick={() => confirm(() => setHospital(null))}>
          Change membership hospital
        </Button>
        <HospitalTeams
          key={hospital.hospital_id}
          {...props}
          hospital={hospital.hospital_id}
          busy={busy}
          onBusyChanged={setBusy}
        />
      </section>
    );
  return (
    <section aria-label="Hospital membership choices" className="space-y-3">
      <p>
        Choose your current hospital to manage its published receiving, procurement or finance
        teams.
      </p>
      {!hospitals.data.pages.some((page) => page.data.length > 0) && (
        <p>No current Staff hospitals are available for membership management.</p>
      )}
      {hospitals.data.pages
        .flatMap((page) => page.data)
        .map((row) => (
          <Button variant="outline" key={row.hospital_id} onClick={() => setHospital(row)}>
            Choose {row.name}
          </Button>
        ))}
      <LoadMore
        hasMore={hospitals.hasNextPage}
        isLoading={hospitals.isFetchingNextPage}
        onClick={() => hospitals.fetchNextPage()}
        label="Load more Staff hospitals"
      />
    </section>
  );
}
function HospitalTeams(
  props: Props & { hospital: string; busy: boolean; onBusyChanged: (busy: boolean) => void },
) {
  const { onChanged } = props;
  const groups = useHospitalPartnerGroups(props.scope, props.hospital);
  const capabilities = usePartnerMemberCapabilities(props.scope, props.hospital);
  const [group, setGroup] = useState<PartnerGroup | null>(null);
  const confirm = useConfirmLeave();
  useEffect(() => {
    if (accessChanged(groups.error) || accessChanged(capabilities.error)) onChanged();
  }, [groups.error, capabilities.error, onChanged]);
  if (capabilities.isPending)
    return <p role="status">Checking current hospital membership capabilities…</p>;
  if (capabilities.isError)
    return (
      <RequestFailure
        message="Hospital membership authority is unavailable."
        error={capabilities.error}
        retry={capabilities.refetch}
      />
    );
  if (groups.isPending) return <p role="status">Loading hospital-published teams…</p>;
  if (groups.isError)
    return (
      <RequestFailure
        message="Hospital partner groups are unavailable."
        error={groups.error}
        retry={groups.refetch}
      />
    );
  if (group)
    return (
      <section className="space-y-4">
        <Button
          variant="outline"
          disabled={props.busy}
          onClick={() => confirm(() => setGroup(null))}
        >
          Change membership team
        </Button>
        <MemberRoster
          key={group.id}
          {...props}
          group={group}
          canPrepare={capabilities.data.can_prepare}
          canReview={capabilities.data.can_review}
        />
      </section>
    );
  const rows = groups.data.pages.flatMap((page) => page.data);
  return (
    <section aria-label="Hospital-published teams" className="space-y-3">
      {!rows.length && <p>No published team proposals are available for this hospital.</p>}
      {rows.map((row) => (
        <Panel as="div" key={row.id}>
          <p>
            {row.name} · {row.purpose} · {row.status}
          </p>
          <Button variant="outline" onClick={() => setGroup(row)}>
            Members of {row.name}
          </Button>
        </Panel>
      ))}
      <LoadMore
        hasMore={groups.hasNextPage}
        isLoading={groups.isFetchingNextPage}
        onClick={() => groups.fetchNextPage()}
        label="Load more hospital teams"
      />
    </section>
  );
}
function MemberRoster(
  props: MemberProps & {
    hospital: string;
    group: PartnerGroup;
    busy: boolean;
    onBusyChanged: (busy: boolean) => void;
  },
) {
  const { onChanged } = props;
  const members = usePartnerMembers(props.scope, props.hospital, props.group.id);
  const [selected, setSelected] = useState<PartnerGroupMemberProposal | null>(null);
  const [message, setMessage] = useState("");
  const [nominating, setNominating] = useState(false);
  const confirm = useConfirmLeave();
  const operations = (
    member: PartnerGroupMemberProposal,
  ): PartnerMemberCommandInput["operation"][] => {
    if (member.status === "pending_review") {
      if (member.prepared_by === props.scope.subject && props.canPrepare) return ["withdraw"];
      if (
        member.prepared_by !== props.scope.subject &&
        member.input.subject !== props.scope.subject &&
        props.canReview
      )
        return ["approve", "reject"];
    }
    return member.status === "approved" && props.canReview ? ["suspend"] : [];
  };

  useEffect(() => {
    if (accessChanged(members.error)) onChanged();
  }, [members.error, onChanged]);
  return (
    <section aria-label="Hospital team membership" className="space-y-4">
      <h3 className="text-lg font-semibold">{props.group.name}</h3>
      {members.isSuccess && (
        <RefreshButton
          error={members.error}
          busy={props.busy || members.isFetching}
          refresh={() =>
            confirm(() => {
              setMessage("");
              void members.refetch();
            })
          }
        >
          Refresh memberships
        </RefreshButton>
      )}

      <p>
        Recorded membership is not current supplier access. Each disclosure still requires current
        Staff eligibility and an independently reviewed supplier grant.
      </p>
      {message && <p role="status">{message}</p>}
      {props.canPrepare && props.group.status === "approved" && (
        <Button
          variant="outline"
          disabled={props.busy || nominating}
          onClick={() =>
            confirm(() => {
              setSelected(null);
              setMessage("");
              setNominating(true);
            })
          }
        >
          Nominate a Staff member
        </Button>
      )}
      {nominating && (
        <MemberNomination
          scope={props.scope}
          group={props.group}
          busy={props.busy}
          onChanged={onChanged}
          onBusyChanged={props.onBusyChanged}
          onFinished={(recovered) => {
            setNominating(false);
            props.onBusyChanged(false);
            setMessage(
              recovered
                ? "Recorded membership nomination recovered."
                : "Membership nomination recorded for independent review.",
            );
          }}
          onCancel={() => {
            setNominating(false);
            props.onBusyChanged(false);
          }}
        />
      )}

      {selected && (
        <MemberDecision
          key={`${selected.id}:${selected.version}`}
          scope={props.scope}
          member={selected}
          operations={operations(selected)}
          onChanged={onChanged}
          onBusyChanged={props.onBusyChanged}
          onFinished={(recovered) => {
            setSelected(null);
            props.onBusyChanged(false);
            setMessage(
              recovered
                ? "Recorded membership decision recovered."
                : "Membership decision recorded.",
            );
          }}
          onCancel={() =>
            confirm(() => {
              setSelected(null);
              props.onBusyChanged(false);
            })
          }
        />
      )}
      {members.isPending ? (
        <p role="status">Loading retained membership evidence…</p>
      ) : members.isError ? (
        <RequestFailure
          message="Team memberships are unavailable."
          error={members.error}
          retry={members.refetch}
        />
      ) : (
        <>
          <ul className="space-y-3">
            {members.data.pages
              .flatMap((page) => page.data)
              .map((member) => (
                <Panel as="li" key={member.id} className="space-y-2">
                  <p className="font-medium">{member.member_display_name}</p>
                  <p>Recorded status: {member.status}</p>
                  <p>Membership expires {formatDateTime(member.input.expires_at)}</p>
                  <p>Source last checked {formatDateTime(member.source_checked_at)}</p>
                  {operations(member).length > 0 && (
                    <Button
                      variant="outline"
                      disabled={props.busy}
                      onClick={() =>
                        confirm(() => {
                          setMessage("");
                          setNominating(false);
                          setSelected(member);
                        })
                      }
                    >
                      Review {member.member_display_name}
                    </Button>
                  )}
                </Panel>
              ))}
          </ul>
          <LoadMore
            hasMore={members.hasNextPage}
            isLoading={members.isFetchingNextPage || props.busy}
            onClick={() => members.fetchNextPage()}
            label="Load more memberships"
          />
        </>
      )}
    </section>
  );
}
