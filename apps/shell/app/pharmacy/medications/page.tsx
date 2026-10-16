import { HmsShell } from "@helivanta/ui";
import { MedicationsPanel } from "@/components/pharmacy/medications-panel";
import { WorkspaceConnections } from "@/components/workspace-connections";

export default function MedicationsPage() {
  return (
    <HmsShell active="/pharmacy/medications">
      <div className="space-y-6">
        <WorkspaceConnections current="pharmacy" />
        <MedicationsPanel />
      </div>
    </HmsShell>
  );
}
