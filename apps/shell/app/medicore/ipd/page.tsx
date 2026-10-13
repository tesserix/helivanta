import { HmsShell } from "@helivanta/ui";
import { PingPanel } from "@/components/medicore/ping-panel";

export default function IpdPage() {
  return (
    <HmsShell active="/medicore/ipd">
      <PingPanel department="IPD" />
    </HmsShell>
  );
}
