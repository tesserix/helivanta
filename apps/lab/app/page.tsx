import { HmsShell } from "@hms/ui";
import { OrderList } from "@/components/order-list";

export default function OrdersPage() {
  return (
    <HmsShell active="/lab">
      <h1 className="mb-6 text-2xl font-semibold">Lab — Orders</h1>
      <OrderList />
    </HmsShell>
  );
}
