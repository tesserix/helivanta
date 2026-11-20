"use client";

import { useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@tesserix/web";
import { pharmacyKey, type PharmacyScope, type PharmacyStocktakeScope } from "@helivanta/api";
import { GuardedLink as Link, useConfirmLeave } from "@helivanta/ui";
import { StockRegister } from "./stock-register";
import { LowStockPanel } from "./low-stock-panel";
import { StocktakeList } from "./stocktake-list";
import { StocktakeDetail } from "./stocktake-detail";
import { StocktakeStart } from "./stocktake-start";
import { StocktakeRecoveryPanel } from "./stocktake-recovery-panel";
import { useStocktakeRecovery } from "./stocktake-recovery";

export function StockWorkspace({
  scope,
  timeZone,
  canConfigure,
  canCount,
  subject,
}: {
  scope: PharmacyScope;
  timeZone: string;
  canConfigure: boolean;
  canCount: boolean;
  subject: string;
}) {
  const view = useSearchParams().get("view");
  const low = view === "low";
  const counts = view === "counts";
  const root = `/pharmacy/stock?pharmacy=${encodeURIComponent(scope.pharmacyId)}&outlet=${encodeURIComponent(scope.outletId)}`;
  return (
    <div className="flex flex-col gap-5">
      <nav aria-label="Stock views" className="flex gap-4 text-sm">
        <Link
          href={root}
          aria-current={!low && !counts ? "page" : undefined}
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
        <Link
          href={`${root}&view=counts`}
          aria-current={counts ? "page" : undefined}
          className="underline underline-offset-4"
        >
          Stocktakes
        </Link>
      </nav>
      {counts ? (
        <StocktakesWorkspace
          key={`${scope.tenantId}:${subject}:${scope.pharmacyId}:${scope.outletId}`}
          scope={{ ...scope, subject }}
          timeZone={timeZone}
          canCount={canCount}
          root={`${root}&view=counts`}
        />
      ) : low ? (
        <LowStockPanel scope={scope} canConfigure={canConfigure} />
      ) : (
        <StockRegister scope={scope} timeZone={timeZone} />
      )}
    </div>
  );
}

function StocktakesWorkspace({
  scope,
  timeZone,
  canCount,
  root,
}: {
  scope: PharmacyStocktakeScope;
  timeZone: string;
  canCount: boolean;
  root: string;
}) {
  const router = useRouter();
  const client = useQueryClient();
  const leave = useConfirmLeave();
  const take = useSearchParams().get("count");
  const [starting, setStarting] = useState(false);
  const recovery = useStocktakeRecovery(scope);
  const blocked = recovery.unavailable || recovery.markers.length > 0;
  const recoveryPanel = <StocktakeRecoveryPanel scope={scope} timeZone={timeZone} />;
  if (starting && canCount)
    return (
      <div className="flex flex-col gap-4">
        {recoveryPanel}
        <Button variant="outline" onClick={() => leave(() => setStarting(false))}>
          Back to stocktakes
        </Button>
        <StocktakeStart
          scope={scope}
          onStarted={(count) => {
            setStarting(false);
            void client.invalidateQueries({ queryKey: pharmacyKey(scope) });
            router.push(`${root}&count=${encodeURIComponent(count.id)}`);
          }}
        />
      </div>
    );
  if (take)
    return (
      <div className="flex flex-col gap-4">
        {recoveryPanel}
        <Link href={root} className="text-sm underline underline-offset-4">
          Back to stocktakes
        </Link>
        <StocktakeDetail
          key={take}
          scope={scope}
          takeId={take}
          timeZone={timeZone}
          canConfigure={canCount && !blocked}
        />
      </div>
    );
  return (
    <div className="flex flex-col gap-4">
      {recoveryPanel}
      <StocktakeList
        scope={scope}
        timeZone={timeZone}
        onStart={canCount && !blocked ? () => setStarting(true) : undefined}
      />
    </div>
  );
}
