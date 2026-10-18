"use client";

import { AlertTriangle, CalendarClock } from "lucide-react";
import { supplyLicenseRenewalSchema, type SupplyLicense } from "@helivanta/api";

export function LicenceRenewalNotice({ renewal }: { renewal: SupplyLicense["renewal"] }) {
  const parsed = supplyLicenseRenewalSchema.safeParse(renewal);
  if (!parsed.success)
    return (
      <p className="mt-3 text-sm text-muted-foreground">
        Renewal date notice unavailable. Refresh the licences to check again.
      </p>
    );
  const value = parsed.data;
  const labels = {
    expired: `Expired ${Math.abs(value.days_remaining)} day${value.days_remaining === -1 ? "" : "s"} ago · renew before review`,
    expires_today: "Expires today · renew now",
    not_yet_valid: "Not yet valid · check the issue date",
    renewal_due_30: `Renew within ${value.days_remaining} day${value.days_remaining === 1 ? "" : "s"}`,
    renewal_due_90: `Plan renewal · ${value.days_remaining} days remaining`,
    renewal_due_180: `Upcoming renewal · ${value.days_remaining} days remaining`,
    current: `Expiry in ${value.days_remaining} days`,
  };
  const urgent = ["expired", "expires_today", "renewal_due_30", "not_yet_valid"].includes(
    value.date_status,
  );
  const Icon = urgent ? AlertTriangle : CalendarClock;
  return (
    <div
      className="mt-3 flex max-w-xl items-start gap-2 rounded-xl border bg-muted/20 p-3 text-sm"
      aria-label="Licence renewal date notice"
    >
      <Icon className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
      <div className="min-w-0 space-y-1">
        <p className={urgent ? "font-semibold text-destructive" : "font-medium"}>
          {labels[value.date_status]}
        </p>
        {value.premise_status === "inactive" && (
          <p className="font-medium text-destructive">
            Premise inactive · review operating details
          </p>
        )}
        <p className="text-xs text-muted-foreground">
          As of {value.local_date} · {value.time_zone}
        </p>
        <p className="text-xs text-muted-foreground">
          Date notice only. Licence verification and trading approval are separate.
        </p>
      </div>
    </div>
  );
}
