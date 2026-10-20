import { MedicationsPanel } from "@/components/pharmacy/medications-panel";
import { WorkspaceConnections } from "@/components/workspace-connections";

export default function MedicationsPage() {
  return (
    <div className="space-y-6">
      <WorkspaceConnections current="pharmacy" />
      <MedicationsPanel />
    </div>
  );
}
