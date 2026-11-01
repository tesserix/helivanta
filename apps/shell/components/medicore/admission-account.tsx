"use client";
import { Button } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  useApiMutation,
  useApiQuery,
  usePermissions,
  type AdmissionAccount,
  type Hospital,
} from "@helivanta/api";
import { formatPharmacyMoney } from "../pharmacy/currency-amount-field";

/**
 * What an admission owes the hospital so far (#197): medicine less returns,
 * and bed days before tax, from billing, under the caller's own billing grant.
 */
export function AdmissionBalance({
  hospital,
  admissionId,
}: {
  hospital: Hospital;
  admissionId: string;
}) {
  const account = useApiQuery<AdmissionAccount>(
    ["billing", "account", hospital.id, admissionId],
    `/billing/hospitals/${hospital.id}/admissions/${admissionId}/account`,
    { staleTime: 30_000 },
  );
  if (account.isPending) return null;
  // No account yet means nothing has been posted; any other failure is shown
  // as one, never as a zero balance.
  if (account.isError)
    return account.error instanceof ApiError && account.error.status === 404 ? (
      <span className="text-xs text-muted-foreground">No charges posted yet</span>
    ) : (
      <span className="text-xs text-destructive">Could not load the account</span>
    );
  const money = (rows: { outstanding_minor: number; currency: string }[]) =>
    rows.map((t) => formatPharmacyMoney(t.outstanding_minor, t.currency)).join(" + ");
  const owed = account.data.totals.filter((t) => t.outstanding_minor !== 0);
  const from = (module: string) =>
    account.data.sources.filter((s) => s.source_module === module && s.outstanding_minor !== 0);
  const { configured, not_priced } = account.data.bed_days;
  const parts = [
    owed.length === 0 ? "Nothing owed yet" : `Owed ${money(owed)}`,
    from("pharmacy").length > 0 && `medicine ${money(from("pharmacy"))}`,
    from("bed").length > 0 && `bed days ${money(from("bed"))} before tax`,
    from("gst").length > 0 && `GST on rooms ${money(from("gst"))}`,
    // Discounts (#200) stand against the charges, so they show as taken off.
    from("discount").length > 0 &&
      `less discounts ${money(from("discount").map((s) => ({ ...s, outstanding_minor: -s.outstanding_minor })))}`,
    // A day without a rate has no amount: said so, never counted as zero.
    !configured
      ? "Bed charging is not set up"
      : not_priced > 0 && `${not_priced} bed ${not_priced === 1 ? "day" : "days"} not priced`,
  ].filter(Boolean);
  return <span className="text-xs text-muted-foreground">{parts.join(" · ")}</span>;
}

/** Whether the caller can read admission accounts here, and a self-grant for administrators. */
export function useBillingAccess(hospital: Hospital) {
  const { can } = usePermissions();
  const access = useApiQuery<{ granted: boolean }>(
    ["billing", "access", hospital.id],
    `/billing/hospitals/${hospital.id}/access`,
    { enabled: can("billing.account.read") },
  );
  return can("billing.account.read") && access.isSuccess;
}

export function BillingAccessSetup({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const me = useApiQuery<{ subject: string }>(["iam", "me", "permissions"], "/iam/me/permissions");
  const access = useApiQuery<{ granted: boolean }>(
    ["billing", "access", hospital.id],
    `/billing/hospitals/${hospital.id}/access`,
    { enabled: can("billing.account.read") },
  );
  const grant = useApiMutation(
    () => {
      if (!me.data?.subject) throw new Error("Could not verify your account. Reload and retry.");
      return apiFetch(
        `/billing/hospitals/${hospital.id}/access/${encodeURIComponent(me.data.subject)}`,
        { method: "PUT" },
      );
    },
    { successToast: "Billing access granted", invalidate: [["billing", "access", hospital.id]] },
  );
  // Only once the check has answered: no button flashes for a reader who has access.
  if (
    !can("billing.access.manage") ||
    access.isSuccess ||
    (can("billing.account.read") && access.isPending)
  )
    return null;
  return (
    <p className="text-sm">
      <Button variant="outline" size="sm" disabled={grant.isPending} onClick={() => grant.mutate()}>
        Grant my billing access at {hospital.name}
      </Button>
    </p>
  );
}
