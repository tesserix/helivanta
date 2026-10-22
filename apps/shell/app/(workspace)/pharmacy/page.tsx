import { Suspense } from "react";
import { DispenseList } from "@/components/pharmacy/dispense-list";

// DispenseList reads its tab and page from the URL (#1295 D5).
export default function DispensesPage() {
  return (
    <Suspense>
      <DispenseList />
    </Suspense>
  );
}
