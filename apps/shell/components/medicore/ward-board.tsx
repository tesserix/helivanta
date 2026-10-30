"use client";

import { useMemo, useState } from "react";
import { z } from "zod";
import { Badge, Button } from "@tesserix/web";
import {
  ApiError,
  admitSchema,
  apiFetch,
  bedClassLabel,
  bedReasonSchema,
  newBedId,
  releaseReasonLabel,
  releaseSchema,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type Admission,
  type Bed,
  type BedState,
  type Hospital,
  type Patient,
  type ReleaseReason,
} from "@helivanta/api";
import {
  ConfirmDialog,
  Field,
  FormSelectField,
  LoadMore,
  Panel,
  RequestFailure,
  SelectControl,
  useZodForm,
} from "@helivanta/ui";
import { PatientName } from "./opd-encounters";
import { BedSetup } from "./bed-setup";
import { AdmissionBalance, BillingAccessSetup, useBillingAccess } from "./admission-account";
import { BedChargingSetup } from "./bed-charging";
import { InterimBills } from "./interim-bills";

const stateLabel: Record<BedState, string> = {
  available: "Available",
  occupied: "Occupied",
  cleaning: "Cleaning",
  out_of_service: "Out of service",
};

const inputClass =
  "w-full rounded-md border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";

/**
 * The inpatient ward board (#106 slice 1): every bed at one hospital, who is
 * in it, and admitting, transferring and releasing patients.
 */
export function WardBoard() {
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
        <Field id="ipd-hospital" label="Hospital">
          <SelectControl
            id="ipd-hospital"
            className="w-72"
            value={hospital.id}
            onValueChange={setHospitalId}
            options={hospitals.items.map((item) => ({ value: item.id, label: item.name }))}
          />
        </Field>
      )}
      <HospitalBeds key={hospital.id} hospital={hospital} />
      <LoadMore
        label="Load more hospitals"
        hasMore={hospitals.hasMore}
        isLoading={hospitals.isFetchingMore}
        onClick={hospitals.loadMore}
      />
    </div>
  );
}

function HospitalBeds({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const billing = useBillingAccess(hospital);
  const access = useApiQuery<{ granted: boolean }>(
    ["medicore", "access", hospital.id],
    `/medicore/hospitals/${hospital.id}/access`,
  );
  const beds = useApiPagedQuery<Bed>(
    ["medicore", "beds", hospital.id],
    `/medicore/hospitals/${hospital.id}/beds?limit=200`,
    { enabled: access.isSuccess },
  );
  if (access.isPending) return <p role="status">Checking MediCore access…</p>;
  if (access.isError)
    return (
      <Panel>
        <h3 className="font-semibold">No MediCore access at {hospital.name}</h3>
        <p className="mt-1 text-sm text-muted-foreground">
          Ask your administrator to grant you MediCore access at this hospital.
        </p>
      </Panel>
    );
  const manage = can("medicore.admission.manage");
  const available = beds.items.filter((b) => b.state === "available");
  // Group by ward, then room, in the order the beds were set up.
  const wards = new Map<string, Map<string, Bed[]>>();
  for (const bed of beds.items) {
    const rooms = wards.get(bed.ward_code) ?? new Map<string, Bed[]>();
    rooms.set(bed.room_code, [...(rooms.get(bed.room_code) ?? []), bed]);
    wards.set(bed.ward_code, rooms);
  }
  return (
    <div className="space-y-6">
      <BillingAccessSetup hospital={hospital} />
      <BedChargingSetup hospital={hospital} />
      {manage && <AdmitPatient hospital={hospital} available={available} />}
      <Panel aria-labelledby="ward-board-heading" className="p-0">
        <h3 id="ward-board-heading" className="px-5 pt-4 font-semibold">
          Beds at {hospital.name}
        </h3>
        {beds.isError ? (
          <div className="p-5">
            <RequestFailure message="Could not load the beds." retry={beds.retry} />
          </div>
        ) : beds.isPending ? (
          <p role="status" className="px-5 py-4 text-sm">
            Loading beds…
          </p>
        ) : beds.items.length === 0 ? (
          <p className="px-5 py-4 text-sm text-muted-foreground">
            No beds are set up at this hospital yet.
          </p>
        ) : (
          [...wards].map(([wardCode, rooms]) => (
            <section key={wardCode} aria-label={`Ward ${wardCode}`} className="mt-3">
              <h4 className="px-5 text-sm font-medium text-muted-foreground">Ward {wardCode}</h4>
              {[...rooms].map(([roomCode, roomBeds]) => (
                <ul key={roomCode} aria-label={`Room ${roomCode}`} className="divide-y border-t">
                  {roomBeds.map((bed) => (
                    <BedRow
                      key={bed.id}
                      hospital={hospital}
                      bed={bed}
                      available={available.filter((b) => b.id !== bed.id)}
                      manage={manage}
                      billing={billing}
                    />
                  ))}
                </ul>
              ))}
            </section>
          ))
        )}
        <div className="px-5 pb-4">
          <LoadMore
            label="Load more beds"
            hasMore={beds.hasMore}
            isLoading={beds.isFetchingMore}
            onClick={beds.loadMore}
          />
        </div>
      </Panel>
      {can("medicore.bed.configure") && <BedSetup hospital={hospital} />}
    </div>
  );
}

type Action = "transfer" | "release" | "out_of_service" | "return" | null;

function BedRow({
  hospital,
  bed,
  available,
  manage,
  billing,
}: {
  hospital: Hospital;
  bed: Bed;
  available: Bed[];
  manage: boolean;
  billing: boolean;
}) {
  const [action, setAction] = useState<Action>(null);
  const [bills, setBills] = useState(false);
  // One id per command on this version of the bed: a retried click repeats
  // the same command, and once the bed moves on (even after a lost answer)
  // the next command gets a fresh id.
  const commandId = useMemo(() => newBedId(), [bed.version, action]); // eslint-disable-line react-hooks/exhaustive-deps
  const done = {
    invalidate: [["medicore", "beds", hospital.id]],
    invalidateOnError: true,
    onSuccess: () => setAction(null),
  };
  const move = useApiMutation(
    ({ to, reason }: { to: BedState; reason?: string }) =>
      apiFetch<Bed>(`/medicore/hospitals/${hospital.id}/beds/${bed.id}/transitions/${commandId}`, {
        method: "PUT",
        body: JSON.stringify({ to, expected_version: bed.version, ...(reason ? { reason } : {}) }),
      }),
    { ...done, successToast: "Bed updated" },
  );
  return (
    <li className="px-5 py-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="font-medium">
            Bed {bed.code}
            <span className="ml-2 text-sm font-normal text-muted-foreground">
              Room {bed.room_code} · {bedClassLabel[bed.bed_class]}
            </span>
          </p>
          {bed.patient_id && (
            <p className="mt-0.5 text-sm">
              <PatientName hospital={hospital} id={bed.patient_id} />
            </p>
          )}
          {billing && bed.admission_id && (
            <p className="mt-0.5">
              <AdmissionBalance hospital={hospital} admissionId={bed.admission_id} />
              <Button
                variant="link"
                size="sm"
                className="ml-2 h-auto p-0 text-xs"
                aria-expanded={bills}
                onClick={() => setBills(!bills)}
              >
                Bills
              </Button>
            </p>
          )}
          {billing && bed.admission_id && bills && (
            <InterimBills hospital={hospital} admissionId={bed.admission_id} />
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant="outline">{stateLabel[bed.state]}</Badge>
          {manage && bed.state === "occupied" && (
            <>
              <Button variant="outline" size="sm" onClick={() => setAction("transfer")}>
                Transfer
              </Button>
              <Button variant="outline" size="sm" onClick={() => setAction("release")}>
                Release
              </Button>
            </>
          )}
          {manage && bed.state === "cleaning" && (
            <Button
              variant="outline"
              size="sm"
              disabled={move.isPending}
              onClick={() => move.mutate({ to: "available" })}
            >
              Mark ready
            </Button>
          )}
          {manage && (bed.state === "available" || bed.state === "cleaning") && (
            <Button variant="outline" size="sm" onClick={() => setAction("out_of_service")}>
              Take out of service
            </Button>
          )}
          {manage && bed.state === "out_of_service" && (
            <Button variant="outline" size="sm" onClick={() => setAction("return")}>
              Return to service
            </Button>
          )}
        </div>
      </div>
      {move.isError && (
        <p role="alert" className="mt-2 text-sm text-destructive">
          {move.error.message}
        </p>
      )}
      {(action === "out_of_service" || action === "return") && (
        <BedReason
          id={`bed-reason-${bed.id}`}
          label={action === "return" ? "Why it is back in service" : "Why it is out of service"}
          busy={move.isPending}
          onCancel={() => setAction(null)}
          onSubmit={(reason) =>
            move.mutate({ to: action === "return" ? "cleaning" : "out_of_service", reason })
          }
        />
      )}
      {action === "transfer" && bed.admission_id && (
        <TransferForm
          hospital={hospital}
          admissionId={bed.admission_id}
          commandId={commandId}
          available={available}
          done={done}
          onCancel={() => setAction(null)}
        />
      )}
      {action === "release" && bed.admission_id && (
        <ReleaseForm
          hospital={hospital}
          admissionId={bed.admission_id}
          bedCode={bed.code}
          done={done}
          onCancel={() => setAction(null)}
        />
      )}
    </li>
  );
}

function BedReason({
  id,
  label,
  busy,
  onCancel,
  onSubmit,
}: {
  id: string;
  label: string;
  busy: boolean;
  onCancel: () => void;
  onSubmit: (reason: string) => void;
}) {
  const form = useZodForm(bedReasonSchema, { reason: "" });
  return (
    <form
      noValidate
      className="mt-3 flex flex-wrap items-end gap-2"
      onSubmit={form.handleSubmit(({ reason }) => onSubmit(reason))}
    >
      <Field id={id} label={label} error={form.formState.errors.reason?.message}>
        <input id={id} className={`${inputClass} w-72`} {...form.register("reason")} />
      </Field>
      <Button type="submit" size="sm" disabled={busy}>
        Save
      </Button>
      <Button type="button" variant="ghost" size="sm" onClick={onCancel}>
        Cancel
      </Button>
    </form>
  );
}

type Done = { invalidate: unknown[][]; invalidateOnError: boolean; onSuccess: () => void };

function TransferForm({
  hospital,
  admissionId,
  commandId,
  available,
  done,
  onCancel,
}: {
  hospital: Hospital;
  admissionId: string;
  commandId: string;
  available: Bed[];
  done: Done;
  onCancel: () => void;
}) {
  const schema = z.object({ to_bed_id: z.string().min(1, "Choose an available bed") });
  const form = useZodForm(schema, { to_bed_id: "" });
  const transfer = useApiMutation(
    ({ to_bed_id, version }: { to_bed_id: string; version: number }) =>
      apiFetch<Admission>(
        `/medicore/hospitals/${hospital.id}/admissions/${admissionId}/transfers/${commandId}`,
        { method: "PUT", body: JSON.stringify({ to_bed_id, expected_bed_version: version }) },
      ),
    { ...done, successToast: "Patient transferred" },
  );
  // The board may have refreshed since the bed was chosen: a bed no longer
  // available is not sent, and the pharmacist is told why.
  const submit = ({ to_bed_id }: z.infer<typeof schema>) => {
    const target = available.find((b) => b.id === to_bed_id);
    if (!target) {
      form.setError("to_bed_id", { message: "That bed was just taken. Choose another." });
      return;
    }
    transfer.mutate({ to_bed_id, version: target.version });
  };
  if (available.length === 0)
    return (
      <p className="mt-3 text-sm text-muted-foreground">
        No other bed is available.{" "}
        <button type="button" className="underline" onClick={onCancel}>
          Close
        </button>
      </p>
    );
  return (
    <form
      noValidate
      className="mt-3 flex flex-wrap items-end gap-2"
      onSubmit={form.handleSubmit(submit)}
    >
      <FormSelectField
        control={form.control}
        name="to_bed_id"
        id={`transfer-${admissionId}`}
        label="Move to bed"
        options={available.map((b) => ({
          value: b.id,
          label: `${b.code} · Room ${b.room_code} · ${bedClassLabel[b.bed_class]}`,
        }))}
      />
      <Button type="submit" size="sm" disabled={transfer.isPending}>
        {transfer.isPending ? "Transferring…" : "Transfer"}
      </Button>
      <Button type="button" variant="ghost" size="sm" onClick={onCancel}>
        Cancel
      </Button>
      {transfer.isError && (
        <p role="alert" className="w-full text-sm text-destructive">
          {transfer.error.message}
        </p>
      )}
    </form>
  );
}

function ReleaseForm({
  hospital,
  admissionId,
  bedCode,
  done,
  onCancel,
}: {
  hospital: Hospital;
  admissionId: string;
  bedCode: string;
  done: Done;
  onCancel: () => void;
}) {
  const form = useZodForm(releaseSchema, { reason: "discharged", note: "" });
  // The parsed values (trimmed note) that the confirmation will send.
  const [confirming, setConfirming] = useState<z.infer<typeof releaseSchema> | null>(null);
  const release = useApiMutation(
    (values: z.infer<typeof releaseSchema>) =>
      apiFetch<Admission>(`/medicore/hospitals/${hospital.id}/admissions/${admissionId}/release`, {
        method: "PUT",
        body: JSON.stringify({
          reason: values.reason,
          ...(values.note ? { note: values.note } : {}),
        }),
      }),
    {
      ...done,
      successToast: "Patient released",
      onSuccess: () => {
        setConfirming(null);
        done.onSuccess();
      },
    },
  );
  const reason = form.watch("reason") as ReleaseReason;
  return (
    <form
      noValidate
      className="mt-3 flex flex-wrap items-end gap-2"
      onSubmit={form.handleSubmit((values) => setConfirming(values))}
    >
      <FormSelectField
        control={form.control}
        name="reason"
        id={`release-reason-${admissionId}`}
        label="Why the patient left the bed"
        options={(Object.keys(releaseReasonLabel) as ReleaseReason[]).map((r) => ({
          value: r,
          label: releaseReasonLabel[r],
        }))}
      />
      <Field
        id={`release-note-${admissionId}`}
        label={reason === "other" ? "What happened" : "Note (optional)"}
        error={form.formState.errors.note?.message}
      >
        <input
          id={`release-note-${admissionId}`}
          className={`${inputClass} w-72`}
          {...form.register("note")}
        />
      </Field>
      <Button type="submit" size="sm" disabled={release.isPending}>
        Release
      </Button>
      <Button type="button" variant="ghost" size="sm" onClick={onCancel}>
        Cancel
      </Button>
      {release.isError && (
        <p role="alert" className="w-full text-sm text-destructive">
          {release.error.message}
        </p>
      )}
      <ConfirmDialog
        open={confirming !== null}
        onOpenChange={(open) => !open && setConfirming(null)}
        title={`Release the patient from bed ${bedCode}?`}
        description="The stay ends now and the bed goes for cleaning. This records the departure; it is not the clinical discharge."
        confirmLabel="Release"
        busy={release.isPending}
        onConfirm={() => confirming && release.mutate(confirming)}
      />
    </form>
  );
}

const searchSchema = z.object({
  query: z.string().trim().min(1, "Enter a name, MRN or mobile").max(100),
});

function AdmitPatient({ hospital, available }: { hospital: Hospital; available: Bed[] }) {
  // One id per admission being made, kept across retries of the same choice.
  const [admissionId, setAdmissionId] = useState(() => newBedId());
  const search = useZodForm(searchSchema, { query: "" });
  const [submitted, setSubmitted] = useState("");
  const patients = useApiPagedQuery<Patient>(
    ["patients", "registry", hospital.id, "ipd", submitted],
    `/patients/search?hospital_id=${hospital.id}`,
    {
      request: { method: "POST", body: JSON.stringify({ query: submitted }) },
      enabled: submitted !== "",
    },
  );
  const form = useZodForm(admitSchema, { patient_id: "", bed_id: "", intent: "emergency" });
  const admit = useApiMutation(
    (values: z.infer<typeof admitSchema> & { version: number }) =>
      apiFetch<Admission>(`/medicore/hospitals/${hospital.id}/admissions/${admissionId}`, {
        method: "PUT",
        body: JSON.stringify({
          patient_id: values.patient_id,
          bed_id: values.bed_id,
          intent: values.intent,
          expected_bed_version: values.version,
        }),
      }),
    {
      successToast: "Patient admitted",
      invalidate: [["medicore", "beds", hospital.id]],
      invalidateOnError: true,
      onSuccess: () => {
        form.reset({ patient_id: "", bed_id: "", intent: "emergency" });
        setAdmissionId(newBedId());
      },
      // A refusal is settled: the next choice is a new admission. Any other
      // failure may have committed, so a retry keeps the id and replays.
      onError: (error: Error) => {
        if (error instanceof ApiError && (error.status === 409 || error.status === 400))
          setAdmissionId(newBedId());
      },
    },
  );
  return (
    <Panel aria-labelledby="admit-heading" className="space-y-4">
      <h3 id="admit-heading" className="font-semibold">
        Admit a patient
      </h3>
      <form
        noValidate
        role="search"
        className="flex flex-wrap items-end gap-2"
        onSubmit={search.handleSubmit(({ query }) => setSubmitted(query))}
      >
        <Field
          id="ipd-patient-search"
          label="Find a patient registered here"
          error={search.formState.errors.query?.message}
        >
          <input
            id="ipd-patient-search"
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
        onSubmit={form.handleSubmit((values) => {
          const target = available.find((b) => b.id === values.bed_id);
          if (!target) {
            form.setError("bed_id", { message: "That bed was just taken. Choose another." });
            return;
          }
          admit.mutate({ ...values, version: target.version });
        })}
      >
        <FormSelectField
          control={form.control}
          name="patient_id"
          id="ipd-patient"
          label="Patient"
          disabled={!patients.items.length}
          options={patients.items.map((p) => ({
            value: p.id,
            label: `${[p.given_name, p.family_name].filter(Boolean).join(" ")} · ${p.mrn}`,
          }))}
        />
        <FormSelectField
          control={form.control}
          name="bed_id"
          id="ipd-bed"
          label="Bed"
          disabled={!available.length}
          options={available.map((b) => ({
            value: b.id,
            label: `${b.code} · Room ${b.room_code} · ${bedClassLabel[b.bed_class]}`,
          }))}
        />
        <FormSelectField
          control={form.control}
          name="intent"
          id="ipd-intent"
          label="Admission"
          options={[
            { value: "emergency", label: "Emergency" },
            { value: "elective", label: "Elective" },
          ]}
        />
        {available.length === 0 && (
          <p className="text-sm text-muted-foreground sm:col-span-3">No bed is available now.</p>
        )}
        <div className="sm:col-span-3">
          <Button type="submit" disabled={admit.isPending}>
            {admit.isPending ? "Admitting…" : "Admit"}
          </Button>
        </div>
        {admit.isError && (
          <p role="alert" className="text-sm text-destructive sm:col-span-3">
            {admit.error.message}
          </p>
        )}
      </form>
    </Panel>
  );
}
