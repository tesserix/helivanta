import { HmsShell } from "@helivanta/ui";
import { MedicationsPanel } from "@/components/pharmacy/medications-panel";

export default function MedicationsPage() {
  return (
    <HmsShell active="/pharmacy/medications">
      <MedicationsPanel />
    </HmsShell>
  );
}
