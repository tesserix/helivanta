import { DispenseList } from "@/components/pharmacy/dispense-list";
import { WorkspaceConnections } from "@/components/workspace-connections";

export default function DispensesPage() {
  return (
    <div className="space-y-6">
      <WorkspaceConnections current="pharmacy" />
      <DispenseList />
    </div>
  );
}
