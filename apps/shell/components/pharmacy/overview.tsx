"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";
import { usePermissions } from "@helivanta/api";
import { PharmacyWorkspace } from "./pharmacy-workspace";

// The PharmaConnect overview (#576). Someone without an outlet role (a doctor
// reading the queue) has no overview and is sent on to the queue.
export function PharmacyOverview() {
  const { can, isLoading } = usePermissions();
  const router = useRouter();
  const allowed = can("pharmacy.catalogue.read");
  useEffect(() => {
    if (!isLoading && !allowed) router.replace("/pharmacy/dispense");
  }, [isLoading, allowed, router]);
  if (isLoading || !allowed) return <p role="status">Loading…</p>;
  return <PharmacyWorkspace view="overview" />;
}
