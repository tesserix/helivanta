"use client";

import "@/lib/csp-zod";
import { useRef, useState, useSyncExternalStore } from "react";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  newPharmacyId,
  pharmacySupplierCreditKey,
  pharmacySupplierCreditMatchInputSchema,
  pharmacySupplierCreditMatchReversalInputSchema,
  readPharmacySupplierCreditMatchContext,
  readPharmacySupplierCreditMatchReversal,
  reversePharmacySupplierCreditMatch,
  recoverPharmacySupplierCreditMatchReversal,
  useApiMutation,
  type PharmacyActorScope,
  type PharmacySupplierCreditMatch,
  type PharmacySupplierCreditMatchReversalInput,
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

type Props = {
  scope: PharmacyActorScope;
  credit: string;
  requested: { id: string; sequence: number } | null;
  canManage: boolean;
};
const referenceSchema = z
  .object({
    id: pharmacySupplierCreditMatchInputSchema.shape.return_id,
    match: pharmacySupplierCreditMatchInputSchema.shape.return_id,
  })
  .strict();
const formSchema = pharmacySupplierCreditMatchReversalInputSchema.pick({ reason: true });
const subscribe = () => () => {};
export function SupplierCreditMatchReversal(props: Props) {
  const ready = useSyncExternalStore(
    subscribe,
    () => true,
    () => false,
  );
  if (!ready) return <p role="status">Checking matching reversal recovery…</p>;
  return (
    <ReversalController
      key={JSON.stringify([
        props.scope.tenantId,
        props.scope.subject,
        props.scope.pharmacyId,
        props.scope.outletId,
        props.credit,
        props.requested,
      ])}
      {...props}
    />
  );
}
function loadReference(key: string) {
  try {
    const raw = sessionStorage.getItem(key);
    return { reference: raw === null ? null : referenceSchema.parse(JSON.parse(raw)), error: "" };
  } catch {
    return {
      reference: null,
      error:
        "Matching reversal recovery is unavailable. Restore session storage and its original reference before continuing.",
    };
  }
}
function ReversalController({ scope, credit, requested, canManage }: Props) {
  const key = `helivanta:supplier-credit-match-reversal:v1:${JSON.stringify([scope.tenantId, scope.subject, scope.pharmacyId, scope.outletId, credit])}`;
  const [initial] = useState(() => loadReference(key));
  const target = initial.reference?.match ?? requested?.id ?? null;
  const [command, setCommand] = useState(initial.reference?.id ?? null);
  const [open, setOpen] = useState(target !== null);
  const [attempt, setAttempt] = useState<{
    id: string;
    input: PharmacySupplierCreditMatchReversalInput;
    original: PharmacySupplierCreditMatch;
  } | null>(null);
  const [missing, setMissing] = useState(false),
    [notice, setNotice] = useState("");
  const working = useRef(false),
    uncertain = useRef(initial.reference !== null);
  const confirmLeave = useConfirmLeave();
  const form = useZodForm(formSchema, { reason: "" });
  const context = useQuery({
    queryKey: pharmacySupplierCreditKey(scope, "match-reversal-context", credit, target),
    queryFn: ({ signal }) => readPharmacySupplierCreditMatchContext(scope, credit, target!, signal),
    enabled: open && target !== null && !initial.error,
    gcTime: 0,
    retry: false,
  });
  const reversal = useQuery({
    queryKey: pharmacySupplierCreditKey(
      scope,
      "match-reversal-evidence",
      credit,
      target,
      context.data?.match.reversal_id,
    ),
    queryFn: ({ signal }) =>
      readPharmacySupplierCreditMatchReversal(scope, credit, target!, signal, context.data!.match),
    enabled: open && !context.isError && !!context.data?.match.reversal_id,
    gcTime: 0,
    retry: false,
  });
  const current = !context.isError ? context.data : undefined;
  const canReverse =
    canManage && !!current && current.credit.state === "active" && !current.match.reversal_id;
  function finish() {
    working.current = false;
    try {
      sessionStorage.removeItem(key);
    } catch {
      setOpen(false);
      setNotice(
        "Reversal is verified but its recovery reference could not be cleared. Check the same original before continuing.",
      );
      return;
    }
    uncertain.current = false;
    setCommand(null);
    setAttempt(null);
    setMissing(false);
    form.reset({ reason: "" });
    setNotice("Return matching reversal verified. Stock and bill balances are unchanged.");
  }
  const save = useApiMutation(
    ({
      id,
      input,
      original,
    }: {
      id: string;
      input: PharmacySupplierCreditMatchReversalInput;
      original: PharmacySupplierCreditMatch;
    }) => reversePharmacySupplierCreditMatch(scope, credit, original, id, input),
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
            sessionStorage.removeItem(key);
          } catch {
            uncertain.current = true;
            setNotice(
              "Keep the reversal reference and check the original result before continuing.",
            );
            return;
          }
          setCommand(null);
          setAttempt(null);
          setNotice(
            "Reversal was refused. Refresh the current match observations before creating another command.",
          );
        } else {
          uncertain.current = true;
          setNotice(
            "The reversal outcome is unconfirmed. Check the original reversal before retrying.",
          );
        }
      },
    },
  );
  const recover = useApiMutation(
    (id: string) =>
      recoverPharmacySupplierCreditMatchReversal(scope, credit, target!, id, undefined, {
        original: current!.match,
        input: attempt?.input,
      }),
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
              ? "The original reversal is not visible. Retry only the exact submitted reversal."
              : "The original reversal is not visible. Re-enter its original reason using the same reference; an existing command cannot be overwritten.",
          );
        } else
          setNotice(
            "The original reversal could not be verified. Keep its reference and check again with current financial access.",
          );
      },
    },
  );
  const pending = save.isPending || recover.isPending;
  useLeaveGuard(
    !!command || (open && form.formState.isDirty),
    "supplier matching reversal",
    pending,
  );
  function submit(values: ReturnType<typeof formSchema.parse>) {
    if (
      working.current ||
      !canReverse ||
      !current ||
      context.isFetching ||
      context.isError ||
      reversal.isError
    )
      return;
    const input = pharmacySupplierCreditMatchReversalInputSchema.parse({
      ...values,
      expected_version: current.credit.version,
      expected_return_matched_minor: current.returned.matched_minor,
    });
    const id = command ?? newPharmacyId();
    try {
      sessionStorage.setItem(key, JSON.stringify({ id, match: target }));
    } catch {
      setNotice("Session storage is unavailable. No reversal has been submitted.");
      return;
    }
    const next = { id, input, original: current.match };
    working.current = true;
    setCommand(id);
    setAttempt(next);
    setMissing(false);
    setNotice("");
    save.mutate(next);
  }
  if (initial.error) return <p role="alert">{initial.error}</p>;
  if (!open)
    return (
      <>
        {notice && <p role="status">{notice}</p>}
        {command && (
          <Button onClick={() => setOpen(true)}>Resume matching reversal recovery</Button>
        )}
      </>
    );
  return (
    <section aria-label="Return matching reversal">
      <Panel>
        <div className="flex flex-wrap justify-between gap-3">
          <h4 className="font-semibold">Review return match</h4>
          <Button
            variant="ghost"
            disabled={pending}
            onClick={() => confirmLeave(() => setOpen(false))}
          >
            Close match review
          </Button>
        </div>
        {context.isError || reversal.isError ? (
          <RequestFailure
            message="Match reversal evidence is unavailable. Previously loaded financial details are hidden."
            error={context.error ?? reversal.error}
            retry={() => {
              void context.refetch();
              if (reversal.isError) void reversal.refetch();
            }}
          />
        ) : !current ? (
          <p role="status">Loading current matching observations…</p>
        ) : (
          <>
            <h5 className="mt-4 font-semibold">
              Return {current.match.return_number} · {current.credit.document_reference}
            </h5>
            <p className="mt-3">
              Original matched amount:{" "}
              {formatPharmacyMoney(current.match.amount_minor, current.credit.currency)}
            </p>
            <p>
              Current return attribution across credits:{" "}
              {formatPharmacyMoney(current.returned.matched_minor, current.credit.currency)}
            </p>
            <p className="my-3 text-sm">
              {current.match.reason} · Matched by{" "}
              <AccountName subject={current.match.recorded_by} />
            </p>
            <p className="my-3 text-sm text-muted-foreground">
              Full reversal removes only this match. Other credits' attribution, unused credit, bill
              liability and physical stock remain unchanged.
            </p>
            {notice && <p role="status">{notice}</p>}
            {command && !(missing && attempt === null && canReverse) ? (
              <div className="my-4 space-y-3">
                <Button
                  disabled={pending || context.isFetching}
                  onClick={() => {
                    if (working.current || !command || !current) return;
                    working.current = true;
                    uncertain.current = true;
                    setMissing(false);
                    recover.mutate(command);
                  }}
                >
                  Check original reversal
                </Button>
                {missing && attempt && canReverse && (
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
                    Retry exact reversal
                  </Button>
                )}
              </div>
            ) : current.match.reversal_id ? (
              <>
                <p className="font-medium">This match has already been reversed.</p>
                {reversal.isPending ? (
                  <p role="status">Loading retained reversal evidence…</p>
                ) : (
                  reversal.data && (
                    <div className="my-3">
                      <p>{reversal.data.reason}</p>
                      <p className="text-sm">
                        Reversed by <AccountName subject={reversal.data.recorded_by} />
                      </p>
                    </div>
                  )
                )}
              </>
            ) : canReverse ? (
              <form
                noValidate
                className="my-4 space-y-4"
                onSubmit={(event) => {
                  void form.handleSubmit(submit)(event);
                }}
              >
                <Field
                  id="supplier-match-reversal-reason"
                  label="Reversal reason"
                  error={form.formState.errors.reason?.message}
                >
                  <Input
                    id="supplier-match-reversal-reason"
                    {...form.register("reason")}
                    maxLength={500}
                    disabled={pending}
                  />
                </Field>
                <div className="flex flex-wrap gap-3">
                  <Button
                    type="submit"
                    variant="destructive"
                    disabled={pending || context.isFetching}
                  >
                    {command ? "Retry original reversal" : "Reverse full match"}
                  </Button>
                  <RefreshButton
                    refresh={context.refetch}
                    error={context.error}
                    busy={pending || context.isFetching}
                  >
                    Refresh current match observations
                  </RefreshButton>
                </div>
              </form>
            ) : (
              <p>New matching reversal is unavailable for this account, credit state or release.</p>
            )}
          </>
        )}
      </Panel>
    </section>
  );
}
