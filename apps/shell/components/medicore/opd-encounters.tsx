"use client";

import { useState } from "react";
import { z } from "zod";
import { Badge, Button } from "@tesserix/web";
import { Stethoscope } from "lucide-react";
import {
  ApiError,
  apiFetch,
  encounterMoves,
  newEncounterId,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type Encounter,
  type EncounterStatus,
  type Hospital,
  type Patient,
  type PatientDetail,
} from "@helivanta/api";
import {
  Field,
  FormSelectField,
  formatTime,
  LoadMore,
  Panel,
  RequestFailure,
  SelectControl,
  useZodForm,
} from "@helivanta/ui";
import { PatientSafety } from "./patient-safety";

const statusLabel: Record<EncounterStatus, string> = {
  arrived: "Arrived",
  in_consultation: "In consultation",
  closed: "Closed",
  left_without_being_seen: "Left without being seen",
  cancelled: "Cancelled",
};
const moveLabel: Record<EncounterStatus, string> = {
  arrived: "Arrived",
  in_consultation: "Start consultation",
  closed: "Close",
  left_without_being_seen: "Left without being seen",
  cancelled: "Cancel",
};
const needsReason = new Set<EncounterStatus>(["left_without_being_seen", "cancelled"]);

const inputClass =
  "w-full rounded-md border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";

/**
 * OPD encounters at one hospital (#80 slice 1): who has arrived, who is
 * being seen, and opening an encounter for a patient registered here.
 */
export function OpdEncounters() {
  const [hospitalId, setHospitalId] = useState("");
  const hospitals = useApiPagedQuery<Hospital>(["workforce", "hospitals"], "/workforce/hospitals");
  const hospital = hospitals.items.find((item) => item.id === hospitalId) ?? hospitals.items[0];
  if (hospitals.isError)
    return <RequestFailure message="Could not load your hospitals." retry={hospitals.retry} />;
  if (hospitals.isPending) return <p role="status">Loading hospitals…</p>;
  if (!hospital)
    return (
      <Panel>
        <h3 className="font-semibold">No hospital access yet</h3>
        <p className="mt-1 text-sm text-muted-foreground">
          Ask your administrator to configure your hospital directory.
        </p>
      </Panel>
    );
  return (
    <div className="space-y-6">
      {hospitals.items.length > 1 && (
        <Field id="opd-hospital" label="Hospital">
          <SelectControl
            id="opd-hospital"
            className="w-72"
            value={hospital.id}
            onValueChange={setHospitalId}
            options={hospitals.items.map((item) => ({ value: item.id, label: item.name }))}
          />
        </Field>
      )}
      <HospitalEncounters key={hospital.id} hospital={hospital} />
      <LoadMore
        label="Load more hospitals"
        hasMore={hospitals.hasMore}
        isLoading={hospitals.isFetchingMore}
        onClick={hospitals.loadMore}
      />
    </div>
  );
}

function HospitalEncounters({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const access = useApiQuery<{ granted: boolean }>(
    ["medicore", "access", hospital.id],
    `/medicore/hospitals/${hospital.id}/access`,
  );
  const [status, setStatus] = useState<EncounterStatus | "">("");
  const encounters = useApiPagedQuery<Encounter>(
    ["medicore", "encounters", hospital.id, status],
    `/medicore/hospitals/${hospital.id}/encounters${status ? `?status=${status}` : ""}`,
    { enabled: access.isSuccess },
  );
  if (access.isPending) return <p role="status">Checking your MediCore access…</p>;
  if (access.error instanceof ApiError && access.error.status === 404)
    return (
      <Panel>
        <h3 className="font-semibold">No MediCore access at {hospital.name}</h3>
        <p className="mt-1 text-sm text-muted-foreground">
          Ask your administrator to grant you MediCore access at this hospital.
        </p>
      </Panel>
    );
  if (access.isError)
    return (
      <RequestFailure
        message="Could not check your MediCore access."
        retry={() => void access.refetch()}
      />
    );
  return (
    <>
      {can("medicore.visit.create") && <OpenEncounter hospital={hospital} />}
      <Panel flush aria-labelledby="encounters-heading">
        <div className="flex flex-wrap items-end justify-between gap-4 border-b p-5">
          <div>
            <h3 id="encounters-heading" className="flex items-center gap-2 font-semibold">
              <Stethoscope className="size-5 text-(--hms-accent)" />
              OPD encounters at {hospital.name}
            </h3>
            <p className="mt-1 text-sm text-muted-foreground">
              Opening an encounter here does not yet create pharmacy or lab work.
            </p>
          </div>
          <Field id="encounter-status" label="Status">
            <SelectControl
              id="encounter-status"
              className="w-56"
              value={status}
              onValueChange={(value) => setStatus(value as EncounterStatus | "")}
              options={[
                { value: "", label: "All statuses" },
                ...(Object.keys(statusLabel) as EncounterStatus[]).map((value) => ({
                  value,
                  label: statusLabel[value],
                })),
              ]}
            />
          </Field>
        </div>
        {encounters.isError ? (
          <RequestFailure message="Could not load encounters." retry={encounters.retry} />
        ) : encounters.isPending ? (
          <p role="status" className="p-5">
            Loading encounters…
          </p>
        ) : encounters.items.length === 0 ? (
          <p className="p-5 text-sm text-muted-foreground">No encounters here yet.</p>
        ) : (
          <ul className="divide-y" aria-label="Encounters">
            {encounters.items.map((row) => (
              <EncounterRow key={row.id} hospital={hospital} row={row} />
            ))}
          </ul>
        )}
        <LoadMore
          label="Load more encounters"
          hasMore={encounters.hasMore}
          isLoading={encounters.isFetchingMore}
          onClick={encounters.loadMore}
        />
      </Panel>
    </>
  );
}

// The patient's name, read from the patient registry under the caller's own
// grant there; the encounter carries only the patient's id.
function PatientName({ hospital, id }: { hospital: Hospital; id: string }) {
  // Each read is an audited disclosure, so a name is read once per page
  // visit, not on every remount. shortcut: one request per row; a batched,
  // audited name lookup in patientmaster is #1378.
  const patient = useApiQuery<PatientDetail>(
    ["patients", "detail", hospital.id, id],
    `/patients/${id}?hospital_id=${hospital.id}`,
    { staleTime: 10 * 60_000, refetchOnWindowFocus: false },
  );
  if (patient.data)
    return (
      <>
        {[patient.data.given_name, patient.data.family_name].filter(Boolean).join(" ")}
        <span className="ml-2 text-sm font-normal text-muted-foreground">{patient.data.mrn}</span>
      </>
    );
  return <>{patient.isError ? "Patient record not available to you" : "Loading patient…"}</>;
}

function EncounterRow({ hospital, row }: { hospital: Hospital; row: Encounter }) {
  const { can } = usePermissions();
  const [reasonFor, setReasonFor] = useState<EncounterStatus | null>(null);
  const [safety, setSafety] = useState(false);
  const clinical = can("medicore.allergy.read") || can("medicore.observation.read");
  const reasonForm = useZodForm(reasonSchema, { reason: "" });
  const move = useApiMutation(
    ({ to, reason }: { to: EncounterStatus; reason?: string }) =>
      apiFetch<Encounter>(`/medicore/hospitals/${hospital.id}/encounters/${row.id}/transitions`, {
        method: "POST",
        body: JSON.stringify({
          to,
          expected_version: row.version,
          ...(reason ? { reason } : {}),
        }),
      }),
    {
      successToast: "Encounter updated",
      invalidate: [["medicore", "encounters", hospital.id]],
      invalidateOnError: true,
      onSuccess: () => {
        setReasonFor(null);
        reasonForm.reset({ reason: "" });
      },
    },
  );
  const moves = can("medicore.visit.update") ? encounterMoves[row.status] : [];
  return (
    <li className="flex flex-wrap items-center justify-between gap-3 px-5 py-3">
      <div className="min-w-0">
        <p className="wrap-anywhere font-medium">
          <PatientName hospital={hospital} id={row.patient_id} />
        </p>
        <p className="mt-0.5 text-sm text-muted-foreground">
          {row.consult_mode === "teleconsultation" ? "Teleconsultation" : "In person"} ·{" "}
          {row.visit_type === "first" ? "First visit" : "Follow-up"} · opened{" "}
          {formatTime(row.created_at)}
        </p>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="outline">{statusLabel[row.status]}</Badge>
        {clinical && (
          <Button
            variant="outline"
            size="sm"
            aria-expanded={safety}
            onClick={() => setSafety(!safety)}
          >
            Patient safety
          </Button>
        )}
        {moves.map((to) =>
          needsReason.has(to) ? (
            <Button
              key={to}
              variant="outline"
              size="sm"
              aria-expanded={reasonFor === to}
              disabled={move.isPending}
              onClick={() => setReasonFor(reasonFor === to ? null : to)}
            >
              {moveLabel[to]}
            </Button>
          ) : (
            <Button
              key={to}
              variant="outline"
              size="sm"
              disabled={move.isPending}
              onClick={() => move.mutate({ to })}
            >
              {moveLabel[to]}
            </Button>
          ),
        )}
      </div>
      {safety && (
        <PatientSafety hospital={hospital} patientId={row.patient_id} encounterId={row.id} />
      )}
      {reasonFor && (
        <form
          noValidate
          className="flex w-full flex-wrap items-end gap-2"
          onSubmit={reasonForm.handleSubmit(({ reason }) => move.mutate({ to: reasonFor, reason }))}
        >
          <Field
            id={`reason-${row.id}`}
            label={`Reason (${moveLabel[reasonFor].toLowerCase()})`}
            error={reasonForm.formState.errors.reason?.message}
          >
            <input
              id={`reason-${row.id}`}
              className={inputClass}
              {...reasonForm.register("reason")}
            />
          </Field>
          <Button type="submit" size="sm" disabled={move.isPending}>
            Confirm
          </Button>
        </form>
      )}
    </li>
  );
}

const reasonSchema = z.object({
  reason: z.string().trim().min(1, "Give a reason").max(500, "At most 500 characters"),
});
const searchSchema = z.object({
  query: z.string().trim().min(1, "Enter a name, MRN or mobile").max(100),
});

const openSchema = z.object({
  patient_id: z.string().min(1, "Choose a patient"),
  consult_mode: z.enum(["in_person", "teleconsultation"]),
  visit_type: z.enum(["first", "follow_up"]),
});

function OpenEncounter({ hospital }: { hospital: Hospital }) {
  // One id per encounter being opened, kept across retries, so a retry after
  // a lost response replays it instead of opening a second one (#80 E4).
  const [encounterId, setEncounterId] = useState(() => newEncounterId());
  const search = useZodForm(searchSchema, { query: "" });
  const [submitted, setSubmitted] = useState("");
  const patients = useApiPagedQuery<Patient>(
    ["patients", "registry", hospital.id, "opd", submitted],
    `/patients/search?hospital_id=${hospital.id}`,
    {
      request: { method: "POST", body: JSON.stringify({ query: submitted }) },
      enabled: submitted !== "",
    },
  );
  const form = useZodForm(openSchema, {
    patient_id: "",
    consult_mode: "in_person",
    visit_type: "first",
  });
  const open = useApiMutation(
    (values: z.infer<typeof openSchema>) =>
      apiFetch<Encounter>(`/medicore/hospitals/${hospital.id}/encounters/${encounterId}`, {
        method: "PUT",
        body: JSON.stringify(values),
      }),
    {
      successToast: "Encounter opened",
      invalidate: [["medicore", "encounters", hospital.id]],
      onSuccess: () => {
        form.reset({ patient_id: "", consult_mode: "in_person", visit_type: "first" });
        setEncounterId(newEncounterId());
      },
    },
  );
  return (
    <Panel aria-labelledby="open-encounter-heading" className="space-y-4">
      <h3 id="open-encounter-heading" className="font-semibold">
        Open an encounter
      </h3>
      <form
        noValidate
        role="search"
        className="flex flex-wrap items-end gap-2"
        onSubmit={search.handleSubmit(({ query }) => setSubmitted(query))}
      >
        <Field
          id="opd-patient-search"
          label="Find a patient registered here"
          error={search.formState.errors.query?.message}
        >
          <input
            id="opd-patient-search"
            className={`${inputClass} w-72`}
            placeholder="Name, MRN or mobile"
            {...search.register("query")}
          />
        </Field>
        <Button type="submit" variant="outline">
          Search
        </Button>
      </form>
      {patients.isError && (
        <RequestFailure
          message="Could not search patients. You may need patient-registry access at this hospital."
          retry={patients.retry}
        />
      )}
      <form
        noValidate
        className="grid gap-4 sm:grid-cols-3"
        onSubmit={form.handleSubmit((values) => open.mutate(values))}
      >
        <FormSelectField
          control={form.control}
          name="patient_id"
          id="opd-patient"
          label="Patient"
          disabled={!patients.items.length}
          options={patients.items.map((p) => ({
            value: p.id,
            label: `${[p.given_name, p.family_name].filter(Boolean).join(" ")} · ${p.mrn}`,
          }))}
        />
        <FormSelectField
          control={form.control}
          name="consult_mode"
          id="opd-consult-mode"
          label="Consultation"
          options={[
            { value: "in_person", label: "In person" },
            { value: "teleconsultation", label: "Teleconsultation" },
          ]}
        />
        <FormSelectField
          control={form.control}
          name="visit_type"
          id="opd-visit-type"
          label="Visit"
          options={[
            { value: "first", label: "First visit" },
            { value: "follow_up", label: "Follow-up" },
          ]}
        />
        <div className="sm:col-span-3">
          <Button type="submit" disabled={open.isPending}>
            {open.isPending ? "Opening…" : "Open encounter"}
          </Button>
        </div>
      </form>
    </Panel>
  );
}
