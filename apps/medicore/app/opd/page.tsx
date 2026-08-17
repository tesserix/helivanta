import { HmsShell } from "@helivanta/ui";
import { PingPanel } from "@/components/ping-panel";
import { VisitPanel } from "@/components/visit-panel";

export default function OpdPage() {
  return (
    <HmsShell active="/medicore/opd">
      <div className="space-y-6">
        <VisitPanel department="OPD" />
        <PingPanel department="OPD" />
      </div>
    </HmsShell>
  );
}
