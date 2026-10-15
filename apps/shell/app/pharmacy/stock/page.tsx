import { HmsShell } from "@helivanta/ui";
import { PharmacyWorkspace } from "../../../components/pharmacy/pharmacy-workspace";
export default function StockPage() {
  return (
    <HmsShell active="/pharmacy/stock">
      <PharmacyWorkspace view="stock" />
    </HmsShell>
  );
}
