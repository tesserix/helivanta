import { HmsShell } from "@hms/ui";
import { MedicationsPanel } from "@/components/medications-panel";

export default function MedicationsPage() {
  return (
    <HmsShell active="/pharmacy/medications">
      <h1 className="mb-6 text-2xl font-semibold">Pharmacy — Medications</h1>
      <MedicationsPanel />
    </HmsShell>
  );
}
