import { OpdEncounters } from "@/components/medicore/opd-encounters";
import { PingPanel } from "@/components/medicore/ping-panel";
import { VisitPanel } from "@/components/medicore/visit-panel";

export default function OpdPage() {
  return (
    <div className="space-y-6">
      <OpdEncounters />
      {/* Legacy visits keep feeding pharmacy and lab until they consume
          encounters (#80 slice 2). */}
      <VisitPanel department="OPD" />
      <PingPanel department="OPD" />
    </div>
  );
}
