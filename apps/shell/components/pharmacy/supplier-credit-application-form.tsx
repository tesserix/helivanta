"use client";

import "@/lib/csp-zod";
import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button, Input } from "@tesserix/web";
import {
  allocatePharmacySupplierCredit,
  recoverPharmacySupplierCreditAllocation,
  listPharmacySupplierCreditPayableChoices,
  pharmacySupplierCreditAllocationInputSchema,
  pharmacySupplierCreditKey,
  pharmacyKey,
  usePagedQuery,
  type PharmacyActorScope,
  type PharmacySupplierCreditContext,
} from "@helivanta/api";
import {
  Controller,
  Field,
  FormSelectField,
  LoadMore,
  RequestFailure,
  RefreshButton,
  useConfirmLeave,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";
import { CurrencyAmountField, formatPharmacyMoney } from "./currency-amount-field";
import {
  creditApplicationCommandKey,
  readCreditApplicationReference,
  useCreditApplicationCommand,
} from "./use-credit-application-command";
const formSchema = pharmacySupplierCreditAllocationInputSchema.pick({
  payable_id: true,
  amount_minor: true,
  occurred_on: true,
  reason: true,
});
export function SupplierCreditApplicationForm({
  scope,
  credit,
  canManage,
}: {
  scope: PharmacyActorScope;
  credit: PharmacySupplierCreditContext;
  canManage: boolean;
}) {
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const storageKey = creditApplicationCommandKey(scope, credit.id, "application");
  const [initial] = useState(() => readCreditApplicationReference(storageKey));
  const defaults = {
    payable_id: "",
    amount_minor: undefined,
    occurred_on: credit.local_date,
    reason: "",
  };
  const form = useZodForm(formSchema, defaults);
  const confirmLeave = useConfirmLeave();
  const canWrite = canManage && credit.state === "active" && credit.available_minor > 0;
  const command = useCreditApplicationCommand({
    scope,
    storageKey,
    initial,
    label: "Bill application",
    canWrite,
    write: (id, input: ReturnType<typeof pharmacySupplierCreditAllocationInputSchema.parse>) =>
      allocatePharmacySupplierCredit(scope, credit.id, id, input),
    recover: (reference, input) =>
      recoverPharmacySupplierCreditAllocation(scope, credit.id, reference.id, undefined, input),
    onVerified: () => {
      form.reset(defaults);
      setOpen(false);
      setDismissed(false);
    },
  });
  const shown = open || (command.reference !== null && !dismissed);
  const visible =
    shown && canWrite && (command.reference === null || (command.missing && !command.hasIntent));
  const bills = usePagedQuery(
    pharmacySupplierCreditKey(scope, "application-sources", credit.id),
    (cursor, signal) => listPharmacySupplierCreditPayableChoices(scope, credit.id, cursor, signal),
    { enabled: visible, gcTime: 0, retry: false },
  );
  const selectedId = form.watch("payable_id");
  const selected = bills.items.find((row) => row.id === selectedId);
  const invalidSource = bills.items.some((row) => row.currency !== credit.currency);
  useLeaveGuard(
    command.reference !== null || (open && form.formState.isDirty),
    "supplier credit application",
    command.busy,
  );
  function submit(value: ReturnType<typeof formSchema.parse>) {
    if (!canWrite || bills.isError || bills.isRefreshing || invalidSource || !selected) return;
    if (value.amount_minor > Math.min(credit.available_minor, selected.outstanding_minor)) {
      form.setError("amount_minor", {
        message: "The amount must fit both unused credit and outstanding liability.",
      });
      return;
    }
    if (
      value.occurred_on < credit.issued_on ||
      value.occurred_on < selected.issued_on ||
      value.occurred_on > credit.local_date
    ) {
      form.setError("occurred_on", {
        message: "Use a date on or after both documents and no later than the outlet date.",
      });
      return;
    }
    command.submit(
      pharmacySupplierCreditAllocationInputSchema.parse({
        ...value,
        expected_version: credit.version,
        expected_available_minor: credit.available_minor,
        expected_payable_version: selected.version,
        expected_outstanding_minor: selected.outstanding_minor,
      }),
    );
  }
  if (command.storageError) return <p role="alert">{command.storageError}</p>;
  return (
    <div className="my-4 space-y-3">
      {command.notice && <p role="status">{command.notice}</p>}
      {!shown ? (
        (canWrite || command.reference !== null) && (
          <Button
            onClick={() => {
              setDismissed(false);
              setOpen(true);
            }}
          >
            {command.reference ? "Resume bill application recovery" : "Apply credit to bill"}
          </Button>
        )
      ) : (
        <>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h4 className="font-semibold">
              {command.reference ? "Bill application recovery" : "Apply unused supplier credit"}
            </h4>
            <Button
              variant="ghost"
              disabled={command.busy}
              onClick={() =>
                confirmLeave(() => {
                  setOpen(false);
                  setDismissed(true);
                  form.reset(defaults);
                })
              }
            >
              Close application form
            </Button>
          </div>
          {!visible ? (
            <>
              <Button disabled={command.busy} onClick={command.check}>
                Check original bill application
              </Button>
              {command.missing && command.hasIntent && canWrite && (
                <Button variant="outline" disabled={command.busy} onClick={command.retryExact}>
                  Retry exact bill application
                </Button>
              )}
            </>
          ) : bills.isError ? (
            <RequestFailure
              message="Eligible bill choices are unavailable. Previously loaded bills are hidden."
              error={bills.error}
              retry={bills.retry}
            />
          ) : invalidSource ? (
            <p role="alert">
              Eligible bill evidence could not be verified. No application can be submitted.
            </p>
          ) : bills.isPending ? (
            <p role="status">Loading eligible bills…</p>
          ) : (
            <form
              noValidate
              className="space-y-4"
              onSubmit={(event) => {
                void form.handleSubmit(submit)(event);
              }}
            >
              <FormSelectField
                control={form.control}
                name="payable_id"
                id="credit-application-bill"
                label="Eligible supplier bill"
                disabled={command.busy}
                options={bills.items.map((row) => ({
                  value: row.id,
                  label: `${row.document_reference} · ${row.payee_name} · ${formatPharmacyMoney(row.outstanding_minor, row.currency)} outstanding`,
                }))}
                onValueChange={(value) => {
                  form.reset(defaults);
                  form.setValue("payable_id", value, { shouldDirty: true });
                }}
              />
              <LoadMore
                label="Load more eligible bills"
                hasMore={bills.hasMore}
                isLoading={bills.isFetchingMore}
                onClick={bills.loadMore}
              />
              <RefreshButton
                refresh={() => queryClient.invalidateQueries({ queryKey: pharmacyKey(scope) })}
                error={bills.error}
                busy={command.busy || bills.isRefreshing}
              >
                Refresh credit and bill observations
              </RefreshButton>
              {!bills.items.length && (
                <p>No eligible outstanding inventory bills for this supplier.</p>
              )}
              {selected && (
                <>
                  <p className="text-sm">
                    Unused credit: {formatPharmacyMoney(credit.available_minor, credit.currency)} ·
                    Outstanding bill:{" "}
                    {formatPharmacyMoney(selected.outstanding_minor, credit.currency)}
                  </p>
                  <Controller
                    control={form.control}
                    name="amount_minor"
                    render={({ field, fieldState }) => (
                      <CurrencyAmountField
                        key={selected.id}
                        id="credit-application-amount"
                        label="Amount to apply"
                        currency={credit.currency}
                        initialMinor={Number.isSafeInteger(field.value) ? field.value : null}
                        onChange={field.onChange}
                        inputRef={field.ref}
                        onBlur={field.onBlur}
                        error={fieldState.error?.message}
                        disabled={command.busy}
                      />
                    )}
                  />
                  <Field
                    id="credit-application-date"
                    label="Application date"
                    error={form.formState.errors.occurred_on?.message}
                  >
                    <Input
                      id="credit-application-date"
                      type="date"
                      min={
                        credit.issued_on > selected.issued_on
                          ? credit.issued_on
                          : selected.issued_on
                      }
                      max={credit.local_date}
                      {...form.register("occurred_on")}
                      disabled={command.busy}
                    />
                  </Field>
                  <p className="text-xs text-muted-foreground">
                    Outlet date: {credit.local_date} ({credit.timezone})
                  </p>
                  <Field
                    id="credit-application-reason"
                    label="Application reason"
                    error={form.formState.errors.reason?.message}
                  >
                    <Input
                      id="credit-application-reason"
                      {...form.register("reason")}
                      maxLength={500}
                      disabled={command.busy}
                    />
                  </Field>
                  <Button type="submit" disabled={command.busy || bills.isRefreshing || !canWrite}>
                    {command.reference
                      ? "Retry original bill application"
                      : "Save bill application"}
                  </Button>
                </>
              )}
            </form>
          )}
        </>
      )}
    </div>
  );
}
