import { HmsShell } from "@hms/ui";
import { PingPanel } from "@/components/ping-panel";

export default function IpdPage() {
  return (
    <HmsShell active="/medicore/ipd">
      <PingPanel department="IPD" />
    </HmsShell>
  );
}
