"use client";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@tesserix/web";
import { stocktakeKey, type PharmacyStocktakeScope } from "@helivanta/api";
import { useConfirmLeave } from "@helivanta/ui";
import { StocktakeContinuations } from "./stocktake-continuations";
import { StocktakeList } from "./stocktake-list";
import { StocktakeStart } from "./stocktake-start";
import { StocktakeDetail } from "./stocktake-detail";
import { StocktakeRecoveryPanel } from "./stocktake-recovery-panel";
import { useStocktakeRecovery } from "./stocktake-recovery";

type Props = {
  scope: PharmacyStocktakeScope;
  timeZone: string;
  canCount: boolean;
  canReview: boolean;
  released: boolean;
};
export function StocktakeOutletWork({ released, ...props }: Props) {
  if (released) return <WorkingStocktakes {...props} />;
  return (
    <>
      <p className="mb-3 text-sm">
        New stocktake work is not available. Retained status and recovery remain accessible.
      </p>
      <StocktakeContinuations
        scope={props.scope}
        timeZone={props.timeZone}
        canCancel={props.canCount}
      />
    </>
  );
}

function WorkingStocktakes({
  scope,
  timeZone,
  canCount,
  canReview,
}: {
  scope: PharmacyStocktakeScope;
  timeZone: string;
  canCount: boolean;
  canReview: boolean;
}) {
  const client = useQueryClient();
  const leave = useConfirmLeave();
  const recovery = useStocktakeRecovery(scope);
  const blocked = recovery.unavailable || recovery.markers.length > 0;
  const [starting, setStarting] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const refresh = () => void client.invalidateQueries({ queryKey: stocktakeKey(scope) });
  return (
    <div className="flex flex-col gap-4">
      <StocktakeRecoveryPanel scope={scope} timeZone={timeZone} />
      {(starting || selected) && (
        <Button
          variant="outline"
          onClick={() =>
            leave(() => {
              setStarting(false);
              setSelected(null);
            })
          }
        >
          Back to stocktakes
        </Button>
      )}
      {starting && canCount ? (
        <StocktakeStart
          scope={scope}
          onStarted={(take) => {
            setStarting(false);
            setSelected(take.id);
            refresh();
          }}
        />
      ) : selected ? (
        <StocktakeDetail
          key={selected}
          scope={scope}
          takeId={selected}
          timeZone={timeZone}
          canConfigure={!blocked && (canCount || canReview)}
          canCount={canCount}
          canReview={canReview}
        />
      ) : (
        <StocktakeList
          scope={scope}
          timeZone={timeZone}
          retainedOnly={false}
          onStart={canCount && !blocked ? () => setStarting(true) : undefined}
          onOpen={(take) => setSelected(take.id)}
        />
      )}
    </div>
  );
}
