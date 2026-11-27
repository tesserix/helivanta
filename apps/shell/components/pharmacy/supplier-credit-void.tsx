"use client";
import "@/lib/csp-zod";
import { useState, useSyncExternalStore } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Input } from "@tesserix/web";
import {
  pharmacySupplierCreditVoidInputSchema,
  pharmacySupplierCreditKey,
  pharmacyKey,
  voidPharmacySupplierCredit,
  readPharmacySupplierCreditVoid,
  recoverPharmacySupplierCreditVoid,
  type PharmacyActorScope,
  type PharmacySupplierCreditContext,
  type PharmacySupplierCreditVoidInput,
} from "@helivanta/api";
import {
  AccountName,
  Field,
  Panel,
  RefreshButton,
  RequestFailure,
  useConfirmLeave,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";
import {
  creditApplicationCommandKey,
  readCreditApplicationReference,
  useCreditApplicationCommand,
} from "./use-credit-application-command";
type Props = {
  scope: PharmacyActorScope;
  credit: PharmacySupplierCreditContext;
  canManage: boolean;
};
const subscribe = () => () => {};
export function SupplierCreditVoid(props: Props) {
  const ready = useSyncExternalStore(
    subscribe,
    () => true,
    () => false,
  );
  if (!ready) return <p role="status">Checking credit void recovery…</p>;
  return (
    <VoidWorkspace
      key={JSON.stringify([
        props.scope.tenantId,
        props.scope.subject,
        props.scope.pharmacyId,
        props.scope.outletId,
        props.credit.id,
      ])}
      {...props}
    />
  );
}
const formSchema = pharmacySupplierCreditVoidInputSchema.pick({ reason: true });
function VoidWorkspace({ scope, credit, canManage }: Props) {
  const queryClient = useQueryClient();
  const key = creditApplicationCommandKey(scope, credit.id, "void");
  const [initial] = useState(() => {
    const saved = readCreditApplicationReference(key);
    return saved.error || saved.reference?.target
      ? {
          reference: null,
          error:
            "The original void recovery reference is unavailable or invalid. Restore it before continuing.",
        }
      : saved;
  });
  const [open, setOpen] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const confirmLeave = useConfirmLeave();
  const form = useZodForm(formSchema, { reason: "" });
  const canWrite =
    canManage &&
    credit.state === "active" &&
    credit.matched_minor === 0 &&
    credit.allocated_minor === 0;
  const command = useCreditApplicationCommand({
    scope,
    storageKey: key,
    initial,
    label: "Supplier credit void",
    canWrite,
    write: (id, input: PharmacySupplierCreditVoidInput) =>
      voidPharmacySupplierCredit(scope, credit.id, id, input),
    recover: (reference, input) =>
      recoverPharmacySupplierCreditVoid(scope, credit.id, reference.id, undefined, input),
    onVerified: () => {
      form.reset({ reason: "" });
      setOpen(false);
      setDismissed(false);
    },
  });
  const history = useQuery({
    queryKey: pharmacySupplierCreditKey(scope, "void-history", credit.id, credit.version),
    queryFn: async ({ signal }) => {
      const result = await readPharmacySupplierCreditVoid(scope, credit.id, signal);
      if (result.version > credit.version)
        throw new Error("The current credit observation must be refreshed.");
      return result;
    },
    enabled: credit.state !== "active",
    gcTime: 0,
    retry: false,
  });
  useLeaveGuard(
    command.reference !== null || (open && form.formState.isDirty),
    "supplier credit void",
    command.busy,
  );
  const shown = open || (command.reference !== null && !dismissed);
  if (command.storageError) return <p role="alert">{command.storageError}</p>;
  return (
    <Panel>
      <h4 className="font-semibold">Credit void</h4>
      <p className="my-3 text-sm text-muted-foreground">
        Void only an unused, unmatched note. Reverse its active return matches and bill applications
        first. Original evidence is retained. Voiding does not change stock, bills, payments or
        cash.
      </p>
      {history.isError ? (
        <RequestFailure
          message="Retained void evidence is unavailable. Previously loaded details are hidden."
          error={history.error}
          retry={history.refetch}
          recheck="Check credit void access"
        />
      ) : (
        credit.state !== "active" &&
        (history.isPending ? (
          <p role="status">Loading retained credit void…</p>
        ) : (
          history.data && (
            <div className="my-3">
              <p>{history.data.reason}</p>
              <p>
                Voided by <AccountName subject={history.data.recorded_by} />
              </p>
            </div>
          )
        ))
      )}
      {command.notice && <p role="status">{command.notice}</p>}
      {!shown ? (
        (canWrite || command.reference !== null) && (
          <Button
            onClick={() => {
              setOpen(true);
              setDismissed(false);
            }}
          >
            {command.reference ? "Resume credit void recovery" : "Void unused credit"}
          </Button>
        )
      ) : (
        <div className="space-y-4">
          <Button
            variant="ghost"
            disabled={command.busy}
            onClick={() =>
              confirmLeave(() => {
                setOpen(false);
                setDismissed(true);
                form.reset({ reason: "" });
              })
            }
          >
            Close credit void
          </Button>
          {command.reference && !(command.missing && !command.hasIntent && canWrite) ? (
            <div className="flex flex-wrap gap-3">
              <Button disabled={command.busy} onClick={command.check}>
                Check original credit void
              </Button>
              {command.missing && command.hasIntent && canWrite && (
                <Button variant="outline" disabled={command.busy} onClick={command.retryExact}>
                  Retry exact credit void
                </Button>
              )}
            </div>
          ) : canWrite && !history.isError ? (
            <form
              noValidate
              className="space-y-4"
              onSubmit={(event) => {
                void form.handleSubmit((value) =>
                  command.submit(
                    pharmacySupplierCreditVoidInputSchema.parse({
                      ...value,
                      expected_version: credit.version,
                    }),
                  ),
                )(event);
              }}
            >
              <Field
                id="supplier-credit-void-reason"
                label="Void reason"
                error={form.formState.errors.reason?.message}
              >
                <Input
                  id="supplier-credit-void-reason"
                  {...form.register("reason")}
                  disabled={command.busy}
                />
              </Field>
              <div className="flex flex-wrap gap-3">
                <Button variant="destructive" type="submit" disabled={command.busy}>
                  {command.reference ? "Retry original credit void" : "Confirm credit void"}
                </Button>
                <RefreshButton
                  error={history.error}
                  refresh={() => queryClient.invalidateQueries({ queryKey: pharmacyKey(scope) })}
                  busy={command.busy}
                >
                  Refresh current credit observations
                </RefreshButton>
              </div>
            </form>
          ) : (
            <p>New void is unavailable for this account, credit state or release.</p>
          )}
        </div>
      )}
    </Panel>
  );
}
