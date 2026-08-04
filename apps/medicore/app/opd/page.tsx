import { HmsShell } from "@hms/ui";
import { PingPanel } from "@/components/ping-panel";
import { VisitPanel } from "@/components/visit-panel";

export default function OpdPage() {
  return (
    <HmsShell active="/medicore/opd">
      <h1 className="mb-6 text-2xl font-semibold">OPD — Outpatients</h1>
      <div className="space-y-10">
        <VisitPanel department="OPD" />
        <PingPanel department="OPD" />
      </div>
    </HmsShell>
  );
}
