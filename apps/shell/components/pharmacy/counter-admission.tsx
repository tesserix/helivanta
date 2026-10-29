"use client";
import { useEffect, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  admissionResolveSchema,
  apiFetch,
  useApiMutation,
  useApiPagedQuery,
  type AdmissionStay,
  type PharmacyHospitalAffiliation,
  type PharmacyPatientLookupResult,
  type PharmacyScope,
  pharmacyRoot,
} from "@helivanta/api";
import { Field, FormSelectField, RequestFailure, useZodForm } from "@helivanta/ui";

export type CounterAdmission = {
  occupancy_id: string;
  admission_id: string;
  hospital_id: string;
  bed_code: string;
  name: string;
};

const sexLabel = { "": "", male: "male", female: "female", other: "other" } as const;

/**
 * Bills a sale to the admission in a bed (#1387): a hospital that has
 * verified this pharmacy, a bed or room code, the one open stay it resolves
 * to, and the patient confirmed by the audited lookup before anything posts.
 */
export function CounterAdmissionLookup({
  scope,
  value,
  onChange,
}: {
  scope: PharmacyScope;
  value: CounterAdmission | null;
  onChange: (admission: CounterAdmission | null) => void;
}) {
  const links = useApiPagedQuery<PharmacyHospitalAffiliation>(
    ["pharmacy", scope.tenantId, scope.pharmacyId, "affiliations"],
    `/pharmacy/pharmacies/${scope.pharmacyId}/hospital-affiliations`,
  );
  const { hasMore, isFetchingMore, loadMore } = links;
  useEffect(() => {
    if (hasMore && !isFetchingMore) loadMore();
  }, [hasMore, isFetchingMore, loadMore]);
  const hospitals = [
    ...new Set(
      links.items.filter((l) => l.verification_status === "verified").map((l) => l.hospital_id),
    ),
  ];
  if (value)
    return (
      <div className="rounded-md border p-3 text-sm">
        <p>
          Billed to the admission of <span className="font-medium">{value.name}</span>, bed{" "}
          {value.bed_code}
        </p>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="mt-2"
          onClick={() => onChange(null)}
        >
          Change bed
        </Button>
      </div>
    );
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
        No hospital has verified this pharmacy yet, so no sale can be billed to an admission.
      </p>
    );
  return <ResolveForm scope={scope} hospitals={hospitals} onChange={onChange} />;
}

function ResolveForm({
  scope,
  hospitals,
  onChange,
}: {
  scope: PharmacyScope;
  hospitals: string[];
  onChange: (admission: CounterAdmission | null) => void;
}) {
  const form = useZodForm(admissionResolveSchema, {
    hospital_id: hospitals.length === 1 ? hospitals[0] : "",
    by: "bed",
    code: "",
  });
  const [found, setFound] = useState<{
    stay: AdmissionStay;
    patient: PharmacyPatientLookupResult;
  } | null>(null);
  const { watch } = form;
  useEffect(() => {
    // A result belongs to the hospital and code it was found for.
    const subscription = watch(() => setFound(null));
    return () => subscription.unsubscribe();
  }, [watch]);
  const resolve = useApiMutation(
    async (values: z.infer<typeof admissionResolveSchema>) => {
      const query = new URLSearchParams({
        hospital_id: values.hospital_id,
        [`${values.by}_code`]: values.code,
      });
      const stay = await apiFetch<AdmissionStay>(
        `${pharmacyRoot(scope)}/admission-occupant?${query.toString()}`,
      );
      const who = new URLSearchParams({
        hospital_id: stay.hospital_id,
        pharmacy_id: scope.pharmacyId,
        patient_id: stay.patient_id,
      });
      const patient = await apiFetch<PharmacyPatientLookupResult>(
        `/patients/pharmacy-lookup?${who.toString()}`,
      );
      return { stay, patient };
    },
    { onSuccess: setFound, suppressErrorToast: true },
  );
  const find = () =>
    !resolve.isPending &&
    void form.handleSubmit((values) => {
      setFound(null);
      resolve.mutate(values);
    })();
  const failure = resolve.error instanceof ApiError ? resolve.error : null;
  const message =
    failure?.code === "ambiguous"
      ? "More than one bed in that room is occupied. Give the bed."
      : failure?.code === "no_occupant"
        ? "No one is in that bed or room now."
        : failure?.status === 404
          ? "The patient could not be confirmed. Ask the hospital for patient lookup access."
          : resolve.error?.message;
  return (
    <div className="space-y-3">
      {/* Not a form: it sits inside the sale's form. */}
      <div
        role="group"
        aria-label="Find the admission"
        className="grid items-end gap-4 sm:grid-cols-4"
      >
        <FormSelectField
          control={form.control}
          name="hospital_id"
          id="admission-hospital"
          label="Hospital"
          options={hospitals.map((h) => ({ value: h, label: h }))}
        />
        <FormSelectField
          control={form.control}
          name="by"
          id="admission-by"
          label="By"
          options={[
            { value: "bed", label: "Bed" },
            { value: "room", label: "Room" },
          ]}
        />
        <Field id="admission-code" label="Code" error={form.formState.errors.code?.message}>
          <Input
            id="admission-code"
            maxLength={20}
            autoComplete="off"
            {...form.register("code")}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                find();
              }
            }}
          />
        </Field>
        <Button type="button" variant="outline" disabled={resolve.isPending} onClick={find}>
          {resolve.isPending ? "Finding…" : "Find admission"}
        </Button>
      </div>
      {resolve.isError && (
        <p role="alert" className="text-sm text-destructive">
          {message}
        </p>
      )}
      {found && (
        <div className="rounded-md border p-3 text-sm">
          <p className="font-medium">{found.patient.name}</p>
          <p className="text-muted-foreground">
            {found.patient.age_estimated ? "About " : ""}
            {found.patient.age_years} years
            {found.patient.sex ? ` · ${sexLabel[found.patient.sex]}` : ""} · Ward{" "}
            {found.stay.ward_code}, room {found.stay.room_code}, bed {found.stay.bed_code}
          </p>
          <Button
            type="button"
            size="sm"
            className="mt-2"
            onClick={() =>
              onChange({
                occupancy_id: found.stay.occupancy_id,
                admission_id: found.stay.admission_id,
                hospital_id: found.stay.hospital_id,
                bed_code: found.stay.bed_code,
                name: found.patient.name,
              })
            }
          >
            Bill this admission
          </Button>
        </div>
      )}
    </div>
  );
}
