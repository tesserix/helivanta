"use client";

import { useState } from "react";
import { z } from "zod";
import { Button } from "@tesserix/web";
import { Search, UserRoundPlus, ShieldCheck, UsersRound } from "lucide-react";
import {
  apiFetch,
  useApiQuery,
  useApiPagedQuery,
  useApiMutation,
  usePermissions,
  patientRegistrationSchema,
  type Hospital,
  type Patient,
  type PatientRegistrationInput,
  type PatientRegistrationResult,
} from "@helivanta/api";
import { Field, LoadMore, useZodForm } from "@helivanta/ui";

const inputClass =
  "w-full rounded-lg border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";
const searchSchema = z.object({ query: z.string().trim().max(100) });

export function PatientWorkspace() {
  const [hospitalId, setHospitalId] = useState("");
  const hospitals = useApiPagedQuery<Hospital>(["workforce", "hospitals"], "/workforce/hospitals");
  const hospital = hospitals.items.find((item) => item.id === hospitalId) ?? hospitals.items[0];
  return (
    <>
      <header className="mb-8 flex flex-wrap items-end justify-between gap-5">
        <div>
          <p className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-(--hms-accent)">
            Patient services
          </p>
          <h2 className="text-3xl font-semibold tracking-tight">
            Every patient. A clear beginning.
          </h2>
          <p className="mt-2 max-w-xl text-sm leading-6 text-muted-foreground">
            Find a patient record or register someone new, with the right hospital context at every
            step.
          </p>
        </div>
        {hospital && (
          <Field id="patient-hospital" label="Hospital">
            <select
              id="patient-hospital"
              className={inputClass}
              value={hospital.id}
              onChange={(event) => setHospitalId(event.target.value)}
            >
              {hospitals.items.map((item) => (
                <option key={item.id} value={item.id}>
                  {item.name}
                </option>
              ))}
            </select>
          </Field>
        )}
      </header>
      {hospitals.isError ? (
        <Failure message="Could not load your hospitals." retry={hospitals.retry} />
      ) : hospitals.isPending ? (
        <p role="status">Loading hospitals…</p>
      ) : hospital ? (
        <HospitalPatients key={hospital.id} hospital={hospital} />
      ) : (
        <section className="rounded-2xl border bg-card p-10 text-center">
          <UsersRound className="mx-auto mb-4 size-8 text-muted-foreground" />
          <h3 className="font-semibold">No hospital access yet</h3>
          <p className="mt-2 text-sm text-muted-foreground">
            Ask your administrator to configure your hospital directory and clinical access.
          </p>
        </section>
      )}
      <LoadMore
        hasMore={hospitals.hasMore}
        isLoading={hospitals.isFetchingMore}
        onClick={hospitals.loadMore}
      />
    </>
  );
}

function Failure({ message, retry }: { message: string; retry: () => void }) {
  return (
    <div role="alert" className="rounded-xl border bg-card p-6">
      <p className="mb-3 text-sm">{message}</p>
      <Button variant="outline" onClick={retry}>
        Try again
      </Button>
    </div>
  );
}

function HospitalPatients({ hospital }: { hospital: Hospital }) {
  const access = useApiQuery<{ level: "reader" | "registrar" }>(
    ["patients", "access", hospital.id],
    `/patients/hospitals/${hospital.id}/access`,
  );
  const { can } = usePermissions();
  return (
    <>
      <div className="mb-6 flex items-center gap-3 rounded-xl border bg-card px-5 py-4 text-sm">
        <ShieldCheck className="size-5 shrink-0 text-(--hms-accent)" />
        <p>
          <span className="font-medium">Hospital-specific records</span>
          <span className="ml-2 text-muted-foreground">
            Patient information stays within {hospital.name}.
          </span>
        </p>
      </div>
      {access.isPending ? (
        <p role="status">Checking clinical access…</p>
      ) : access.isError ? (
        <Failure
          message="Clinical access is not available. Ask your administrator for a patient-registry grant, or retry if access was just updated."
          retry={() => void access.refetch()}
        />
      ) : (
        <Registry
          hospital={hospital}
          writable={access.data?.level === "registrar" && can("patient.register")}
        />
      )}
      {can("patient.access.manage") && <ClinicalAccessSetup hospital={hospital} />}
    </>
  );
}

function Registry({ hospital, writable }: { hospital: Hospital; writable: boolean }) {
  const [query, setQuery] = useState("");
  const [registering, setRegistering] = useState(false);
  const [selected, setSelected] = useState<string | null>(null);
  const [receipt, setReceipt] = useState<PatientRegistrationResult | null>(null);
  const form = useZodForm(searchSchema, { query: "" });
  const patients = useApiPagedQuery<Patient>(
    ["patients", "registry", hospital.id],
    `/patients/search?hospital_id=${hospital.id}`,
    { request: { method: "POST", body: JSON.stringify({ query }) } },
  );
  return (
    <>
      <section className="overflow-hidden rounded-2xl border bg-card shadow-sm">
        <div className="flex flex-wrap items-center justify-between gap-4 border-b p-6">
          <div>
            <h3 className="text-lg font-semibold">Patient registry</h3>
            <p className="mt-1 text-sm text-muted-foreground">
              Search before registering to help prevent duplicate records.
            </p>
          </div>
          {writable && (
            <Button
              onClick={() => {
                setRegistering(true);
                setReceipt(null);
                setSelected(null);
              }}
            >
              <UserRoundPlus className="mr-2 size-4" />
              Register patient
            </Button>
          )}
        </div>
        <form
          noValidate
          onSubmit={form.handleSubmit((value) => {
            setQuery(value.query);
            setSelected(null);
            setReceipt(null);
          })}
          className="flex flex-wrap items-end gap-3 border-b bg-muted/20 p-5"
        >
          <div className="min-w-48 flex-1">
            <Field
              id="patient-search"
              label="Find a patient"
              error={form.formState.errors.query?.message}
            >
              <input
                id="patient-search"
                className={inputClass}
                placeholder="Name prefix, exact MRN or phone number"
                autoComplete="off"
                {...form.register("query")}
              />
            </Field>
          </div>
          <Button type="submit" variant="outline">
            <Search className="mr-2 size-4" />
            Search patients
          </Button>
        </form>
        {patients.isError ? (
          <div className="p-5">
            <Failure
              message="Could not load patients. No records are shown until access can be verified."
              retry={patients.retry}
            />
          </div>
        ) : patients.isPending ? (
          <p className="p-8" role="status">
            Loading patients…
          </p>
        ) : patients.items.length === 0 ? (
          <div className="px-6 py-14 text-center">
            <UsersRound className="mx-auto mb-4 size-8 text-muted-foreground" />
            <h4 className="font-semibold">
              {query ? "No matching patients" : "Your patient registry starts here"}
            </h4>
            <p className="mt-2 text-sm text-muted-foreground">
              {query
                ? "Check the spelling, MRN or phone number before registering a new patient."
                : "Registered patients will appear here, with a unique medical record number."}
            </p>
          </div>
        ) : (
          <div className="relative overflow-x-auto">
            <table className="w-full text-left text-sm">
              <caption className="sr-only">Patients at {hospital.name}</caption>
              <thead className="bg-muted/30 text-xs text-muted-foreground">
                <tr>
                  {["Patient", "Medical record number", "Date of birth", "Contact", ""].map(
                    (label) => (
                      <th key={label} scope="col" className="px-6 py-3 font-medium">
                        {label || <span className="sr-only">Actions</span>}
                      </th>
                    ),
                  )}
                </tr>
              </thead>
              <tbody className="divide-y">
                {patients.items.map((patient) => (
                  <tr key={patient.id} className="hover:bg-muted/20">
                    <td className="px-6 py-4 font-medium">
                      {patient.given_name} {patient.family_name}
                    </td>
                    <td className="px-6 py-4 font-mono text-xs">{patient.mrn}</td>
                    <td className="px-6 py-4 text-muted-foreground">
                      {patient.dob}
                      {patient.dob_estimated ? " · Estimated" : ""}
                    </td>
                    <td className="px-6 py-4 text-muted-foreground">
                      {patient.mobile || "Not recorded"}
                    </td>
                    <td className="px-6 py-4">
                      <Button
                        size="sm"
                        variant="ghost"
                        aria-label={`Open ${patient.given_name} ${patient.family_name}`}
                        onClick={() => {
                          setSelected(patient.id);
                          setRegistering(false);
                          setReceipt(null);
                        }}
                      >
                        Open record
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <div className="border-t px-6 py-4 text-xs text-muted-foreground">
          {patients.items.length} records loaded ·{" "}
          {writable ? "Registration access" : "Read-only access"}
          <LoadMore
            hasMore={patients.hasMore}
            isLoading={patients.isFetchingMore}
            onClick={patients.loadMore}
          />
        </div>
      </section>
      {registering && (
        <Registration
          hospital={hospital}
          onCancel={() => setRegistering(false)}
          onSaved={(result) => {
            setReceipt(result);
            setRegistering(false);
          }}
        />
      )}
      {receipt && (
        <section role="status" className="mt-6 rounded-xl border bg-card p-6">
          <h3 className="font-semibold">Registration saved</h3>
          <p className="mt-2 text-sm">
            {receipt.patient.given_name} {receipt.patient.family_name} has been registered at{" "}
            {hospital.name}.
          </p>
          {receipt.possible_matches.length > 0 && (
            <p className="mt-3 text-sm text-muted-foreground">
              Possible similar records were found. Review the registry before creating any further
              registration for this patient.
            </p>
          )}
        </section>
      )}
      {selected && (
        <PatientDetail
          key={selected}
          hospital={hospital}
          id={selected}
          onClose={() => setSelected(null)}
        />
      )}
    </>
  );
}

function PatientDetail({
  hospital,
  id,
  onClose,
}: {
  hospital: Hospital;
  id: string;
  onClose: () => void;
}) {
  const record = useApiQuery<Patient>(
    ["patients", "detail", hospital.id, id],
    `/patients/${id}?hospital_id=${hospital.id}`,
  );
  return (
    <section className="mt-6 rounded-2xl border bg-card p-6">
      <div className="mb-5 flex justify-between gap-4">
        <h3 className="text-lg font-semibold">Patient record</h3>
        <Button variant="ghost" onClick={onClose}>
          Close record
        </Button>
      </div>
      {record.isPending ? (
        <p role="status">Loading patient…</p>
      ) : record.isError ? (
        <Failure
          message="This patient record could not be opened. Check hospital access and try again."
          retry={() => void record.refetch()}
        />
      ) : (
        record.data && (
          <dl className="grid gap-5 sm:grid-cols-2">
            {Object.entries({
              Name: `${record.data.given_name} ${record.data.family_name}`,
              MRN: record.data.mrn,
              "Date of birth": `${record.data.dob}${record.data.dob_estimated ? " (estimated)" : ""}`,
              "Sex as recorded": record.data.sex || "Not recorded",
              Phone: record.data.mobile || "Not recorded",
              Address: record.data.address_line || "Not recorded",
            }).map(([label, value]) => (
              <div key={label}>
                <dt className="text-xs text-muted-foreground">{label}</dt>
                <dd className="mt-1 whitespace-pre-wrap text-sm font-medium">{value}</dd>
              </div>
            ))}
          </dl>
        )
      )}
    </section>
  );
}

function Registration({
  hospital,
  onCancel,
  onSaved,
}: {
  hospital: Hospital;
  onCancel: () => void;
  onSaved: (result: PatientRegistrationResult) => void;
}) {
  const [requestId] = useState(() => crypto.randomUUID());
  const form = useZodForm(patientRegistrationSchema, {
    given_name: "",
    family_name: "",
    dob: "",
    dob_estimated: false,
    sex: "",
    mobile: "",
    address_line: "",
    notice_version: "",
    consented_by: "patient",
    guardian_name: "",
    guardian_relationship: "",
    consent_confirmed: false,
  });
  const save = useApiMutation(
    (value: PatientRegistrationInput) => {
      const { consent_confirmed: confirmed, ...input } = value;
      if (!confirmed) throw new Error("Record consent before registration.");
      return apiFetch<PatientRegistrationResult>(`/patients?hospital_id=${hospital.id}`, {
        method: "POST",
        body: JSON.stringify({ ...input, request_id: requestId }),
      });
    },
    {
      invalidate: [["patients", "registry", hospital.id]],
      suppressErrorToast: true,
      successToast: `Registration saved at ${hospital.name}`,
      onSuccess: onSaved,
    },
  );
  return (
    <section aria-labelledby="registration-heading" className="mt-6 rounded-2xl border bg-card p-6">
      <div className="mb-6 flex justify-between gap-4">
        <div>
          <h3 id="registration-heading" className="text-xl font-semibold">
            Register a patient
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            {hospital.name} · A medical record number is assigned when you save.
          </p>
        </div>
        <Button variant="ghost" disabled={save.isPending} onClick={onCancel}>
          Cancel registration
        </Button>
      </div>
      <form
        noValidate
        onSubmit={form.handleSubmit((value) => save.mutate(value))}
        className="grid gap-5 sm:grid-cols-2"
      >
        {(
          [
            ["given_name", "Given name"],
            ["family_name", "Family name"],
            ["dob", "Date of birth"],
            ["mobile", "Phone number"],
            ["address_line", "Address"],
            ["notice_version", "Consent notice version"],
          ] as const
        ).map(([name, label]) => (
          <Field
            key={name}
            id={`registration-${name}`}
            label={label}
            error={form.formState.errors[name]?.message}
          >
            <input
              id={`registration-${name}`}
              className={inputClass}
              type={name === "dob" ? "date" : name === "mobile" ? "tel" : "text"}
              autoComplete="off"
              {...form.register(name)}
            />
          </Field>
        ))}
        <Field
          id="registration-sex"
          label="Sex as recorded"
          error={form.formState.errors.sex?.message}
        >
          <select id="registration-sex" className={inputClass} {...form.register("sex")}>
            <option value="">Not recorded</option>
            <option value="female">Female</option>
            <option value="male">Male</option>
            <option value="other">Other</option>
          </select>
        </Field>
        <Field
          id="registration-consented-by"
          label="Consent recorded from"
          error={form.formState.errors.consented_by?.message}
        >
          <select
            id="registration-consented-by"
            className={inputClass}
            {...form.register("consented_by")}
          >
            <option value="patient">Patient</option>
            <option value="guardian">Guardian</option>
          </select>
        </Field>
        {form.watch("consented_by") === "guardian" &&
          (
            [
              ["guardian_name", "Guardian name"],
              ["guardian_relationship", "Guardian relationship"],
            ] as const
          ).map(([name, label]) => (
            <Field
              key={name}
              id={`registration-${name}`}
              label={label}
              error={form.formState.errors[name]?.message}
            >
              <input
                id={`registration-${name}`}
                className={inputClass}
                autoComplete="off"
                {...form.register(name)}
              />
            </Field>
          ))}
        <label className="flex items-center gap-3 text-sm sm:col-span-2">
          <input type="checkbox" {...form.register("dob_estimated")} />
          Date of birth is estimated
        </label>
        <div className="border-t pt-5 sm:col-span-2">
          <label className="flex items-start gap-3 text-sm">
            <input type="checkbox" className="mt-1" {...form.register("consent_confirmed")} />I have
            recorded the patient's or guardian's consent
          </label>
          {form.formState.errors.consent_confirmed && (
            <p role="alert" className="mt-2 text-sm text-destructive">
              {form.formState.errors.consent_confirmed.message}
            </p>
          )}
          <p className="mt-3 text-xs leading-5 text-muted-foreground">
            Record the notice actually presented. This receipt does not verify guardian identity or
            enable sharing with another hospital.
          </p>
        </div>
        {save.isError && (
          <p role="alert" className="text-sm text-destructive sm:col-span-2">
            {save.error.message}. If a duplicate exists, cancel and open the existing record. After
            a connection failure, retry this registration without changing the details.
          </p>
        )}
        <div className="flex justify-end border-t pt-5 sm:col-span-2">
          <Button type="submit" disabled={save.isPending}>
            {save.isPending ? "Saving…" : "Save registration"}
          </Button>
        </div>
      </form>
    </section>
  );
}

const accessSchema = z.object({
  confirmed: z.boolean().refine((value) => value, "Confirm the clinical access grant."),
});
function ClinicalAccessSetup({ hospital }: { hospital: Hospital }) {
  const identity = useApiQuery<{ subject: string }>(
    ["iam", "me", "permissions"],
    "/iam/me/permissions",
  );
  const form = useZodForm(accessSchema, { confirmed: false });
  const grant = useApiMutation(
    async () => {
      if (!identity.data?.subject)
        throw new Error("Could not verify your account. Reload and retry.");
      return apiFetch(
        `/patients/hospitals/${hospital.id}/access/${encodeURIComponent(identity.data.subject)}`,
        { method: "PUT", body: JSON.stringify({ level: "registrar" }) },
      );
    },
    {
      invalidate: [["patients", "access", hospital.id]],
      successToast: "Clinical access granted",
      suppressErrorToast: true,
    },
  );
  return (
    <details className="mt-6 rounded-xl border bg-card p-5">
      <summary className="cursor-pointer text-sm font-medium">
        Clinical access administration
      </summary>
      <form
        noValidate
        className="mt-4 space-y-4"
        onSubmit={form.handleSubmit(() => grant.mutate())}
      >
        <p className="text-sm text-muted-foreground">
          Staff-directory access does not grant patient access. This action grants your signed-in
          account patient registration access at {hospital.name}.
        </p>
        <label className="flex items-start gap-3 text-sm">
          <input type="checkbox" className="mt-1" {...form.register("confirmed")} />
          Grant my account clinical registration access to this hospital
        </label>
        {form.formState.errors.confirmed && (
          <p role="alert" className="text-sm text-destructive">
            {form.formState.errors.confirmed.message}
          </p>
        )}
        {grant.isError && (
          <p role="alert" className="text-sm text-destructive">
            {grant.error.message}
          </p>
        )}
        <Button type="submit" disabled={grant.isPending}>
          Grant clinical access
        </Button>
      </form>
    </details>
  );
}
