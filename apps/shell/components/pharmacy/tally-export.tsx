"use client";

import { useEffect, useId, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Input } from "@tesserix/web";
import { FileText } from "lucide-react";
import { z } from "zod";
import {
  ApiError,
  fetchPharmacyTallyVouchers,
  newPharmacyId,
  pharmacyTallyKey,
  pharmacyTallyLedgerNameSchema,
  readPharmacyTallyPreview,
  savePharmacyTallyLedgerMap,
  useApiMutation,
  type PharmacyActorScope,
  type PharmacyTallyLedgerMapInput,
  type PharmacyTallyPreview,
} from "@helivanta/api";
import { Field, Panel, RequestFailure, useLeaveGuard, useZodForm } from "@helivanta/ui";
import { RegisterExport } from "./h1-register";
import { periodSchema, type Period } from "./period";

/** Sales, credit notes and supplier payments as Tally vouchers, under the ledger names the outlet maps (#1690, ADR-0076). */
export function TallyExport({
  scope,
  today,
  writable,
}: {
  scope: PharmacyActorScope;
  today: string;
  writable: boolean;
}) {
  const [period, setPeriod] = useState<Period>({ from: today.slice(0, 8) + "01", to: today });
  const form = useZodForm(periodSchema, period);
  return (
    <Panel>
      <h3 className="flex items-center gap-2 font-semibold">
        <FileText className="size-5 text-(--hms-accent)" />
        Tally export
      </h3>
      <p className="mt-2 max-w-3xl text-sm leading-6 text-muted-foreground">
        Sales invoices, credit notes, supplier purchase bills and payments as Tally accounting
        vouchers, with GST split by rate into CGST, SGST, UTGST and IGST. Record each purchase
        bill’s source tax evidence first. Every preview and download is recorded.
      </p>
      <form
        noValidate
        className="mt-4 flex flex-wrap items-end gap-4"
        onSubmit={form.handleSubmit(setPeriod)}
      >
        <Field id="tally-from" label="From" error={form.formState.errors.from?.message}>
          <Input id="tally-from" type="date" {...form.register("from")} />
        </Field>
        <Field id="tally-to" label="To" error={form.formState.errors.to?.message}>
          <Input id="tally-to" type="date" {...form.register("to")} />
        </Field>
        <Button type="submit" variant="outline">
          Preview period
        </Button>
      </form>
      <Preview
        key={`${period.from}:${period.to}`}
        scope={scope}
        period={period}
        writable={writable}
      />
    </Panel>
  );
}

const COUNTS = [
  ["sale_count", "Sales invoices"],
  ["credit_note_count", "Credit notes"],
  ["supplier_payment_count", "Supplier payments"],
  ["payment_reversal_count", "Payment reversals"],
  ["purchase_count", "Supplier purchase bills"],
] as const;

function Preview({
  scope,
  period,
  writable,
}: {
  scope: PharmacyActorScope;
  period: Period;
  writable: boolean;
}) {
  const client = useQueryClient();
  const key = pharmacyTallyKey(scope, "preview", period.from, period.to);
  // Each preview is an audited read, so it runs when asked and never on focus.
  const preview = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => readPharmacyTallyPreview(scope, period, signal),
    gcTime: 0,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
    retry: false,
  });
  const [editing, setEditing] = useState(false);
  const refresh = () => void client.invalidateQueries({ queryKey: key });
  if (preview.isError)
    return (
      <div className="mt-5">
        <RequestFailure
          message={
            preview.error instanceof ApiError && preview.error.code === "tally_purchase_tax_missing"
              ? "Supplier bills in this period need source tax evidence. Open Supplier bills, record each bill’s purchase tax breakdown, then preview again."
              : "Could not load the Tally preview."
          }
          error={preview.error}
          retry={preview.refetch}
        />
      </div>
    );
  if (preview.isPending)
    return (
      <p role="status" className="mt-5">
        Loading the period…
      </p>
    );
  const data = preview.data;
  const vouchers = COUNTS.reduce((sum, [field]) => sum + data[field], 0);
  const unnamed = data.heads.filter((head) => head.ledger_name === null).length;
  return (
    <div className="mt-5 space-y-5">
      <dl className="grid max-w-3xl grid-cols-2 gap-3 sm:grid-cols-3">
        {COUNTS.map(([field, label]) => (
          <div key={field} className="rounded-md border p-3">
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className="mt-1 text-lg font-semibold tabular-nums">{data[field]}</dd>
          </div>
        ))}
      </dl>
      {vouchers === 0 ? (
        <p className="text-sm text-muted-foreground">
          No sales, credit notes, supplier purchase bills or payments from {period.from} to{" "}
          {period.to}.
        </p>
      ) : (
        <>
          <LedgerNames
            key={`${data.ledger_map_id ?? "none"}:${data.heads.map((head) => head.head).join()}`}
            scope={scope}
            preview={data}
            writable={writable}
            previewKey={key}
            onEditing={setEditing}
            onConflict={refresh}
          />
          <div className="space-y-2">
            {editing ? (
              <p className="text-sm text-muted-foreground">
                Save the ledger names before downloading.
              </p>
            ) : unnamed ? (
              <p className="text-sm text-muted-foreground">
                Name a Tally ledger for {unnamed} account {unnamed === 1 ? "head" : "heads"} before
                downloading.
              </p>
            ) : (
              <RegisterExport
                label="Tally export"
                noun="Tally"
                format="XML"
                download={(signal) =>
                  explained(
                    fetchPharmacyTallyVouchers(scope, period, data.ledger_map_id, signal),
                    {
                      tally_purchase_tax_missing:
                        "New supplier bills need source tax evidence. Record it in Supplier bills, then preview again.",
                      tally_ledger_map_changed:
                        "The ledger names changed since this preview. Check them below, then download again.",
                      tally_ledgers_unmapped:
                        "The period now needs more ledger names. Name them below, then download again.",
                    },
                    refresh,
                  )
                }
              />
            )}
            <p className="max-w-3xl text-sm leading-6 text-muted-foreground">
              Import each file into Tally once: importing the same file again may duplicate its
              vouchers. The ledgers and voucher types named here must already exist in the Tally
              company, and each voucher type must accept the numbers Helivanta gives it.
            </p>
          </div>
        </>
      )}
    </div>
  );
}

// A known refusal means the preview is stale: reload it and say what to check.
async function explained<T>(
  request: Promise<T>,
  messages: Record<string, string>,
  refresh: () => void,
): Promise<T> {
  try {
    return await request;
  } catch (cause) {
    const message = cause instanceof ApiError ? messages[cause.code] : undefined;
    if (!(cause instanceof ApiError) || message === undefined) throw cause;
    refresh();
    throw new ApiError(cause.code, message, cause.status, { ...cause.detail });
  }
}

type Heads = PharmacyTallyPreview["heads"];
// A head with no name yet may stay blank; a named head keeps a name, since a map never drops one.
function namesSchema(heads: Heads) {
  return z
    .object({ names: z.array(z.object({ ledger_name: z.string() })) })
    .superRefine((value, ctx) =>
      value.names.forEach(({ ledger_name }, index) => {
        if (ledger_name === "" && heads[index]?.ledger_name === null) return;
        const parsed = pharmacyTallyLedgerNameSchema.safeParse(ledger_name);
        if (!parsed.success)
          ctx.addIssue({
            code: "custom",
            path: ["names", index, "ledger_name"],
            message: parsed.error.issues[0]?.message ?? "Enter the Tally ledger name.",
          });
      }),
    );
}
const headLabel = (head: Heads[number]) =>
  head.supplier_name ? `${head.label} (${head.supplier_name})` : head.label;

function LedgerNames({
  scope,
  preview,
  writable,
  previewKey,
  onEditing,
  onConflict,
}: {
  scope: PharmacyActorScope;
  preview: PharmacyTallyPreview;
  writable: boolean;
  previewKey: unknown[];
  onEditing: (editing: boolean) => void;
  onConflict: () => void;
}) {
  const prefix = useId();
  const [schema] = useState(() => namesSchema(preview.heads));
  const form = useZodForm(schema, {
    names: preview.heads.map((head) => ({ ledger_name: head.ledger_name ?? "" })),
  });
  const dirty = form.formState.isDirty;
  // One map id per intent: resending the same names after an unanswered save replays its receipt.
  const [attempt, setAttempt] = useState<{ intent: string; id: string } | null>(null);
  const save = useApiMutation(
    ({ id, input }: { id: string; input: PharmacyTallyLedgerMapInput }) =>
      explained(
        savePharmacyTallyLedgerMap(scope, id, input, AbortSignal.timeout(15_000)),
        {
          tally_ledger_map_conflict:
            "Someone saved ledger names since this preview. Check the names below and save again.",
        },
        onConflict,
      ),
    { successToast: "Ledger names saved", invalidate: [previewKey] },
  );
  useLeaveGuard(dirty, "Tally ledger names", save.isPending);
  useEffect(() => {
    onEditing(dirty);
    return () => onEditing(false);
  }, [dirty, onEditing]);

  if (!writable)
    return (
      <div className="space-y-2">
        <h4 className="text-sm font-semibold">Tally ledger names</h4>
        <dl className="max-w-3xl divide-y rounded-md border text-sm">
          {preview.heads.map((head) => (
            <div key={head.head} className="grid gap-1 px-4 py-3 sm:grid-cols-2">
              <dt className="wrap-anywhere text-muted-foreground">{headLabel(head)}</dt>
              <dd className={head.ledger_name === null ? "text-(--hms-amber)" : "wrap-anywhere"}>
                {head.ledger_name ?? "Not named"}
              </dd>
            </div>
          ))}
        </dl>
        <p className="text-sm text-muted-foreground">
          Naming ledgers needs editor access to this outlet.
        </p>
      </div>
    );
  return (
    <form
      noValidate
      className="space-y-4"
      onSubmit={form.handleSubmit((values) => {
        if (save.isPending) return;
        const entries = preview.heads.flatMap((head, index) => {
          const name = values.names[index]?.ledger_name ?? "";
          return name !== "" && name !== head.ledger_name
            ? [{ head: head.head, ledger_name: name }]
            : [];
        });
        if (!entries.length) return;
        const input = { replaces: preview.ledger_map_id, entries };
        const intent = JSON.stringify(input);
        const next = attempt?.intent === intent ? attempt : { intent, id: newPharmacyId() };
        setAttempt(next);
        save.mutate({ id: next.id, input });
      })}
    >
      <div>
        <h4 className="text-sm font-semibold">Tally ledger names</h4>
        <p className="mt-1 max-w-3xl text-sm text-muted-foreground">
          Enter each ledger exactly as it is named in Tally. Several heads may share one ledger.
          Saving keeps the earlier names as a version.
        </p>
      </div>
      <div className="grid max-w-3xl gap-4 sm:grid-cols-2">
        {preview.heads.map((head, index) => (
          <Field
            key={head.head}
            id={`${prefix}-${index}`}
            label={headLabel(head)}
            error={form.formState.errors.names?.[index]?.ledger_name?.message}
          >
            <Input
              id={`${prefix}-${index}`}
              autoComplete="off"
              maxLength={100}
              {...form.register(`names.${index}.ledger_name`)}
            />
          </Field>
        ))}
      </div>
      <Button type="submit" disabled={!dirty || save.isPending}>
        {save.isPending ? "Saving…" : "Save ledger names"}
      </Button>
    </form>
  );
}
