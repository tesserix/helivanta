"use client";
import { useMemo, useState } from "react";
import { Badge, Button } from "@tesserix/web";
import {
  apiFetch,
  bedReasonSchema,
  dischargeDispositionLabel,
  dischargeOrderSchema,
  newBedId,
  useApiMutation,
  usePermissions,
  type Bed,
  type Hospital,
} from "@helivanta/api";
import { Field, FormSelectField, formatDateTime, useZodForm } from "@helivanta/ui";

const inputClass =
  "w-full rounded-md border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";

/**
 * An occupied bed's discharge order (#96): shown to the ward; ordered and
 * withdrawn by a doctor. The release stays the ward's.
 */
export function DischargeOrder({ hospital, bed }: { hospital: Hospital; bed: Bed }) {
  const { can } = usePermissions();
  // The form is opened for the order the doctor saw. If the board shows
  // another since (a colleague ordered or withdrew), it closes rather than
  // turning into the other command under the doctor's hand.
  const [openFor, setOpenFor] = useState<string | null | undefined>(undefined);
  if (bed.state !== "occupied" || !bed.admission_id) return null;
  const doctor = can("medicore.discharge.order");
  const ordered = bed.discharge_order_id && bed.discharge_disposition;
  const open = openFor !== undefined && openFor === bed.discharge_order_id;
  return (
    <div className="mt-1">
      <div className="flex flex-wrap items-center gap-2">
        {ordered && (
          <Badge variant="outline">
            Discharge ordered · {dischargeDispositionLabel[bed.discharge_disposition!]}
            {bed.discharge_ordered_at && ` · ${formatDateTime(bed.discharge_ordered_at)}`}
          </Badge>
        )}
        {doctor && !open && (
          <Button
            variant="outline"
            size="sm"
            aria-label={`${ordered ? "Withdraw discharge" : "Order discharge"} for bed ${bed.code}`}
            onClick={() => setOpenFor(bed.discharge_order_id)}
          >
            {ordered ? "Withdraw discharge" : "Order discharge"}
          </Button>
        )}
      </div>
      {doctor && open && (
        <DischargeForm
          hospital={hospital}
          admissionId={bed.admission_id}
          orderId={bed.discharge_order_id}
          onDone={() => setOpenFor(undefined)}
        />
      )}
    </div>
  );
}

function DischargeForm({
  hospital,
  admissionId,
  orderId,
  onDone,
}: {
  hospital: Hospital;
  admissionId: string;
  orderId: string | null;
  onDone: () => void;
}) {
  // One command id per open form: a retried submit repeats the same command.
  const commandId = useMemo(() => newBedId(), []);
  const base = `/medicore/hospitals/${hospital.id}/admissions/${admissionId}/discharge-orders`;
  const done = {
    invalidate: [["medicore", "beds", hospital.id]],
    invalidateOnError: true,
    onSuccess: onDone,
  };
  const order = useApiMutation(
    (body: { disposition: string; note?: string }) =>
      apiFetch(`${base}/${commandId}`, { method: "PUT", body: JSON.stringify(body) }),
    { ...done, successToast: "Discharge ordered" },
  );
  const withdraw = useApiMutation(
    (reason: string) =>
      apiFetch(`${base}/${orderId}/withdrawals/${commandId}`, {
        method: "PUT",
        body: JSON.stringify({ reason }),
      }),
    { ...done, successToast: "Discharge withdrawn" },
  );
  const orderForm = useZodForm(dischargeOrderSchema, { disposition: "home", note: "" });
  const withdrawForm = useZodForm(bedReasonSchema, { reason: "" });
  const cancel = (
    <Button type="button" variant="ghost" size="sm" onClick={onDone}>
      Cancel
    </Button>
  );
  if (orderId)
    return (
      <form
        noValidate
        className="mt-3 flex flex-wrap items-end gap-2"
        onSubmit={withdrawForm.handleSubmit(({ reason }) => withdraw.mutate(reason))}
      >
        <Field
          id={`withdraw-${admissionId}`}
          label="Why discharge is withdrawn"
          error={withdrawForm.formState.errors.reason?.message}
        >
          <input
            id={`withdraw-${admissionId}`}
            className={`${inputClass} w-72`}
            {...withdrawForm.register("reason")}
          />
        </Field>
        <Button type="submit" size="sm" disabled={withdraw.isPending}>
          Withdraw discharge
        </Button>
        {cancel}
      </form>
    );
  return (
    <form
      noValidate
      className="mt-3 flex flex-wrap items-end gap-2"
      onSubmit={orderForm.handleSubmit(({ disposition, note }) =>
        order.mutate(note ? { disposition, note } : { disposition }),
      )}
    >
      <FormSelectField
        control={orderForm.control}
        name="disposition"
        id={`disposition-${admissionId}`}
        label="Discharge to"
        options={Object.entries(dischargeDispositionLabel).map(([value, label]) => ({
          value,
          label,
        }))}
      />
      <Field
        id={`discharge-note-${admissionId}`}
        label="Note (optional)"
        error={orderForm.formState.errors.note?.message}
      >
        <input
          id={`discharge-note-${admissionId}`}
          className={`${inputClass} w-72`}
          {...orderForm.register("note")}
        />
      </Field>
      <Button type="submit" size="sm" disabled={order.isPending}>
        Order discharge
      </Button>
      {cancel}
    </form>
  );
}
