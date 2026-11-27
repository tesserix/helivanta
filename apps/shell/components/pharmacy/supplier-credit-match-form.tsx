"use client";

import "@/lib/csp-zod";
import { useRef, useState, useSyncExternalStore } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  newPharmacyId,
  pharmacySupplierCreditMatchInputSchema,
  pharmacySupplierCreditKey,
  listPharmacySupplierCreditReturnChoices,
  matchPharmacySupplierCredit,
  recoverPharmacySupplierCreditMatch,
  useApiMutation,
  usePagedQuery,
  type PharmacyActorScope,
  type PharmacySupplierCreditContext,
  type PharmacySupplierCreditMatchInput,
} from "@helivanta/api";
import {
  Controller,
  Field,
  FormSelectField,
  LoadMore,
  RequestFailure,
  useConfirmLeave,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";
import { CurrencyAmountField, formatPharmacyMoney } from "./currency-amount-field";

type Props = {
  scope: PharmacyActorScope;
  credit: PharmacySupplierCreditContext;
  canManage: boolean;
};
const formSchema = pharmacySupplierCreditMatchInputSchema.pick({
  return_id: true,
  amount_minor: true,
  reason: true,
  overage_reason: true,
});
const subscribe = () => () => {};
export function SupplierCreditMatchForm(props: Props) {
  const ready = useSyncExternalStore(
    subscribe,
    () => true,
    () => false,
  );
  if (!ready) return <p role="status">Checking return matching recovery…</p>;
  return (
    <MatchController
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
function readReference(key: string) {
  try {
    const id = sessionStorage.getItem(key);
    if (
      id !== null &&
      !pharmacySupplierCreditMatchInputSchema.shape.return_id.safeParse(id).success
    )
      return {
        id: null,
        error: "The saved matching reference is invalid. Resolve it before creating another match.",
      };
    return { id, error: "" };
  } catch {
    return { id: null, error: "Session storage is unavailable. No return match can be submitted." };
  }
}
function MatchController({ scope, credit, canManage }: Props) {
  const queryClient = useQueryClient();
  const [refreshing, setRefreshing] = useState(false);
  const storageKey = `helivanta:supplier-credit-match:v1:${JSON.stringify([scope.tenantId, scope.subject, scope.pharmacyId, scope.outletId, credit.id])}`;
  const [initial] = useState(() => readReference(storageKey));
  const [command, setCommand] = useState(initial.id);
  const [open, setOpen] = useState(initial.id !== null);
  const [attempt, setAttempt] = useState<{
    id: string;
    input: PharmacySupplierCreditMatchInput;
  } | null>(null);
  const [missing, setMissing] = useState(false),
    [notice, setNotice] = useState("");
  const working = useRef(false),
    uncertain = useRef(initial.id !== null);
  const confirmLeave = useConfirmLeave();
  const defaults = { return_id: "", amount_minor: undefined, reason: "", overage_reason: null };
  const form = useZodForm(formSchema, defaults);
  const returnID = form.watch("return_id");
  const canCreate =
    canManage && credit.state === "active" && credit.matched_minor < credit.amount_minor;
  const visible = open && canCreate && (command === null || (missing && attempt === null));
  const sources = usePagedQuery(
    pharmacySupplierCreditKey(scope, "match-sources", credit.id),
    (cursor, signal) => listPharmacySupplierCreditReturnChoices(scope, credit.id, cursor, signal),
    { enabled: visible, gcTime: 0, retry: false },
  );
  const source = !sources.isError ? sources.items.find((row) => row.id === returnID) : undefined;
  function finish() {
    working.current = false;
    try {
      sessionStorage.removeItem(storageKey);
    } catch {
      setOpen(false);
      setNotice(
        "Return matching is verified but its recovery reference could not be cleared. Check the same original before another match.",
      );
      return;
    }
    uncertain.current = false;
    setCommand(null);
    setAttempt(null);
    setMissing(false);
    setOpen(false);
    form.reset(defaults);
    setNotice("Return matching verified. Bill applications and physical stock are unchanged.");
  }
  const save = useApiMutation(
    ({ id, input }: { id: string; input: PharmacySupplierCreditMatchInput }) =>
      matchPharmacySupplierCredit(scope, credit.id, id, input),
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
              "Keep the matching reference and check the original result before continuing.",
            );
            return;
          }
          setCommand(null);
          setAttempt(null);
          setNotice(
            "Match was refused. Refresh the credit and dispatched returns, then review the current observations.",
          );
        } else {
          uncertain.current = true;
          setNotice(
            "The matching outcome is unconfirmed. Check the original match before retrying.",
          );
        }
      },
    },
  );
  const recover = useApiMutation(
    (id: string) =>
      recoverPharmacySupplierCreditMatch(scope, credit.id, id, undefined, attempt?.input),
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
              ? "The original match is not visible. Retry only the exact submitted match."
              : "The original match is not visible. Re-enter the original match using the same reference; an existing command cannot be overwritten.",
          );
        } else
          setNotice(
            "The original match could not be verified. Keep its reference and check again with current financial access.",
          );
      },
    },
  );
  const pending = save.isPending || recover.isPending || refreshing;
  useLeaveGuard(!!command || form.formState.isDirty, "supplier credit matching", pending);
  function submit(values: ReturnType<typeof formSchema.parse>) {
    if (
      working.current ||
      !canCreate ||
      !source ||
      sources.isError ||
      sources.isRefreshing ||
      source.currency !== credit.currency
    )
      return;
    if (values.amount_minor > credit.amount_minor - credit.matched_minor) {
      form.setError("amount_minor", {
        message: "The amount exceeds the credit remaining for return matching.",
      });
      return;
    }
    if (
      source.matched_minor + values.amount_minor > source.expected_credit_minor &&
      !values.overage_reason
    ) {
      form.setError("overage_reason", {
        message: "Explain why the total attribution exceeds the expected return value.",
      });
      return;
    }
    const input = pharmacySupplierCreditMatchInputSchema.parse({
      ...values,
      expected_version: credit.version,
      expected_return_matched_minor: source.matched_minor,
    });
    const id = command ?? newPharmacyId();
    try {
      sessionStorage.setItem(storageKey, id);
    } catch {
      setNotice("Session storage is unavailable. No return match has been submitted.");
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
  if (initial.error) return <p role="alert">{initial.error}</p>;
  return (
    <div className="my-4 space-y-3">
      {notice && <p role="status">{notice}</p>}
      {!open ? (
        (command !== null || canCreate) && (
          <Button
            onClick={() => {
              setOpen(true);
              setNotice("");
            }}
          >
            {command ? "Resume matching recovery" : "Match credit to return"}
          </Button>
        )
      ) : (
        <>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h4 className="font-semibold">
              {command ? "Return matching recovery" : "Match actual credit to return"}
            </h4>
            <Button
              variant="ghost"
              disabled={pending}
              onClick={() => confirmLeave(() => setOpen(false))}
            >
              Close matching form
            </Button>
          </div>
          {!visible ? (
            <>
              <p>Check the original matching reference before creating another command.</p>
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
                Check original match
              </Button>
              {missing && attempt && canCreate && (
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
                  Retry exact match
                </Button>
              )}
            </>
          ) : sources.isError ? (
            <RequestFailure
              message="Dispatched return choices are unavailable. Previously loaded sources are hidden."
              error={sources.error}
              retry={sources.retry}
            />
          ) : sources.isPending ? (
            <p role="status">Loading matching sources…</p>
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
                name="return_id"
                id="supplier-credit-match-return"
                label="Dispatched return to match"
                disabled={pending}
                options={sources.items.map((row) => ({
                  value: row.id,
                  label: `Return ${row.number} · ${row.payee_name} · ${row.original_bill_reference}`,
                }))}
                onValueChange={(value) => {
                  form.reset(defaults);
                  form.setValue("return_id", value, { shouldDirty: true });
                }}
              />
              <LoadMore
                label="Load more matching sources"
                hasMore={sources.hasMore}
                isLoading={sources.isFetchingMore}
                onClick={sources.loadMore}
              />
              <Button
                type="button"
                variant="outline"
                disabled={pending || sources.isRefreshing}
                onClick={() => {
                  if (working.current) return;
                  working.current = true;
                  setRefreshing(true);
                  void queryClient
                    .invalidateQueries({ queryKey: pharmacySupplierCreditKey(scope) })
                    .finally(() => {
                      working.current = false;
                      setRefreshing(false);
                    });
                }}
              >
                Refresh credit and matching sources
              </Button>
              {!sources.items.length && <p>No dispatched returns are available for matching.</p>}
              {source && (
                <>
                  <p className="text-sm">
                    Expected return value:{" "}
                    {formatPharmacyMoney(source.expected_credit_minor, credit.currency)} · Already
                    attributed across credits:{" "}
                    {formatPharmacyMoney(source.matched_minor, credit.currency)} · Credit remaining
                    for attribution:{" "}
                    {formatPharmacyMoney(
                      credit.amount_minor - credit.matched_minor,
                      credit.currency,
                    )}
                  </p>
                  <Controller
                    control={form.control}
                    name="amount_minor"
                    render={({ field, fieldState }) => (
                      <CurrencyAmountField
                        key={source.id}
                        id="supplier-credit-match-amount"
                        label="Amount to match"
                        currency={credit.currency}
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
                    id="supplier-credit-match-reason"
                    label="Matching reason"
                    error={form.formState.errors.reason?.message}
                  >
                    <Input
                      id="supplier-credit-match-reason"
                      {...form.register("reason")}
                      disabled={pending}
                    />
                  </Field>
                  <Field
                    id="supplier-credit-match-overage"
                    label="Excess attribution explanation"
                    error={form.formState.errors.overage_reason?.message}
                  >
                    <Input
                      id="supplier-credit-match-overage"
                      {...form.register("overage_reason", {
                        setValueAs: (value: string | null) => value?.trim() || null,
                      })}
                      disabled={pending}
                    />
                  </Field>
                  <Button type="submit" disabled={pending || sources.isRefreshing || !canCreate}>
                    {command ? "Retry original match" : "Save return match"}
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
