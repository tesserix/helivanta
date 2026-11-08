"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import { Panel, RequestFailure, useConfirmLeave } from "@helivanta/ui";
import {
  useStaffPostingScopes,
  useStaffPostingTargets,
  type StaffPostingHospitalScope,
  type StaffPostingTarget,
  type EnrollmentRequestScope,
} from "@helivanta/api";
import { StaffPostingDraftPanel } from "./posting-drafts-panel";
import { useStaffAccessCutoff, STAFF_ACCESS_CHANGED } from "./staff-access-cutoff";
export function StaffPostingWorkspace({
  scope,
  onContextChanged,
}: {
  scope: EnrollmentRequestScope;
  onContextChanged: () => void;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const confirmLeave = useConfirmLeave();
  const hospitals = useStaffPostingScopes(scope, cursor);
  const cutoff = useStaffAccessCutoff([hospitals.error], onContextChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (hospitals.isPending) return <p role="status">Checking posting hospitals…</p>;
  if (hospitals.isError)
    return (
      <RequestFailure
        message="Posting hospital access could not be verified."
        retry={hospitals.refetch}
        error={hospitals.error}
      />
    );
  const hospital = hospitals.data.data.find((row) => row.hospital_id === selected);
  return (
    <section className="space-y-5" aria-label="Scoped posting workspace">
      <p className="max-w-3xl text-sm text-muted-foreground">
        Read or prepare department placements within your approved hospital scope. Posting drafts
        require independent review before they can support an effective placement.
      </p>
      <div className="flex flex-wrap gap-3">
        {hospitals.data.data.map((row) => (
          <Button
            key={row.hospital_id}
            variant={row.hospital_id === selected ? "default" : "outline"}
            aria-label={`Open posting hospital ${row.name}`}
            onClick={() => confirmLeave(() => setSelected(row.hospital_id))}
          >
            {row.name} · {row.can_prepare ? "Preparer" : "Reader"}
          </Button>
        ))}
      </div>
      {hospitals.data.data.length === 0 && <p>No approved posting hospitals on this page.</p>}
      {selected && !hospital && (
        <p role="alert">
          The selected hospital is no longer available. Reopen your posting workspace.
        </p>
      )}
      {hospital && (
        <PostingHospital
          key={hospital.hospital_id}
          scope={scope}
          hospital={hospital}
          onContextChanged={onContextChanged}
        />
      )}
      {hospitals.data.page.has_more && (
        <Button
          variant="outline"
          onClick={() =>
            confirmLeave(() => {
              setSelected(null);
              setCursor(hospitals.data.page.next_cursor);
            })
          }
        >
          More posting hospitals
        </Button>
      )}
      {cursor && (
        <Button
          variant="ghost"
          onClick={() =>
            confirmLeave(() => {
              setSelected(null);
              setCursor(null);
            })
          }
        >
          First posting hospitals
        </Button>
      )}
    </section>
  );
}
function PostingHospital({
  scope,
  hospital,
  onContextChanged,
}: {
  scope: EnrollmentRequestScope;
  hospital: StaffPostingHospitalScope;
  onContextChanged: () => void;
}) {
  const [cursor, setCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<StaffPostingTarget | null>(null);
  const confirmLeave = useConfirmLeave();
  const workspace = { ...scope, hospitalId: hospital.hospital_id };
  const targets = useStaffPostingTargets(workspace, cursor);
  const cutoff = useStaffAccessCutoff([targets.error], onContextChanged);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (targets.isPending) return <p role="status">Loading posting Staff choices…</p>;
  if (targets.isError)
    return (
      <RequestFailure
        message="Staff choices could not be verified."
        retry={targets.refetch}
        error={targets.error}
      />
    );
  return (
    <Panel className="space-y-4">
      <h2 className="font-semibold">{hospital.name} · Staff placement drafts</h2>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {targets.data.data.map((row) => (
          <Panel key={row.id} className="flex flex-col items-start gap-3">
            <div>
              <p className="font-medium">{row.display_name}</p>
              <p className="text-sm text-muted-foreground">{row.employee_code}</p>
            </div>
            <Button
              variant="outline"
              aria-label={`Posting preparation for ${row.display_name}`}
              onClick={() => confirmLeave(() => setSelected(row))}
            >
              Open posting drafts
            </Button>
          </Panel>
        ))}
      </div>
      {targets.data.data.length === 0 && <p>No Staff choices on this page.</p>}
      {selected && (
        <StaffPostingDraftPanel
          key={`${selected.id}:${hospital.can_prepare}`}
          scope={{ ...workspace, staffId: selected.id, access: "scoped" }}
          staffLabel={`${selected.display_name} · ${selected.employee_code}`}
          canManage={hospital.can_prepare}
          onClose={() => setSelected(null)}
          onContextChanged={onContextChanged}
        />
      )}
      {targets.data.page.has_more && (
        <Button
          variant="outline"
          onClick={() =>
            confirmLeave(() => {
              setSelected(null);
              setCursor(targets.data.page.next_cursor);
            })
          }
        >
          More posting Staff choices
        </Button>
      )}
      {cursor && (
        <Button
          variant="ghost"
          onClick={() =>
            confirmLeave(() => {
              setSelected(null);
              setCursor(null);
            })
          }
        >
          First posting Staff choices
        </Button>
      )}
    </Panel>
  );
}
