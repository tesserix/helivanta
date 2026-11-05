"use client";

import { useEffect, useRef, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  fetchPharmacyPrescriptionRegisterCSV,
  pharmacyKey,
  pharmacyPrescriptionRegisterPath,
  pharmacyRoot,
  useApiPagedQuery,
  type PharmacyPrescriptionRegisterEntry,
  type PharmacyPrescriptionRegisterFilter,
  type PharmacyProduct,
  type PharmacyScope,
} from "@helivanta/api";
import { Field, FormSelectField, LoadMore, Panel, RequestFailure, useZodForm } from "@helivanta/ui";
import { RegisterExport } from "./h1-register";
import { periodSchema } from "./period";

/**
 * The prescription register (#1062; Drugs Rules 1945, Rule 65(3)(1)): every
 * Schedule G, H and H1 supply on a prescription, as recorded at the sale,
 * with what has come back. It is read, never edited, and every read is
 * recorded. Schedule H1 also keeps its own register (65(3)(h)).
 */

const filterSchema = periodSchema.and(
  z.object({
    regulatoryClass: z.enum(["all", "G", "H", "H1"]),
    productId: z.string(),
  }),
);
type FilterValues = z.infer<typeof filterSchema>;

export function PrescriptionRegister({ scope, today }: { scope: PharmacyScope; today: string }) {
  const [filter, setFilter] = useState<PharmacyPrescriptionRegisterFilter>({
    from: today.slice(0, 8) + "01",
    to: today,
  });
  const form = useZodForm(filterSchema, {
    from: filter.from,
    to: filter.to,
    regulatoryClass: "all",
    productId: "",
  });
  const products = useApiPagedQuery<PharmacyProduct>(
    pharmacyKey(scope, "prescription-register", "products"),
    `${pharmacyRoot(scope)}/catalogue/products?limit=100`,
  );
  const apply = (v: FilterValues) =>
    setFilter({
      from: v.from,
      to: v.to,
      ...(v.regulatoryClass !== "all" ? { regulatoryClass: v.regulatoryClass } : {}),
      ...(v.productId ? { productId: v.productId } : {}),
      // Changing the period or schedule keeps the patient being looked at.
      ...(filter.patientId ? { patientId: filter.patientId } : {}),
    });
  // Who is being looked at, by name, and where focus goes once narrowed.
  const [patientName, setPatientName] = useState("");
  const showEvery = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (filter.patientId) showEvery.current?.focus();
  }, [filter.patientId]);
  return (
    <Panel>
      <h3 className="font-semibold">Prescription register</h3>
      <p className="mt-1 text-sm text-muted-foreground">
        Every Schedule G, H and H1 supply on a prescription, with the prescriber, the patient, the
        drug, its manufacturer, batch and expiry, as recorded at the sale. The serial is the invoice
        and line. Returns are shown against their supply. The records cannot be edited, and every
        view and download is recorded.
      </p>
      <form
        noValidate
        className="mt-4 flex flex-wrap items-end gap-4"
        onSubmit={form.handleSubmit(apply)}
      >
        <Field id="rx-from" label="From" error={form.formState.errors.from?.message}>
          <Input id="rx-from" type="date" {...form.register("from")} />
        </Field>
        <Field id="rx-to" label="To" error={form.formState.errors.to?.message}>
          <Input id="rx-to" type="date" {...form.register("to")} />
        </Field>
        <FormSelectField
          control={form.control}
          name="regulatoryClass"
          id="rx-class"
          label="Schedule"
          options={[
            { value: "all", label: "G, H and H1" },
            { value: "G", label: "Schedule G" },
            { value: "H", label: "Schedule H" },
            { value: "H1", label: "Schedule H1" },
          ]}
        />
        <FormSelectField
          control={form.control}
          name="productId"
          id="rx-product"
          label="Product"
          options={[
            { value: "", label: "All products" },
            ...products.items.map((p) => ({ value: p.id, label: `${p.sku} · ${p.brand_name}` })),
          ]}
        />
        <Button type="submit" variant="outline">
          Show register
        </Button>
      </form>
      {filter.patientId && (
        <p className="mt-3 text-sm">
          Supplies to the registered patient {patientName}.{" "}
          <Button
            ref={showEvery}
            type="button"
            variant="ghost"
            onClick={() => setFilter({ ...filter, patientId: undefined })}
          >
            Show every patient
          </Button>
        </p>
      )}
      <Entries
        key={JSON.stringify(filter)}
        scope={scope}
        filter={filter}
        onPatient={(patientId, name) => {
          setPatientName(name);
          setFilter({ ...filter, patientId });
        }}
      />
    </Panel>
  );
}

const notRecorded = "not recorded";

function Entries({
  scope,
  filter,
  onPatient,
}: {
  scope: PharmacyScope;
  filter: PharmacyPrescriptionRegisterFilter;
  onPatient: (patientId: string, name: string) => void;
}) {
  const entries = useApiPagedQuery<PharmacyPrescriptionRegisterEntry>(
    pharmacyKey(scope, "prescription-register", filter),
    pharmacyPrescriptionRegisterPath(scope, filter),
  );
  return (
    <div className="mt-4 space-y-3">
      <RegisterExport
        label="Prescription register export"
        download={(signal) => fetchPharmacyPrescriptionRegisterCSV(scope, filter, signal)}
      />
      {entries.isError ? (
        <RequestFailure
          message="Could not load the prescription register."
          retry={entries.retry}
          error={entries.error}
        />
      ) : entries.isPending ? (
        <p role="status">Loading the register…</p>
      ) : entries.items.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          No supplies on prescription match, from {filter.from} to {filter.to}.
        </p>
      ) : (
        <table className="w-full text-sm" aria-label="Prescription register">
          <thead className="text-left text-muted-foreground">
            <tr>
              <th className="py-2">Serial</th>
              <th>Date</th>
              <th>Schedule</th>
              <th>Prescriber</th>
              <th>Patient</th>
              <th>Drug</th>
              <th>Manufacturer, batch</th>
              <th>Supplied</th>
              <th>Returned</th>
            </tr>
          </thead>
          <tbody>
            {entries.items.map((e) => (
              <tr key={e.id} className="border-t align-top">
                <td className="py-2">{e.serial}</td>
                <td>{e.supply_date}</td>
                <td>{e.regulatory_class}</td>
                <td>
                  {e.prescriber_name} ({e.prescriber_registration}),{" "}
                  {e.prescriber_address ?? `address ${notRecorded}`}
                </td>
                <td>
                  {e.patient_name}, {e.patient_address ?? `address ${notRecorded}`}
                  {e.patient_id && !filter.patientId && (
                    <Button
                      type="button"
                      variant="ghost"
                      className="block"
                      aria-label={`Supplies to ${e.patient_name} (${e.serial})`}
                      onClick={() => onPatient(e.patient_id!, e.patient_name)}
                    >
                      This patient&apos;s supplies
                    </Button>
                  )}
                </td>
                <td>
                  {e.brand_name} ({e.generic_name})
                </td>
                <td>
                  {e.manufacturer}, {e.lot_number}, exp. {e.expires_on}
                </td>
                <td>{e.quantity_units}</td>
                <td>{e.returned_units}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
      <LoadMore
        label="Load more entries"
        hasMore={entries.hasMore}
        isLoading={entries.isFetchingMore}
        onClick={entries.loadMore}
      />
    </div>
  );
}
