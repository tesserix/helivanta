import { HmsShell } from "@helivanta/ui";
import { PharmacyWorkspace } from "../../../components/pharmacy/pharmacy-workspace";
export default function AccountsPage() {
  return (
    <HmsShell active="/pharmacy/accounts">
      <PharmacyWorkspace view="accounts" />
    </HmsShell>
  );
}
