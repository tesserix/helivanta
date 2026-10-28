import { redirect } from "next/navigation";
import { PharmacyOverview } from "@/components/pharmacy/overview";

// The queue's own parameters: a link from before #576, when the Dispense
// queue lived at /pharmacy, still opens the queue.
const QUEUE_PARAMS = ["tab", "page", "cursor", "q"];

export default async function PharmacyOverviewPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const params = await searchParams;
  if (QUEUE_PARAMS.some((key) => params[key] !== undefined)) {
    const query = new URLSearchParams();
    for (const [key, value] of Object.entries(params))
      for (const v of Array.isArray(value) ? value : value === undefined ? [] : [value])
        query.append(key, v);
    redirect(`/pharmacy/dispense?${query}`);
  }
  return <PharmacyOverview />;
}
