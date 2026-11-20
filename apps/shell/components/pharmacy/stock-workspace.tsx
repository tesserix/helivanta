"use client";

import { useSearchParams } from "next/navigation";
import type { PharmacyScope } from "@helivanta/api";
import { GuardedLink as Link } from "@helivanta/ui";
import { StockRegister } from "./stock-register";
import { LowStockPanel } from "./low-stock-panel";

export function StockWorkspace({
  scope,
  timeZone,
  canConfigure,
}: {
  scope: PharmacyScope;
  timeZone: string;
  canConfigure: boolean;
}) {
  const low = useSearchParams().get("view") === "low";
  const root = `/pharmacy/stock?pharmacy=${encodeURIComponent(scope.pharmacyId)}&outlet=${encodeURIComponent(scope.outletId)}`;
  return (
    <div className="flex flex-col gap-5">
      <nav aria-label="Stock views" className="flex gap-4 text-sm">
        <Link
          href={root}
          aria-current={!low ? "page" : undefined}
          className="underline underline-offset-4"
        >
          Stock register
        </Link>
        <Link
          href={`${root}&view=low`}
          aria-current={low ? "page" : undefined}
          className="underline underline-offset-4"
        >
          Low stock
        </Link>
      </nav>
      {low ? (
        <LowStockPanel scope={scope} canConfigure={canConfigure} />
      ) : (
        <StockRegister scope={scope} timeZone={timeZone} />
      )}
    </div>
  );
}
