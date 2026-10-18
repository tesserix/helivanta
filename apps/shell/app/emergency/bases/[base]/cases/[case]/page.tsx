import { HmsShell } from "@helivanta/ui";
import { EmergencyCaseDetailPanel } from "../../../../../../components/emergency/emergency-case-detail";
export default async function EmergencyCasePage({
  params,
}: {
  params: Promise<{ base: string; case: string }>;
}) {
  const { base, case: caseID } = await params;
  return (
    <HmsShell active="/emergency">
      <EmergencyCaseDetailPanel baseID={base} caseID={caseID} />
    </HmsShell>
  );
}
