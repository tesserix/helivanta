"use client";
import { useState } from "react";
import { Badge, Button } from "@tesserix/web";
import {
  apiFetch,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type Hospital,
  type InterimBill,
  type InterimBillLine,
} from "@helivanta/api";
import { LoadMore, RequestFailure, formatDateTime } from "@helivanta/ui";
import { formatPharmacyMoney } from "../pharmacy/currency-amount-field";

const sourceLabel: Record<string, string> = { pharmacy: "Medicine", bed: "Bed day" };

/**
 * An admission's interim bills (#197): each the account frozen as issued, and,
 * for the billing desk, issuing the next one.
 */
export function InterimBills({
  hospital,
  admissionId,
}: {
  hospital: Hospital;
  admissionId: string;
}) {
  const { can } = usePermissions();
  const [shown, setShown] = useState<number | null>(null);
  const path = `/billing/hospitals/${hospital.id}/admissions/${admissionId}/interim-bills`;
  const key = ["billing", "interim-bills", hospital.id, admissionId];
  const bills = useApiPagedQuery<InterimBill>(key, path);
  const issue = useApiMutation(() => apiFetch<InterimBill>(path, { method: "POST" }), {
    successToast: "Interim bill issued",
    invalidate: [key],
    onSuccess: (bill) => setShown(bill.number),
  });
  return (
    <div className="mt-2 space-y-2 rounded-md border border-border p-3">
      {can("billing.bill.issue") && (
        <Button size="sm" disabled={issue.isPending} onClick={() => issue.mutate()}>
          Issue interim bill
        </Button>
      )}
      {bills.isError ? (
        <RequestFailure message="Could not load the interim bills." retry={bills.retry} />
      ) : bills.items.length === 0 && !bills.isPending ? (
        <p className="text-sm text-muted-foreground">No interim bills yet.</p>
      ) : (
        <ul className="space-y-1 text-sm">
          {bills.items.map((b) => (
            <li key={b.id}>
              <Button
                variant="link"
                size="sm"
                className="h-auto p-0"
                aria-expanded={shown === b.number}
                onClick={() => setShown(shown === b.number ? null : b.number)}
              >
                Interim bill {b.number}
              </Button>
              <span className="ml-2 text-muted-foreground">
                {formatDateTime(b.issued_at)} · {b.line_count}{" "}
                {b.line_count === 1 ? "entry" : "entries"}
              </span>
              {shown === b.number && <BillDetail path={`${path}/${b.number}`} />}
            </li>
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more bills"
        hasMore={bills.hasMore}
        isLoading={bills.isFetchingMore}
        onClick={bills.loadMore}
      />
    </div>
  );
}

function BillDetail({ path }: { path: string }) {
  const bill = useApiQuery<InterimBill>(["billing", "interim-bill", path], path);
  const lines = useApiPagedQuery<InterimBillLine>(
    ["billing", "interim-bill-lines", path],
    `${path}/lines`,
  );
  if (bill.isPending)
    return <p className="mt-2 text-xs text-muted-foreground">Loading the bill…</p>;
  if (bill.isError)
    return <RequestFailure message="Could not load the bill." retry={() => void bill.refetch()} />;
  const b = bill.data;
  return (
    <section
      aria-label={`Interim bill ${b.number}`}
      className="mt-2 space-y-2 border-t border-border pt-2"
    >
      <p className="font-medium">Interim bill {b.number} — not a tax invoice</p>
      <p className="text-xs text-muted-foreground">
        Issued {formatDateTime(b.issued_at)} by {b.issued_by}. Medicine is billed on the pharmacy's
        GST invoice; bed days are before tax.
      </p>
      <p>
        {b.totals.length === 0
          ? "Nothing charged"
          : `Owed ${b.totals.map((t) => formatPharmacyMoney(t.outstanding_minor, t.currency)).join(" + ")}`}
      </p>
      {b.unpriced_bed_days > 0 && (
        // A day without a rate has no amount: said so, never counted as zero.
        <p className="text-destructive">
          {b.unpriced_bed_days} bed {b.unpriced_bed_days === 1 ? "day" : "days"} not priced, not
          included
        </p>
      )}
      {lines.isError ? (
        <RequestFailure message="Could not load the bill's entries." retry={lines.retry} />
      ) : (
        <ul className="divide-y divide-border">
          {lines.items.map((l) => (
            <li key={l.id} className="flex flex-wrap items-center justify-between gap-2 py-1">
              <span>
                {sourceLabel[l.source_module] ?? l.source_module} · {l.source_document}
                {l.kind === "reversal" && " · reversed"}
                {l.new && (
                  <Badge variant="outline" className="ml-2">
                    New
                  </Badge>
                )}
              </span>
              <span>
                {l.kind === "reversal" ? "−" : ""}
                {formatPharmacyMoney(l.amount_minor, l.currency)}
              </span>
            </li>
          ))}
        </ul>
      )}
      <LoadMore
        label="Load more entries"
        hasMore={lines.hasMore}
        isLoading={lines.isFetchingMore}
        onClick={lines.loadMore}
      />
    </section>
  );
}
