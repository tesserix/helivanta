"use client";

import { useEffect, useRef, useState } from "react";
import { isEnrollmentAccessChanged } from "@helivanta/api";

export const STAFF_ACCESS_CHANGED =
  "Staff access changed. Reopen Staff to verify your current workspace.";

export function useStaffAccessCutoff(
  errors: readonly unknown[],
  onCutoff?: () => void,
  isCutoff: (error: unknown) => boolean = isEnrollmentAccessChanged,
): boolean {
  const denied = errors.some(isCutoff);
  const [cutoff, setCutoff] = useState(false);
  const notified = useRef(false);
  if (denied && !cutoff) setCutoff(true);
  useEffect(() => {
    if (denied && onCutoff && !notified.current) {
      notified.current = true;
      onCutoff();
    }
  }, [denied, onCutoff]);
  return cutoff || denied;
}
