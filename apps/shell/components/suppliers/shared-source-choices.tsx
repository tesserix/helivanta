"use client";
import { Button } from "@tesserix/web";
import { Panel } from "@helivanta/ui";
import type { PartnerShipmentSourceChoice } from "@helivanta/api";
type Props = {
  choices: PartnerShipmentSourceChoice[];
  onSelect: (choice: PartnerShipmentSourceChoice) => void;
};
export function SharedSourceChoices({ choices, onSelect }: Props) {
  return (
    <ul aria-label="Current shared supplier sources" className="grid gap-4 md:grid-cols-2">
      {choices.map((choice) => (
        <li key={choice.source.grant_id}>
          <Panel className="h-full space-y-3">
            <h3 className="break-words text-base font-semibold">
              {choice.source.source_organization_name}
            </h3>
            <p className="break-words text-sm text-muted-foreground">
              {choice.source.source_location_name}
            </p>
            <div className="flex flex-wrap gap-2 text-xs">
              <span>
                {choice.source.scope.source_product === "pharmacy" ? "Pharmacy" : "Warehouse"}
              </span>
              <span>·</span>
              <span>{choice.recipientKind === "group" ? "Team access" : "Direct access"}</span>
            </div>
            <Button
              variant="outline"
              aria-label={`View shipments from ${choice.source.source_organization_name} — ${choice.source.source_location_name}`}
              onClick={() => onSelect(choice)}
            >
              View shipments
            </Button>
          </Panel>
        </li>
      ))}
    </ul>
  );
}
