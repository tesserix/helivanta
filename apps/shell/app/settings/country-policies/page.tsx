import { HmsShell } from "@helivanta/ui";
import { TenantPicker } from "../../../components/tenant-picker";
import { CountryPolicyWorkspace } from "../../../components/country-policy/workspace";

export default function CountryPoliciesPage() {
  return (
    <HmsShell active="/settings/country-policies" tenantPicker={<TenantPicker />}>
      <CountryPolicyWorkspace />
    </HmsShell>
  );
}
