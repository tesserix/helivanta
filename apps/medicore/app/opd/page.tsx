import { HmsShell } from "@hms/ui";
import { PingPanel } from "@/components/ping-panel";

export default function OpdPage() {
  return (
    <HmsShell active="/medicore/opd">
      <h1 className="mb-6 text-2xl font-semibold">OPD — Outpatients</h1>
      <PingPanel department="OPD" />
    </HmsShell>
  );
}
