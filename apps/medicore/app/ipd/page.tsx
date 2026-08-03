import { HmsShell } from "@/components/hms-shell";
import { PingPanel } from "@/components/ping-panel";

export default function IpdPage() {
  return (
    <HmsShell active="/medicore/ipd">
      <h1 className="mb-6 text-2xl font-semibold">IPD — Wards</h1>
      <PingPanel department="IPD" />
    </HmsShell>
  );
}
