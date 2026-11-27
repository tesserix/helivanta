"use client";

import "@/lib/csp-zod";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, Input } from "@tesserix/web";
import {
  pharmacySupplierCreditAllocationReversalInputSchema,
  pharmacySupplierCreditKey,
  readPharmacySupplierCreditAllocationContext,
  readPharmacySupplierCreditAllocationReversal,
  reversePharmacySupplierCreditAllocation,
  recoverPharmacySupplierCreditAllocationReversal,
  type PharmacyActorScope,
  type PharmacySupplierCreditAllocation,
  type PharmacySupplierCreditAllocationReversalInput,
} from "@helivanta/api";
import {
  AccountName,
  Field,
  Panel,
  RequestFailure,
  RefreshButton,
  useConfirmLeave,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";
import { formatPharmacyMoney } from "./currency-amount-field";
import {
  creditApplicationCommandKey,
  readCreditApplicationReference,
  useCreditApplicationCommand,
} from "./use-credit-application-command";
type Intent = {
  input: PharmacySupplierCreditAllocationReversalInput;
  original: PharmacySupplierCreditAllocation;
};
const formSchema = pharmacySupplierCreditAllocationReversalInputSchema.pick({
  occurred_on: true,
  reason: true,
});
export function SupplierCreditApplicationReview({
  scope,
  credit,
  requested,
  canManage,
  onClose,
}: {
  scope: PharmacyActorScope;
  credit: string;
  requested: { id: string; sequence: number } | null;
  canManage: boolean;
  onClose: () => void;
}) {
  const storageKey = creditApplicationCommandKey(scope, credit, "application-reversal");
  const [initial] = useState(() => {
    const saved = readCreditApplicationReference(storageKey);
    return saved.reference && !saved.reference.target
      ? {
          reference: null,
          error:
            "The original application target is missing. Restore its recovery reference before continuing.",
        }
      : saved;
  });
  const target = initial.reference?.target ?? requested?.id ?? null;
  const [dismissed, setDismissed] = useState(false);
  const shown = target !== null && !dismissed;
  const confirmLeave = useConfirmLeave();
  const form = useZodForm(formSchema, { occurred_on: "", reason: "" });
  const context = useQuery({
    queryKey: pharmacySupplierCreditKey(scope, "application-review", credit, target),
    queryFn: ({ signal }) =>
      readPharmacySupplierCreditAllocationContext(scope, credit, target!, signal),
    enabled: shown && !initial.error,
    gcTime: 0,
    retry: false,
  });
  const current = !context.isError ? context.data : undefined;
  const canWrite =
    canManage && !!current && current.credit.state === "active" && !current.allocation.reversal_id;
  const command = useCreditApplicationCommand({
    scope,
    storageKey,
    initial,
    label: "Bill application reversal",
    canWrite,
    write: (id, intent: Intent) =>
      reversePharmacySupplierCreditAllocation(scope, credit, intent.original, id, intent.input),
    recover: (reference, intent) => {
      if (!current || !reference.target)
        throw new Error("Current application evidence is unavailable.");
      return recoverPharmacySupplierCreditAllocationReversal(
        scope,
        credit,
        reference.target,
        reference.id,
        undefined,
        { original: current.allocation, input: intent?.input },
      );
    },
    onVerified: () => form.reset({ occurred_on: "", reason: "" }),
  });
  const reversal = useQuery({
    queryKey: pharmacySupplierCreditKey(
      scope,
      "application-reversal-history",
      credit,
      target,
      current?.allocation.reversal_id,
    ),
    queryFn: ({ signal }) =>
      readPharmacySupplierCreditAllocationReversal(
        scope,
        credit,
        target!,
        signal,
        current!.allocation,
      ),
    enabled: shown && !!current?.allocation.reversal_id,
    gcTime: 0,
    retry: false,
  });
  useLeaveGuard(
    command.reference !== null || (shown && form.formState.isDirty),
    "bill application reversal",
    command.busy,
  );
  function submit(value: ReturnType<typeof formSchema.parse>) {
    if (!canWrite || !current || context.isFetching || reversal.isError) return;
    if (
      value.occurred_on < current.allocation.occurred_on ||
      value.occurred_on > current.credit.local_date
    ) {
      form.setError("occurred_on", {
        message:
          "Use a date on or after the original application and no later than the outlet date.",
      });
      return;
    }
    const input = pharmacySupplierCreditAllocationReversalInputSchema.parse({
      ...value,
      expected_version: current.credit.version,
      expected_available_minor: current.credit.available_minor,
      expected_payable_version: current.payable.version,
      expected_outstanding_minor: current.payable.outstanding_minor,
    });
    command.submit({ input, original: current.allocation }, current.allocation.id);
  }
  if (command.storageError) return <p role="alert">{command.storageError}</p>;
  if (!shown)
    return (
      <>
        {command.notice && <p role="status">{command.notice}</p>}
        {command.reference && (
          <Button onClick={() => setDismissed(false)}>
            Resume bill application reversal recovery
          </Button>
        )}
      </>
    );
  return (
    <section aria-label="Bill application review">
      <Panel>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h4 className="font-semibold">Review bill application</h4>
          <Button
            variant="ghost"
            disabled={command.busy}
            onClick={() =>
              confirmLeave(() => {
                setDismissed(true);
                form.reset({ occurred_on: "", reason: "" });
                if (!command.reference) onClose();
              })
            }
          >
            Close bill application review
          </Button>
        </div>
        {context.isError || reversal.isError ? (
          <RequestFailure
            message="Bill application evidence is unavailable. Previously loaded financial details are hidden."
            error={context.error ?? reversal.error}
            retry={() => {
              void context.refetch();
              if (reversal.isError) void reversal.refetch();
            }}
          />
        ) : !current ? (
          <p role="status">Loading current credit and bill observations…</p>
        ) : (
          <>
            <h5 className="mt-4 font-semibold">
              {current.payable.document_reference} · {current.credit.document_reference}
            </h5>
            <p>
              Original applied amount:{" "}
              {formatPharmacyMoney(current.allocation.amount_minor, current.credit.currency)}
            </p>
            <p>
              Current unused credit:{" "}
              {formatPharmacyMoney(current.credit.available_minor, current.credit.currency)}
            </p>
            <p>
              Current bill outstanding:{" "}
              {formatPharmacyMoney(current.payable.outstanding_minor, current.payable.currency)}
            </p>
            <p>
              Payments retained on bill:{" "}
              {formatPharmacyMoney(current.payable.paid_minor, current.payable.currency)}
            </p>
            <p className="my-3 text-sm">
              {current.allocation.reason} · Applied by{" "}
              <AccountName subject={current.allocation.recorded_by} />
            </p>
            <p className="my-3 text-sm text-muted-foreground">
              Full reversal restores only the original applied amount to unused credit and bill
              liability. Recorded payments, physical stock and cash are retained.
            </p>
            {command.notice && <p role="status">{command.notice}</p>}
            {command.reference && !(command.missing && !command.hasIntent && canWrite) ? (
              <div className="my-4 flex flex-wrap gap-3">
                <Button disabled={command.busy || context.isFetching} onClick={command.check}>
                  Check original bill application reversal
                </Button>
                {command.missing && command.hasIntent && canWrite && (
                  <Button variant="outline" disabled={command.busy} onClick={command.retryExact}>
                    Retry exact bill application reversal
                  </Button>
                )}
              </div>
            ) : current.allocation.reversal_id ? (
              <>
                <p className="font-medium">This bill application has already been reversed.</p>
                {reversal.isPending ? (
                  <p role="status">Loading retained application reversal…</p>
                ) : (
                  reversal.data && (
                    <div className="my-3">
                      <p>{reversal.data.reason}</p>
                      <p>
                        Reversed by <AccountName subject={reversal.data.recorded_by} />
                      </p>
                    </div>
                  )
                )}
              </>
            ) : canWrite ? (
              <form
                noValidate
                className="my-4 space-y-4"
                onSubmit={(event) => {
                  void form.handleSubmit(submit)(event);
                }}
              >
                <Field
                  id="credit-application-reversal-date"
                  label="Application reversal date"
                  error={form.formState.errors.occurred_on?.message}
                >
                  <Input
                    id="credit-application-reversal-date"
                    type="date"
                    min={current.allocation.occurred_on}
                    max={current.credit.local_date}
                    {...form.register("occurred_on")}
                    disabled={command.busy}
                  />
                </Field>
                <p className="text-xs text-muted-foreground">
                  Outlet date: {current.credit.local_date} ({current.credit.timezone})
                </p>
                <Field
                  id="credit-application-reversal-reason"
                  label="Application reversal reason"
                  error={form.formState.errors.reason?.message}
                >
                  <Input
                    id="credit-application-reversal-reason"
                    {...form.register("reason")}
                    disabled={command.busy}
                  />
                </Field>
                <div className="flex flex-wrap gap-3">
                  <Button
                    type="submit"
                    variant="destructive"
                    disabled={command.busy || context.isFetching}
                  >
                    {command.reference
                      ? "Retry original bill application reversal"
                      : "Reverse full bill application"}
                  </Button>
                  <RefreshButton
                    refresh={context.refetch}
                    error={context.error}
                    busy={command.busy || context.isFetching}
                  >
                    Refresh current application observations
                  </RefreshButton>
                </div>
              </form>
            ) : (
              <p>
                New bill application reversal is unavailable for this account, credit state or
                release.
              </p>
            )}
          </>
        )}
      </Panel>
    </section>
  );
}
