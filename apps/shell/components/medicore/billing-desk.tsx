"use client";
import { useState } from "react";
import { Badge, Button } from "@tesserix/web";
import { useApiPagedQuery, type DeskAccount, type DeskView, type Hospital } from "@helivanta/api";
import { Field, LoadMore, Panel, RequestFailure, SelectControl } from "@helivanta/ui";
import { formatPharmacyMoney } from "../pharmacy/currency-amount-field";
import { BillingAccessSetup, useBillingAccess } from "./admission-account";
import { AdmissionBills } from "./admission-bills";
import { CashDesk } from "./cash-desk";
import { DailyCollectionReport } from "./daily-collection";
import { GstSetup } from "./gst";
import { PatientName } from "./opd-encounters";

const views: { value: DeskView; label: string }[] = [
  { value: "to_collect", label: "To collect" },
  { value: "to_refund", label: "To refund" },
  { value: "discharge_ordered", label: "Discharge ordered" },
  { value: "open", label: "In the ward" },
  { value: "released", label: "Released" },
  { value: "", label: "All accounts" },
];

/** The billing desk page: one hospital at a time, as the ward board. */
export function BillingDeskPage() {
  const [hospitalId, setHospitalId] = useState("");
  const hospitals = useApiPagedQuery<Hospital>(["workforce", "hospitals"], "/workforce/hospitals");
  const hospital = hospitals.items.find((item) => item.id === hospitalId) ?? hospitals.items[0];
  if (hospitals.isError)
    return <RequestFailure message="Could not load your hospitals." retry={hospitals.retry} />;
  if (hospitals.isPending) return <p role="status">Loading hospitals…</p>;
  if (!hospital)
    return (
      <Panel>
        <h3 className="font-semibold">No hospital access yet</h3>
        <p className="mt-1 text-sm text-muted-foreground">
          Ask your administrator to configure your hospital directory.
        </p>
      </Panel>
    );
  return (
    <div className="space-y-6">
      {hospitals.items.length > 1 && (
        <Field id="desk-hospital" label="Hospital">
          <SelectControl
            id="desk-hospital"
            className="w-72"
            value={hospital.id}
            onValueChange={setHospitalId}
            options={hospitals.items.map((item) => ({ value: item.id, label: item.name }))}
          />
        </Field>
      )}
      <BillingDesk key={hospital.id} hospital={hospital} />
      <LoadMore
        label="Load more hospitals"
        hasMore={hospitals.hasMore}
        isLoading={hospitals.isFetchingMore}
        onClick={hospitals.loadMore}
      />
    </div>
  );
}

/**
 * The billing desk (#197–#199): a hospital's admission accounts by what they
 * need, each opening its bills, payments, deposits and refunds, including
 * after the patient has left the ward board.
 */
export function BillingDesk({ hospital }: { hospital: Hospital }) {
  const billing = useBillingAccess(hospital);
  const [view, setView] = useState<DeskView>("to_collect");
  if (!billing)
    return (
      <div className="space-y-4">
        <BillingAccessSetup hospital={hospital} />
        <Panel>
          <h3 className="font-semibold">No billing access at {hospital.name}</h3>
          <p className="mt-1 text-sm text-muted-foreground">
            Ask your administrator to grant you billing access at this hospital.
          </p>
        </Panel>
      </div>
    );
  return (
    <div className="space-y-6">
      <CashDesk hospital={hospital} />
      <DailyCollectionReport hospital={hospital} />
      <GstSetup hospital={hospital} />
      <Panel>
        <div className="flex flex-wrap items-end justify-between gap-3">
          <h3 className="font-semibold">Admission accounts</h3>
          <Field id="desk-view" label="Show">
            <SelectControl
              id="desk-view"
              className="w-56"
              value={view || "all"}
              onValueChange={(v) => setView((v === "all" ? "" : v) as DeskView)}
              options={views.map((v) => ({ value: v.value || "all", label: v.label }))}
            />
          </Field>
        </div>
        <Accounts key={view} hospital={hospital} view={view} />
      </Panel>
    </div>
  );
}

function Accounts({ hospital, view }: { hospital: Hospital; view: DeskView }) {
  const accounts = useApiPagedQuery<DeskAccount>(
    ["billing", "desk", hospital.id, view],
    `/billing/hospitals/${hospital.id}/accounts${view ? `?view=${view}` : ""}`,
  );
  if (accounts.isError)
    return <RequestFailure message="Could not load the accounts." retry={accounts.retry} />;
  if (accounts.items.length === 0 && !accounts.isPending)
    return <p className="mt-3 text-sm text-muted-foreground">No accounts here.</p>;
  return (
    <>
      <ul className="mt-3 divide-y divide-border">
        {accounts.items.map((a) => (
          <Account key={a.admission_id} hospital={hospital} account={a} />
        ))}
      </ul>
      <LoadMore
        label="Load more accounts"
        hasMore={accounts.hasMore}
        isLoading={accounts.isFetchingMore}
        onClick={accounts.loadMore}
      />
    </>
  );
}

function Account({ hospital, account }: { hospital: Hospital; account: DeskAccount }) {
  const [open, setOpen] = useState(false);
  const money = (minor: number) => formatPharmacyMoney(minor, "INR");
  const state =
    account.status === "closed"
      ? "Released"
      : account.discharge_ordered
        ? "Discharge ordered"
        : "In the ward";
  return (
    <li className="py-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="font-medium" id={`desk-patient-${account.admission_id}`}>
            <PatientName hospital={hospital} id={account.patient_id} />
          </p>
          <p className="text-sm text-muted-foreground">
            <Badge variant="outline">{state}</Badge>{" "}
            {account.balance_minor < 0
              ? `in credit ${money(-account.balance_minor)}`
              : `balance due ${money(account.balance_minor)}`}
            {account.deposit_held_minor > 0 &&
              ` · deposits held ${money(account.deposit_held_minor)}`}
            {account.refundable_minor > 0 && ` · to refund ${money(account.refundable_minor)}`}
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          aria-expanded={open}
          aria-describedby={`desk-patient-${account.admission_id}`}
          onClick={() => setOpen(!open)}
        >
          {open ? "Close" : "Open"}
        </Button>
      </div>
      {open && <AdmissionBills hospital={hospital} admissionId={account.admission_id} />}
    </li>
  );
}
