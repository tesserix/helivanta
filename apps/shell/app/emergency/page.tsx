import { HmsShell } from "@helivanta/ui";
import { EmergencyWorkspace } from "../../components/emergency/emergency-workspace";
export default function EmergencyPage() {
  return (
    <HmsShell active="/emergency">
      <EmergencyWorkspace />
    </HmsShell>
  );
}
