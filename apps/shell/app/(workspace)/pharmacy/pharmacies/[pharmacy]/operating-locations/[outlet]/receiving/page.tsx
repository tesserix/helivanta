import { ReceiverDuty } from "@/components/pharmacy/receiver-duty";
export default async function ReceiverDutyPage({
  params,
}: {
  params: Promise<{ pharmacy: string; outlet: string }>;
}) {
  const { pharmacy, outlet } = await params;
  return <ReceiverDuty pharmacyId={pharmacy} outletId={outlet} />;
}
