import { HmsShell } from "@helivanta/ui";
import { EmergencyCrewWorkspace } from "../../../components/emergency/emergency-crew";

export default function EmergencyCrewPage() {
  return (
    <HmsShell active="/emergency/crew">
      <EmergencyCrewWorkspace />
    </HmsShell>
  );
}
