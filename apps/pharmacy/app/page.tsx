import { HmsShell } from "@helivanta/ui";
import { DispenseList } from "@/components/dispense-list";

export default function DispensesPage() {
  return (
    <HmsShell active="/pharmacy">
      <DispenseList />
    </HmsShell>
  );
}
