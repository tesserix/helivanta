"use client";

import "@/lib/csp-zod";

import { useRef, useState, useSyncExternalStore } from "react";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  newPharmacyId,
  pharmacySupplierCreditDocumentInputSchema,
  pharmacySupplierCreditKey,
  listPharmacySupplierCreditInitialSources,
  recordPharmacySupplierCredit,
  recoverPharmacySupplierCredit,
  useApiMutation,
  usePagedQuery,
  type PharmacyActorScope,
  type PharmacySupplierCreditInput,
} from "@helivanta/api";
import {
  Field,
  LoadMore,
  Panel,
  RequestFailure,
  Controller,
  FormSelectField,
  useZodForm,
  useConfirmLeave,
  useLeaveGuard,
} from "@helivanta/ui";
import { CurrencyAmountField, formatPharmacyMoney } from "./currency-amount-field";

type Props = { scope: PharmacyActorScope; canRecord: boolean; onRecorded?: (id: string) => void };
function recoveryKey(scope: PharmacyActorScope) {
  return `helivanta:supplier-credit-document:v1:${JSON.stringify([scope.tenantId, scope.subject, scope.pharmacyId, scope.outletId])}`;
}
const subscribeHydration = () => () => {};
const clientHydrated = () => true;
const serverHydrated = () => false;
export function SupplierCreditRecord(props: Props) {
  const ready = useSyncExternalStore(subscribeHydration, clientHydrated, serverHydrated);
  if (!ready) return <p role="status">Checking supplier credit recovery…</p>;
  return <RecordController key={recoveryKey(props.scope)} {...props} />;
}
function readRecovery(key: string) {
  try {
    const saved = sessionStorage.getItem(key);
    if (
      saved !== null &&
      !pharmacySupplierCreditDocumentInputSchema.shape.return_id.safeParse(saved).success
    )
      return {
        saved: null,
        error:
          "The saved recovery reference is invalid. Resolve it before recording another credit.",
      };
    return { saved, error: "" };
  } catch {
    return {
      saved: null,
      error:
        "This browser cannot keep a recovery reference. Enable session storage before recording credit.",
    };
  }
}
function RecordController({ scope, canRecord, onRecorded }: Props) {
  const key = recoveryKey(scope);
  const [recovery, setRecovery] = useState(() => readRecovery(key));
  const { saved, error } = recovery;
  const [open, setOpen] = useState(saved !== null);
  const [notice, setNotice] = useState("");
  function restore() {
    const next = readRecovery(key);
    setRecovery(next);
    return next.saved !== null;
  }
  return (
    <div className="space-y-3">
      {notice && <p role="status">{notice}</p>}
      {error ? (
        <Panel>
          <p role="alert">{error}</p>
          <Button variant="outline" onClick={() => setOpen(restore())}>
            Check recovery status
          </Button>
        </Panel>
      ) : open ? (
        <CreditForm
          scope={scope}
          canRecord={canRecord}
          saved={saved}
          storageKey={key}
          close={() => setOpen(false)}
          completed={(id, reference) => {
            setOpen(false);
            setRecovery({ saved: null, error: "" });
            setNotice(
              `Supplier credit ${reference} verified. Return matching and bill applications are separate steps.`,
            );
            onRecorded?.(id);
          }}
        />
      ) : (
        canRecord && (
          <Button
            onClick={() => {
              restore();
              setOpen(true);
            }}
          >
            Record supplier credit
          </Button>
        )
      )}
    </div>
  );
}
function CreditForm({
  scope,
  canRecord,
  saved,
  storageKey,
  close,
  completed,
}: {
  scope: PharmacyActorScope;
  canRecord: boolean;
  saved: string | null;
  storageKey: string;
  close: () => void;
  completed: (id: string, reference: string) => void;
}) {
  const confirmLeave = useConfirmLeave();
  const [command, setCommand] = useState<string | null>(saved);
  const [attempt, setAttempt] = useState<{ id: string; input: PharmacySupplierCreditInput } | null>(
    null,
  );
  const [missing, setMissing] = useState(false),
    [notice, setNotice] = useState("");
  const defaults = {
    return_id: "",
    document_reference: "",
    issued_on: "",
    reason: "",
    amount_minor: undefined,
    currency: "INR" as const,
    correction_of: null,
  };
  const form = useZodForm(pharmacySupplierCreditDocumentInputSchema, defaults);
  const sourceId = form.watch("return_id");
  const working = useRef(false),
    uncertain = useRef(saved !== null);
  const visible = canRecord && (command === null || (missing && attempt === null));
  const sources = usePagedQuery(
    pharmacySupplierCreditKey(scope, "initial-sources"),
    (cursor, signal) => listPharmacySupplierCreditInitialSources(scope, cursor, signal),
    { enabled: visible, gcTime: 0, retry: false },
  );
  const source = !sources.isError ? sources.items.find((row) => row.id === sourceId) : undefined;
  function finish(row: { id: string; document_reference: string }) {
    working.current = false;
    try {
      sessionStorage.removeItem(storageKey);
    } catch {
      /* A later open recovers the verified original again. */
    }
    completed(row.id, row.document_reference);
  }
  const save = useApiMutation(
    ({ id, input }: { id: string; input: PharmacySupplierCreditInput }) =>
      recordPharmacySupplierCredit(scope, id, input),
    {
      gcTime: 0,
      suppressErrorToast: true,
      invalidate: [pharmacySupplierCreditKey(scope)],
      onSuccess: finish,
      onError: (error) => {
        working.current = false;
        if (
          !uncertain.current &&
          error instanceof ApiError &&
          [400, 401, 403, 404, 409, 422].includes(error.status)
        ) {
          try {
            sessionStorage.removeItem(storageKey);
          } catch {
            uncertain.current = true;
            setNotice(
              "Keep this recovery reference and check the original credit before continuing.",
            );
            return;
          }
          setCommand(null);
          setAttempt(null);
          setNotice("Credit was not recorded. Check the document details and your current access.");
        } else {
          uncertain.current = true;
          setNotice(
            "The credit outcome is unconfirmed. Check the original credit before retrying.",
          );
        }
      },
    },
  );
  const recover = useApiMutation(
    (id: string) => recoverPharmacySupplierCredit(scope, id, undefined, attempt?.input),
    {
      gcTime: 0,
      suppressErrorToast: true,
      invalidate: [pharmacySupplierCreditKey(scope)],
      onSuccess: finish,
      onError: (error) => {
        working.current = false;
        if (error instanceof ApiError && error.status === 404) {
          setMissing(true);
          setNotice(
            attempt
              ? "The original is not visible. You can retry the exact submitted credit using the same recovery reference."
              : "The original is not visible. Re-enter the original supplier note using the same recovery reference; an existing record cannot be overwritten.",
          );
        } else
          setNotice(
            "The original credit could not be verified. Keep its recovery reference and check again with current financial access.",
          );
      },
    },
  );
  const pending = save.isPending || recover.isPending;
  useLeaveGuard(!!command || form.formState.isDirty, "supplier credit", pending);
  function submit(input: PharmacySupplierCreditInput) {
    if (
      working.current ||
      !canRecord ||
      !source ||
      sources.isError ||
      input.return_id !== source.id ||
      input.currency !== source.currency
    )
      return;
    const id = command ?? newPharmacyId();
    try {
      sessionStorage.setItem(storageKey, id);
    } catch {
      setNotice("This browser cannot keep a recovery reference. No credit has been submitted.");
      return;
    }
    const next = { id, input };
    working.current = true;
    setCommand(id);
    setAttempt(next);
    setMissing(false);
    setNotice("");
    save.mutate(next);
  }
  if (!canRecord && command === null)
    return (
      <Panel>
        <p role="status">
          New supplier credit recording is unavailable for this account or release.
        </p>
        <Button variant="ghost" onClick={() => confirmLeave(close)}>
          Close credit form
        </Button>
      </Panel>
    );
  return (
    <Panel>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 className="font-semibold">
          {command ? "Supplier credit recovery" : "Record actual supplier credit"}
        </h3>
        <Button variant="ghost" disabled={pending} onClick={() => confirmLeave(close)}>
          Close credit form
        </Button>
      </div>
      {notice && (
        <p role="alert" className="my-3 text-sm">
          {notice}
        </p>
      )}
      {!visible ? (
        <div className="mt-4 space-y-3">
          <p>Keep the original reference until the recorded credit is verified.</p>
          {pending && <p role="status">Verifying supplier credit…</p>}
          <Button
            disabled={pending || !command}
            onClick={() => {
              if (working.current || !command) return;
              working.current = true;
              uncertain.current = true;
              setMissing(false);
              recover.mutate(command);
            }}
          >
            Check original credit
          </Button>
          {missing && attempt && canRecord && (
            <Button
              variant="outline"
              disabled={pending}
              onClick={() => {
                if (working.current) return;
                working.current = true;
                setMissing(false);
                save.mutate(attempt);
              }}
            >
              Retry exact credit
            </Button>
          )}
        </div>
      ) : sources.isError ? (
        <RequestFailure
          message="Dispatched financial sources are unavailable. Previously loaded choices are hidden."
          error={sources.error}
          retry={sources.retry}
        />
      ) : sources.isPending ? (
        <p role="status">Loading dispatched financial sources…</p>
      ) : (
        <form
          noValidate
          className="mt-4 space-y-4"
          onSubmit={(event) => {
            void form.handleSubmit(submit)(event);
          }}
        >
          <FormSelectField
            control={form.control}
            name="return_id"
            id="supplier-credit-source"
            label="Dispatched supplier return"
            disabled={pending}
            options={sources.items.map((row) => ({
              value: row.id,
              label: `Return ${row.number} · ${row.payee_name} · ${row.original_bill_reference}`,
            }))}
            onValueChange={(value) => {
              form.reset(defaults);
              form.setValue("return_id", value, { shouldDirty: true });
              const selected = sources.items.find((row) => row.id === value);
              if (selected) form.setValue("currency", selected.currency);
            }}
          />
          <LoadMore
            label="Load more dispatched returns"
            hasMore={sources.hasMore}
            isLoading={sources.isFetchingMore}
            onClick={sources.loadMore}
          />
          {!sources.items.length && <p>No dispatched returns are available for this outlet.</p>}
          {source && (
            <>
              <p className="text-sm text-muted-foreground">
                Expected return value:{" "}
                {formatPharmacyMoney(source.expected_credit_minor, source.currency)}. Enter the
                actual amount on the supplier credit note below.
              </p>
              <Field
                id="supplier-credit-reference"
                label="Supplier credit reference"
                error={form.formState.errors.document_reference?.message}
              >
                <Input
                  id="supplier-credit-reference"
                  {...form.register("document_reference")}
                  maxLength={100}
                  disabled={pending}
                />
              </Field>
              <Field
                id="supplier-credit-issued"
                label="Credit issue date"
                error={form.formState.errors.issued_on?.message}
              >
                <Input
                  id="supplier-credit-issued"
                  type="date"
                  {...form.register("issued_on")}
                  disabled={pending}
                />
              </Field>
              <Controller
                control={form.control}
                name="amount_minor"
                render={({ field, fieldState }) => (
                  <CurrencyAmountField
                    key={source.id}
                    id="supplier-credit-amount"
                    label="Actual credit amount"
                    currency={source.currency}
                    initialMinor={Number.isSafeInteger(field.value) ? field.value : null}
                    onChange={field.onChange}
                    inputRef={field.ref}
                    onBlur={field.onBlur}
                    error={fieldState.error?.message}
                    disabled={pending}
                  />
                )}
              />
              <Field
                id="supplier-credit-reason"
                label="Credit record reason"
                error={form.formState.errors.reason?.message}
              >
                <Input
                  id="supplier-credit-reason"
                  {...form.register("reason")}
                  maxLength={500}
                  disabled={pending}
                />
              </Field>
              <Button type="submit" disabled={pending || !canRecord}>
                {command ? "Retry original supplier credit" : "Save supplier credit"}
              </Button>
            </>
          )}
        </form>
      )}
    </Panel>
  );
}
