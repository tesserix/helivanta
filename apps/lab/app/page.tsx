import { HmsShell } from "@helivanta/ui";
import { OrderList } from "@/components/order-list";

export default function OrdersPage() {
  return (
    <HmsShell active="/lab">
      <OrderList />
    </HmsShell>
  );
}
