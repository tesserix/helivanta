"use client";

import { useEffect, useState } from "react";
import { Button } from "@tesserix/web";
import {
  isEnrollmentAccessChanged,
  pharmacyStaffKey,
  savePharmacyEnrollment,
  useApiMutation,
  usePharmacyEnrollment,
  usePharmacySupervisorChoices,
  type EnrollmentInput,
  type EnrollmentRequestScope,
  type PharmacyStaff,
} from "@helivanta/api";
import { Panel, RequestFailure } from "@helivanta/ui";
import { EnrollmentForm, enrollmentInputFrom } from "@/app/(workspace)/workforce/enrollment-editor";

/** A pharmacy employer has no HR masters (ADR-0063): profile choices stay free text and contract type is not offered. */
export function PharmacyEnrollmentEditor({
  scope,
  pharmacy,
  staff,
  onClose,
  onContextChanged,
}: {
  scope: EnrollmentRequestScope;
  pharmacy: { pharmacy_id: string; name: string };
  staff: PharmacyStaff;
  onClose: () => void;
  onContextChanged: () => void;
}) {
  const [attempt, setAttempt] = useState(0);
  const query = usePharmacyEnrollment(scope, pharmacy.pharmacy_id, staff.id, attempt);
  const supervisors = usePharmacySupervisorChoices(scope, pharmacy.pharmacy_id);
  const save = useApiMutation(
    (input: EnrollmentInput) =>
      savePharmacyEnrollment(scope, pharmacy.pharmacy_id, staff.id, input),
    {
      invalidate: [pharmacyStaffKey(scope, pharmacy.pharmacy_id)],
      successToast: "Staff enrollment saved",
      onSuccess: onClose,
      suppressErrorToast: true,
    },
  );
  const accessChanged = [query.error, supervisors.error, save.error].some(
    isEnrollmentAccessChanged,
  );
  useEffect(() => {
    if (accessChanged) onContextChanged();
  }, [accessChanged, onContextChanged]);
  if (query.isPending)
    return (
      <Panel role="status" className="mt-6">
        Loading protected enrollment…
      </Panel>
    );
  if (query.isError)
    return (
      <div className="mt-6 space-y-3">
        <RequestFailure
          message="Could not load enrollment. Your existing record has not been changed."
          retry={query.refetch}
          error={query.error}
        />
        <Button variant="ghost" onClick={onClose}>
          Cancel
        </Button>
      </div>
    );
  return (
    <EnrollmentForm
      key={attempt}
      employer={pharmacy.name}
      self={staff.id}
      editing
      value={enrollmentInputFrom(query.data)}
      onClose={onClose}
      onReload={() => setAttempt((value) => value + 1)}
      hrIntake={false}
      supervisors={supervisors}
      masters={null}
      save={save}
      accessChanged={accessChanged}
    />
  );
}
