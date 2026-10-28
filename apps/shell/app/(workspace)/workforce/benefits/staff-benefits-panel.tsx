"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import {
  dependentsKey,
  dependentsPath,
  useApiPagedQuery,
  type Dependent,
  type Hospital,
  type Staff,
} from "@helivanta/api";
import { Field, SelectControl } from "@helivanta/ui";
import { Failure } from "../roster-policy/policy-ui";
import { DependentsSection } from "./dependents-section";
import { EnrollmentsSection } from "./enrollments-section";

export function StaffBenefitsPanel({
  hospital,
  writable,
  approver,
}: {
  hospital: Hospital;
  writable: boolean;
  approver: boolean;
}) {
  const staff = useApiPagedQuery<Staff>(
    ["workforce", "staff", hospital.id],
    `/workforce/hospitals/${hospital.id}/staff`,
  );
  const [selected, setSelected] = useState("");
  const member = staff.items.find((s) => s.id === selected);
  return (
    <section
      aria-label="Staff benefits"
      className="overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <div className="flex flex-wrap items-end justify-between gap-4 p-5">
        <div>
          <h3 className="font-semibold">Staff benefits</h3>
          <p className="mt-1 max-w-2xl text-xs text-muted-foreground">
            An employee&apos;s dependents and the coverage each plan gives them. Coverage takes
            effect only once a second person approves it.
          </p>
        </div>
        <div className="w-full sm:w-80">
          <Field id="benefits-member" label="Staff member">
            <SelectControl
              id="benefits-member"
              value={selected}
              placeholder={staff.isPending ? "Loading…" : "Choose a staff member"}
              onValueChange={setSelected}
              options={staff.items.map((s) => ({
                value: s.id,
                label: `${s.display_name} · ${s.employee_code}${s.employment_status === "inactive" ? " (inactive)" : ""}`,
              }))}
            />
          </Field>
        </div>
      </div>
      {staff.isError ? (
        <div className="p-5">
          <Failure message="Could not load staff." retry={staff.retry} />
        </div>
      ) : member ? (
        <StaffBenefits
          key={member.id}
          hospital={hospital}
          member={member}
          writable={writable}
          approver={approver}
        />
      ) : (
        <p className="border-t p-5 text-sm text-muted-foreground">
          Choose a staff member to see their dependents and coverage.
        </p>
      )}
      {staff.hasMore && (
        <div className="px-5 pb-5">
          <Button variant="outline" disabled={staff.isFetchingMore} onClick={staff.loadMore}>
            Load more staff
          </Button>
        </div>
      )}
    </section>
  );
}

function StaffBenefits({
  hospital,
  member,
  writable,
  approver,
}: {
  hospital: Hospital;
  member: Staff;
  writable: boolean;
  approver: boolean;
}) {
  const scope = { hospitalId: hospital.id, staffId: member.id };
  const dependents = useApiPagedQuery<Dependent>(dependentsKey(scope), dependentsPath(scope));
  const active = member.employment_status === "active";
  return (
    <>
      {!active && (
        <p className="border-t px-5 py-3 text-sm text-muted-foreground">
          Inactive staff take no new dependents or coverage. Existing coverage can still be ended.
        </p>
      )}
      <DependentsSection
        scope={scope}
        dependents={dependents}
        writable={writable}
        adding={writable && active}
      />
      <EnrollmentsSection
        scope={scope}
        member={member}
        dependents={dependents}
        writable={writable}
        adding={writable && active}
        approver={approver}
      />
    </>
  );
}
