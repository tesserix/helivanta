"use client";
import { useEffect, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  pharmacyPatientLookupSchema,
  useApiMutation,
  useApiPagedQuery,
  type PharmacyHospitalAffiliation,
  type PharmacyPatientLookupResult,
  type PharmacyScope,
} from "@helivanta/api";
import { Field, FormSelectField, RequestFailure, useZodForm } from "@helivanta/ui";

export type CounterPatient = { patient_id: string; hospital_id: string; name: string };

const sexLabel = { "": "", male: "male", female: "female", other: "other" } as const;

/**
 * Finds a registered patient for the sale (#1386 B-2c): a hospital that has
 * verified this pharmacy, then the exact MRN through the audited lookup. Only
 * a patient the pharmacist confirms is handed to the sale.
 */
export function CounterPatientLookup({
  scope,
  value,
  onChange,
}: {
  scope: PharmacyScope;
  value: CounterPatient | null;
  onChange: (patient: CounterPatient | null) => void;
}) {
  const links = useApiPagedQuery<PharmacyHospitalAffiliation>(
    ["pharmacy", scope.tenantId, scope.pharmacyId, "affiliations"],
    `/pharmacy/pharmacies/${scope.pharmacyId}/hospital-affiliations`,
  );
  // The picker needs every verified hospital, not the first page's.
  const { hasMore, isFetchingMore, loadMore } = links;
  useEffect(() => {
    if (hasMore && !isFetchingMore) loadMore();
  }, [hasMore, isFetchingMore, loadMore]);
  const verified = links.items.filter((l) => l.verification_status === "verified");
  const hospitals = [...new Set(verified.map((l) => l.hospital_id))];

  if (value) {
    return (
      <div className="rounded-md border p-3 text-sm">
        <p>
          Registered patient <span className="font-medium">{value.name}</span>
        </p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="mt-2"
          onClick={() => onChange(null)}
        >
          Change patient
        </Button>
      </div>
    );
  }
  if (links.isError)
    return (
      <RequestFailure
        message="Could not check which hospitals have verified this pharmacy."
        retry={links.retry}
      />
    );
  if (links.isPending || links.hasMore) return <p role="status">Checking verified hospitals…</p>;
  if (hospitals.length === 0)
    return (
      <p className="text-sm text-muted-foreground">
        No hospital has verified this pharmacy yet, so registered patients cannot be named on a
        sale. Record a walk-in buyer instead.
      </p>
    );
  // Mounted once the hospitals are known, so a single one is preselected.
  return <LookupForm scope={scope} hospitals={hospitals} onChange={onChange} />;
}

function LookupForm({
  scope,
  hospitals,
  onChange,
}: {
  scope: PharmacyScope;
  hospitals: string[];
  onChange: (patient: CounterPatient | null) => void;
}) {
  const form = useZodForm(pharmacyPatientLookupSchema, {
    hospital_id: hospitals.length === 1 ? hospitals[0] : "",
    mrn: "",
  });
  const [found, setFound] = useState<
    (PharmacyPatientLookupResult & { hospital_id: string }) | null
  >(null);
  // A result belongs to the hospital and MRN it was found for: editing either
  // withdraws it, so the pharmacist can only confirm what the fields say.
  const { watch } = form;
  useEffect(() => {
    const subscription = watch(() => setFound(null));
    return () => subscription.unsubscribe();
  }, [watch]);
  const lookup = useApiMutation(
    async (values: z.infer<typeof pharmacyPatientLookupSchema>) => {
      const query = new URLSearchParams({
        hospital_id: values.hospital_id,
        pharmacy_id: scope.pharmacyId,
        mrn: values.mrn,
      });
      const result = await apiFetch<PharmacyPatientLookupResult>(
        `/patients/pharmacy-lookup?${query.toString()}`,
      );
      return { ...result, hospital_id: values.hospital_id };
    },
    { onSuccess: setFound, suppressErrorToast: true },
  );
  const find = () =>
    // One lookup at a time: each is audited, and a slow answer must not
    // overwrite the one asked for after it.
    !lookup.isPending &&
    void form.handleSubmit((values) => {
      setFound(null);
      lookup.mutate(values);
    })();
  // Every refusal is the same 404, so the counter cannot say which check failed.
  const notFound = lookup.error instanceof ApiError && lookup.error.status === 404;

  return (
    <div className="space-y-3">
      {/* Not a form: it sits inside the sale's form, and Enter here must
          look the patient up, never post the sale. */}
      <div
        role="group"
        aria-label="Find a registered patient"
        className="grid items-end gap-4 sm:grid-cols-3"
      >
        <FormSelectField
          control={form.control}
          name="hospital_id"
          id="patient-hospital"
          label="Hospital"
          options={hospitals.map((h) => ({ value: h, label: h }))}
        />
        <Field id="patient-mrn" label="MRN" error={form.formState.errors.mrn?.message}>
          <Input
            id="patient-mrn"
            maxLength={64}
            autoComplete="off"
            {...form.register("mrn")}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                find();
              }
            }}
          />
        </Field>
        <Button type="button" variant="outline" disabled={lookup.isPending} onClick={find}>
          {lookup.isPending ? "Finding…" : "Find patient"}
        </Button>
      </div>
      {notFound && (
        <p role="alert" className="text-sm text-destructive">
          No patient found. Check the MRN; if it is right, ask the hospital for patient lookup
          access.
        </p>
      )}
      {lookup.isError && !notFound && (
        <p role="alert" className="text-sm text-destructive">
          {lookup.error.message}
        </p>
      )}
      {found && (
        <div className="rounded-md border p-3 text-sm">
          <p className="font-medium">{found.name}</p>
          <p className="text-muted-foreground">
            {found.age_estimated ? "About " : ""}
            {found.age_years} years{found.sex ? ` · ${sexLabel[found.sex]}` : ""}
          </p>
          <Button
            type="button"
            size="sm"
            className="mt-2"
            onClick={() =>
              onChange({
                patient_id: found.patient_id,
                hospital_id: found.hospital_id,
                name: found.name,
              })
            }
          >
            This is the patient
          </Button>
        </div>
      )}
    </div>
  );
}
