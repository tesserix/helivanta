import { HmsShell } from "@helivanta/ui";
import { PharmacyWorkspace } from "../../../components/pharmacy/pharmacy-workspace";
export default function CataloguePage() {
  return (
    <HmsShell active="/pharmacy/catalogue">
      <PharmacyWorkspace view="catalogue" />
    </HmsShell>
  );
}
