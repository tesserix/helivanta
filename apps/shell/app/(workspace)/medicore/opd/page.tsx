import { PingPanel } from "@/components/medicore/ping-panel";
import { VisitPanel } from "@/components/medicore/visit-panel";

export default function OpdPage() {
  return (
    <div className="space-y-6">
      <VisitPanel department="OPD" />
      <PingPanel department="OPD" />
    </div>
  );
}
