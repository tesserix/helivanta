import { HmsShell } from "@helivanta/ui";
import { EmergencyReceivingWorkspace } from "../../../components/emergency/emergency-receiving";
export default function ReceivingPage() {
  return (
    <HmsShell active="/emergency/receiving">
      <EmergencyReceivingWorkspace />
    </HmsShell>
  );
}
