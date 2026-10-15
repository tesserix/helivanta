import { HmsShell } from "@helivanta/ui";
import { PartiesWorkspace } from "../../../components/supply/parties-workspace";
export default function SupplyPartiesPage() {
  return (
    <HmsShell active="/supply/parties">
      <PartiesWorkspace />
    </HmsShell>
  );
}
