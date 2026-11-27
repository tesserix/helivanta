"use client";

import { useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Input } from "@tesserix/web";
import { z } from "zod";
import {
  ApiError,
  taxScheme,
  validateTaxRegistration,
  formatRatePercent,
  pharmacyPurchaseTaxInputSchema,
  pharmacyPurchaseTaxKey,
  pharmacyTallyKey,
  readPharmacyPurchaseTax,
  savePharmacyPurchaseTax,
  useApiMutation,
  type PharmacyActorScope,
  type PharmacyPayableDetail,
  type PharmacyPurchaseTaxReceipt,
} from "@helivanta/api";
import {
  AccountName,
  Field,
  FormSelectField,
  Panel,
  RequestFailure,
  RefreshButton,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";
import { CurrencyAmountField, formatPharmacyMoney } from "./currency-amount-field";
import { useCommandId } from "./command-id";

const components = ["cgst", "sgst", "igst", "utgst"] as const;
const scheme = taxScheme("IN")!;
const jurisdictions = scheme.jurisdictions.map((s) => ({ value: s.code, label: s.name }));

function evidenceForm(doc: PharmacyPayableDetail) {
  return pharmacyPurchaseTaxInputSchema.superRefine((value, ctx) => {
    const issue = (path: (string | number)[], message: string) =>
      ctx.addIssue({ code: "custom", path, message });
    const supplier = scheme.jurisdictions.find((s) => s.code === value.supplier_state);
    const gstSupply = value.lines.some((line) => line.supply_nature !== "outside_gst");
    if ((gstSupply || value.supplier_state !== "") && !supplier)
      issue(["supplier_state"], "Choose the supplier's state or territory.");
    if (
      (gstSupply || value.place_of_supply !== "") &&
      !scheme.jurisdictions.some((s) => s.code === value.place_of_supply)
    )
      issue(["place_of_supply"], "Choose the invoice's place of supply.");
    if (
      value.supplier_gstin &&
      validateTaxRegistration(scheme, value.supplier_state, value.supplier_gstin)
    )
      issue(["supplier_gstin"], "Check the supplier GSTIN and its state.");
    value.lines.forEach((line, i) => {
      const source = doc.lines[i];
      const total = components.reduce((sum, c) => sum + line[`${c}_minor`], 0);
      const recoverable = components.reduce((sum, c) => sum + line[`${c}_recoverable_minor`], 0);
      if (
        !source ||
        line.position !== i + 1 ||
        total !== source.tax_minor ||
        recoverable !== source.recoverable_tax_minor
      )
        issue(
          ["lines", i],
          "Tax components and recoverable amounts must match the posted source totals.",
        );
      if (
        line.supply_nature === "taxable"
          ? line.rate_ppm <= 0 || line.rate_ppm % 100 !== 0
          : line.rate_ppm !== 0
      )
        issue(
          ["lines", i, "rate_ppm"],
          "Enter the source rate to at most two decimal places; non-taxable lines have a zero rate.",
        );
      if (source && line.assessable_minor > source.net_minor + source.landed_minor)
        issue(
          ["lines", i, "assessable_minor"],
          "The source base value cannot exceed the original net plus landed costs.",
        );
      if (
        line.supply_nature === "taxable" &&
        Number.isSafeInteger(line.assessable_minor) &&
        Number.isSafeInteger(line.rate_ppm)
      ) {
        if (source && source.net_minor + source.landed_minor > 0 && line.assessable_minor === 0)
          issue(["lines", i, "assessable_minor"], "Enter the source value for this taxable line.");
        const denominator = BigInt(
          value.supplier_state === value.place_of_supply ? 2000000 : 1000000,
        );
        const expected = Number(
          (BigInt(line.assessable_minor) * BigInt(line.rate_ppm) + denominator / BigInt(2)) /
            denominator,
        );
        const amounts =
          value.supplier_state === value.place_of_supply
            ? [line.cgst_minor, line.sgst_minor + line.utgst_minor]
            : [line.igst_minor];
        if (
          amounts.some((amount) => Math.abs(amount - expected) > 1) &&
          !line.source_variance_reason
        )
          issue(
            ["lines", i, "assessable_minor"],
            "Check the source base and GST rate. If the invoice uses a different rounding calculation, record its explanation below.",
          );
      }
      if (total > 0 && (!value.supplier_gstin || line.supply_nature !== "taxable"))
        issue(["lines", i], "Positive GST needs the supplier GSTIN and a taxable classification.");
      for (const c of components) {
        if (line[`${c}_recoverable_minor`] > line[`${c}_minor`])
          issue(
            ["lines", i, `${c}_recoverable_minor`],
            "Recoverable tax cannot exceed this component.",
          );
      }
      if (
        value.supplier_state !== value.place_of_supply
          ? line.cgst_minor + line.sgst_minor + line.utgst_minor !== 0
          : line.igst_minor !== 0 ||
            Math.abs(line.cgst_minor - line.sgst_minor - line.utgst_minor) > 1 ||
            (supplier?.kind === "union_territory" ? line.sgst_minor !== 0 : line.utgst_minor !== 0)
      )
        issue(
          ["lines", i],
          "Check interstate IGST versus the local CGST and SGST/UTGST components.",
        );
    });
  });
}

export function PurchaseTax({
  scope,
  doc,
  writable,
}: {
  scope: PharmacyActorScope;
  doc: PharmacyPayableDetail;
  writable: boolean;
}) {
  const key = pharmacyPurchaseTaxKey(scope, doc.id);
  const evidence = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => readPharmacyPurchaseTax(scope, doc.id, signal),
    retry: false,
    gcTime: 0,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
  if (evidence.isError)
    return (
      <RequestFailure
        message="Could not load purchase tax evidence."
        error={evidence.error}
        retry={evidence.refetch}
      />
    );
  if (evidence.isPending || evidence.isFetching)
    return <p role="status">Loading purchase tax evidence…</p>;
  return (
    <Panel>
      <h4 className="font-semibold">Purchase tax evidence</h4>
      <p className="mt-2 text-sm text-muted-foreground">
        Copy the supplier invoice's GST breakdown. This adds export evidence without changing the
        posted bill, valuation or balance. Recoverability is your finance decision; this form does
        not determine legal tax credit eligibility.
      </p>
      <Evidence
        key={evidence.data?.id ?? "none"}
        scope={scope}
        doc={doc}
        writable={writable}
        current={evidence.data}
        reloadControl={
          <RefreshButton refresh={evidence.refetch} error={evidence.error}>
            Discard changes and reload
          </RefreshButton>
        }
      />
    </Panel>
  );
}

function Evidence({
  scope,
  doc,
  writable,
  current,
  reloadControl,
}: {
  scope: PharmacyActorScope;
  doc: PharmacyPayableDetail;
  writable: boolean;
  current: PharmacyPurchaseTaxReceipt | null;
  reloadControl: ReactNode;
}) {
  const [editing, setEditing] = useState(!current);
  if (current && !editing)
    return (
      <div className="mt-4 space-y-3 text-sm">
        <p>
          Supplier GSTIN: {current.supplier_gstin ?? "Not recorded"} · Place of supply:{" "}
          {current.place_of_supply
            ? (jurisdictions.find((s) => s.value === current.place_of_supply)?.label ??
              "Unavailable")
            : "Not applicable"}
        </p>
        <p>
          Recorded by <AccountName subject={current.recorded_by} /> ·{" "}
          <time dateTime={current.recorded_at}>{current.recorded_at.slice(0, 10)}</time>
        </p>
        <p>{current.reason}</p>
        <ul className="space-y-2">
          {current.lines.map((line, i) => (
            <li key={line.position}>
              {doc.lines[i]?.description} · {line.supply_nature.replaceAll("_", " ")} ·{" "}
              {formatRatePercent(line.rate_ppm)}%
              {components.map((c) => (
                <span key={c} className="block text-muted-foreground">
                  {c.toUpperCase()}: {formatPharmacyMoney(line[`${c}_minor`], "INR")} · Recoverable:{" "}
                  {formatPharmacyMoney(line[`${c}_recoverable_minor`], "INR")}
                </span>
              ))}
            </li>
          ))}
        </ul>
        {writable && (
          <Button variant="outline" onClick={() => setEditing(true)}>
            Record corrected evidence
          </Button>
        )}
      </div>
    );
  return (
    <div className="mt-4 space-y-3">
      {!current && (
        <p role="status" className="text-sm">
          No purchase tax evidence recorded. This bill must be classified before its period can be
          exported to Tally.
        </p>
      )}
      {writable ? (
        <EvidenceEditor
          scope={scope}
          doc={doc}
          current={current}
          reloadControl={reloadControl}
          cancel={current ? () => setEditing(false) : undefined}
        />
      ) : (
        <p className="text-sm text-muted-foreground">
          Recording evidence needs Editor access to this outlet.
        </p>
      )}
    </div>
  );
}

function EvidenceEditor({
  scope,
  doc,
  current,
  cancel,
  reloadControl,
}: {
  scope: PharmacyActorScope;
  doc: PharmacyPayableDetail;
  current: PharmacyPurchaseTaxReceipt | null;
  cancel?: () => void;
  reloadControl: ReactNode;
}) {
  const [discard, setDiscard] = useState<"reload" | "cancel" | null>(null);
  const [schema] = useState(() => evidenceForm(doc));
  const form = useZodForm(
    schema,
    current
      ? {
          supplier_gstin: current.supplier_gstin,
          supplier_state: current.supplier_state,
          place_of_supply: current.place_of_supply,
          lines: current.lines,
          replaces: current.id,
          reason: "",
        }
      : {
          replaces: null,
          supplier_gstin: null,
          supplier_state: "",
          place_of_supply: "",
          reason: "",
          lines: doc.lines.map((line, i) => ({
            position: i + 1,
            supply_nature: "taxable" as const,
            rate_ppm: 0,
            assessable_minor: line.net_minor,
            source_variance_reason: null,
            cgst_minor: 0,
            sgst_minor: 0,
            igst_minor: 0,
            utgst_minor: 0,
            cgst_recoverable_minor: 0,
            sgst_recoverable_minor: 0,
            igst_recoverable_minor: 0,
            utgst_recoverable_minor: 0,
          })),
        },
  );
  const client = useQueryClient();
  const command = useCommandId();
  const save = useApiMutation(
    async (values: z.infer<typeof schema>) => {
      const result = await savePharmacyPurchaseTax(
        scope,
        doc.id,
        command(values),
        values,
        AbortSignal.timeout(15000),
      );
      form.reset(values);
      void client.invalidateQueries({ queryKey: pharmacyPurchaseTaxKey(scope, doc.id) });
      void client.invalidateQueries({ queryKey: pharmacyTallyKey(scope) });
      return result;
    },
    { successToast: "Purchase tax evidence recorded", suppressErrorToast: true },
  );
  const conflict = save.error instanceof ApiError && save.error.code === "purchase_tax_conflict";
  const uncertain =
    save.isError &&
    !(save.error instanceof ApiError && save.error.status >= 400 && save.error.status < 500);
  useLeaveGuard(form.formState.isDirty, "Purchase tax evidence", save.isPending);
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((values) => {
        if (!save.isPending) save.mutate(values);
      })}
      className="space-y-5"
    >
      <fieldset disabled={save.isPending || uncertain} className="space-y-5">
        <p className="text-sm text-muted-foreground">
          GST jurisdiction fields can be blank when every line is outside GST. If you record a
          GSTIN, include its source state. Copy the base value from the document; taxable landed
          charges can change it.
        </p>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            id="purchase-gstin"
            label="Supplier GSTIN (if shown on the source document)"
            error={form.formState.errors.supplier_gstin?.message}
          >
            <Input
              id="purchase-gstin"
              value={form.watch("supplier_gstin") ?? ""}
              onChange={(event) =>
                form.setValue("supplier_gstin", event.target.value.trim().toUpperCase() || null, {
                  shouldDirty: true,
                })
              }
            />
          </Field>
          <FormSelectField
            control={form.control}
            name="supplier_state"
            id="purchase-state"
            label="Supplier state / territory"
            options={jurisdictions}
          />
          <FormSelectField
            control={form.control}
            name="place_of_supply"
            id="purchase-place"
            label="Place of supply on invoice"
            options={jurisdictions}
          />
          <Field
            id="purchase-reason"
            label="Source / correction reason"
            error={form.formState.errors.reason?.message}
          >
            <Input id="purchase-reason" {...form.register("reason")} />
          </Field>
        </div>
        {doc.lines.map((line, i) => (
          <fieldset key={i} className="space-y-3 border-t pt-4">
            <legend className="px-1 text-sm font-semibold">
              {i + 1}. {line.description}
            </legend>
            <p className="text-sm text-muted-foreground">
              Posted tax: {formatPharmacyMoney(line.tax_minor, doc.currency)} · Recoverable tax:{" "}
              {formatPharmacyMoney(line.recoverable_tax_minor, doc.currency)}
            </p>
            <div className="grid gap-4 sm:grid-cols-2">
              <FormSelectField
                control={form.control}
                name={`lines.${i}.supply_nature`}
                id={`purchase-nature-${i}`}
                label="Source tax treatment"
                options={[
                  { value: "taxable", label: "Taxable" },
                  { value: "nil_rated", label: "Nil-rated" },
                  { value: "exempt", label: "Exempt" },
                  { value: "outside_gst", label: "Outside GST" },
                ]}
              />
              <CurrencyAmountField
                id={`purchase-base-${i}`}
                label="Source line base value"
                currency="INR"
                initialMinor={form.getValues(`lines.${i}.assessable_minor`)}
                error={form.formState.errors.lines?.[i]?.assessable_minor?.message}
                onChange={(value) =>
                  form.setValue(`lines.${i}.assessable_minor`, value, { shouldDirty: true })
                }
              />
              <RateField
                initial={form.getValues(`lines.${i}.rate_ppm`)}
                id={`purchase-rate-${i}`}
                error={form.formState.errors.lines?.[i]?.rate_ppm?.message}
                onChange={(value) =>
                  form.setValue(`lines.${i}.rate_ppm`, value, { shouldDirty: true })
                }
              />
            </div>
            <Field
              id={`purchase-variance-${i}`}
              label="Source calculation difference explanation (only if needed)"
              error={form.formState.errors.lines?.[i]?.source_variance_reason?.message}
            >
              <Input
                id={`purchase-variance-${i}`}
                value={form.watch(`lines.${i}.source_variance_reason`) ?? ""}
                onChange={(event) =>
                  form.setValue(`lines.${i}.source_variance_reason`, event.target.value || null, {
                    shouldDirty: true,
                  })
                }
              />
            </Field>
            {components.map((c) => (
              <div key={c} className="grid gap-4 sm:grid-cols-2">
                <CurrencyAmountField
                  id={`purchase-${i}-${c}`}
                  label={c.toUpperCase()}
                  currency="INR"
                  initialMinor={form.getValues(`lines.${i}.${c}_minor`)}
                  error={form.formState.errors.lines?.[i]?.[`${c}_minor`]?.message}
                  onChange={(v) => form.setValue(`lines.${i}.${c}_minor`, v, { shouldDirty: true })}
                />
                <CurrencyAmountField
                  id={`purchase-${i}-${c}-recoverable`}
                  label={`Recoverable ${c.toUpperCase()}`}
                  currency="INR"
                  initialMinor={form.getValues(`lines.${i}.${c}_recoverable_minor`)}
                  error={form.formState.errors.lines?.[i]?.[`${c}_recoverable_minor`]?.message}
                  onChange={(v) =>
                    form.setValue(`lines.${i}.${c}_recoverable_minor`, v, { shouldDirty: true })
                  }
                />
              </div>
            ))}
            {form.formState.errors.lines?.[i]?.message && (
              <p role="alert" className="text-sm text-destructive">
                {form.formState.errors.lines[i]?.message}
              </p>
            )}
          </fieldset>
        ))}
      </fieldset>
      {conflict && (
        <div role="alert" className="space-y-2 text-sm">
          <p>Someone recorded a newer evidence version. Reload it before making a correction.</p>
          <Button type="button" variant="outline" onClick={() => setDiscard("reload")}>
            Reload current evidence
          </Button>
        </div>
      )}
      {save.isError && !conflict && (
        <RequestFailure
          message="Could not record purchase tax evidence. Retry the same details if the outcome is unknown; reload to resolve a version conflict."
          error={save.error}
          retry={() => {
            if (save.variables) save.mutate(save.variables);
          }}
        />
      )}
      {uncertain && (
        <p role="status" className="text-sm">
          The recording outcome is uncertain. Retry the original evidence before editing or starting
          a correction.
        </p>
      )}
      {discard && (
        <div role="alert" className="space-y-2 text-sm">
          <p>
            Discard these unsaved edits
            {discard === "reload" ? " and load the current evidence" : ""}?
          </p>
          <div className="flex gap-3">
            {discard === "reload" ? (
              reloadControl
            ) : (
              <Button
                type="button"
                onClick={() => {
                  setDiscard(null);
                  cancel?.();
                }}
              >
                Discard changes
              </Button>
            )}
            <Button type="button" variant="outline" onClick={() => setDiscard(null)}>
              Keep editing
            </Button>
          </div>
        </div>
      )}
      <div className="flex gap-3">
        <Button type="submit" disabled={save.isPending || uncertain}>
          {save.isPending ? "Recording…" : "Record purchase tax evidence"}
        </Button>
        {cancel && (
          <Button
            type="button"
            variant="outline"
            disabled={save.isPending || uncertain}
            onClick={() => {
              if (form.formState.isDirty) setDiscard("cancel");
              else cancel?.();
            }}
          >
            Cancel
          </Button>
        )}
      </div>
    </form>
  );
}

function RateField({
  initial,
  id,
  onChange,
  error,
}: {
  initial: number;
  id: string;
  onChange: (value: number) => void;
  error?: string;
}) {
  const [raw, setRaw] = useState(formatRatePercent(initial));
  return (
    <Field id={id} label="GST rate (%)" error={error}>
      <Input
        id={id}
        inputMode="decimal"
        value={raw}
        onChange={(event) => {
          const value = event.target.value;
          setRaw(value);
          const match = /^(\d{1,3})(?:\.(\d{0,2}))?$/.exec(value);
          const ppm = match
            ? Number(match[1]) * 10000 + Number((match[2] ?? "").padEnd(4, "0"))
            : Number.NaN;
          onChange(ppm <= 1000000 ? ppm : Number.NaN);
        }}
      />
    </Field>
  );
}
