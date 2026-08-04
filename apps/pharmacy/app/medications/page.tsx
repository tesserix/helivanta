import { HmsShell } from "@hms/ui";
import { MedicationsPanel } from "@/components/medications-panel";

export default function MedicationsPage() {
  return (
    <HmsShell active="/pharmacy/medications">
      <MedicationsPanel />
    </HmsShell>
  );
}
