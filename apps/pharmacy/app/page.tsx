import { HmsShell } from "@hms/ui";
import { DispenseList } from "@/components/dispense-list";

export default function DispensesPage() {
  return (
    <HmsShell active="/pharmacy">
      <h1 className="mb-6 text-2xl font-semibold">Pharmacy — Dispenses</h1>
      <DispenseList />
    </HmsShell>
  );
}
