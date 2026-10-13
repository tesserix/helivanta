import { HmsShell } from "@helivanta/ui";
import { TenantPicker } from "@/components/tenant-picker";
import { PatientWorkspace } from "@/components/patients/patient-workspace";

export default function PatientsPage() {
  return (
    <HmsShell active="/patients" tenantPicker={<TenantPicker />}>
      <PatientWorkspace />
    </HmsShell>
  );
}
