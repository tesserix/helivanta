"use client";

import { useRef, useState } from "react";
import { Button, Input, Badge } from "@tesserix/web";
import {
  useApiPagedQuery,
  usePermissions,
  newEmergencyId,
  emergencyVehicleSchema,
  emergencyCrewSchema,
  emergencyDutySchema,
  emergencyReadinessSchema,
  emergencyConstraintClearanceSchema,
  emergencyFleetChoices,
  emergencyLocalTime,
  emergencyTimeInput,
  type EmergencyVehicle,
  type EmergencyVehicleInput,
  type EmergencyCrew,
  type EmergencyCrewInput,
  type EmergencyDuty,
  type EmergencyDutyInput,
  type EmergencyReadinessInput,
  type EmergencyConstraint,
} from "@helivanta/api";
import {
  Field,
  FormSelectField,
  useZodForm,
  LoadMore,
  formatTime,
  ConfirmDialog,
} from "@helivanta/ui";

import { useEmergencyCommand, EmergencyCommandFailure } from "./emergency-command";

const options = (group: Record<string, string>) =>
  Object.entries(group).map(([value, label]) => ({ value, label }));

export function EmergencyFleet({
  base,
  level,
}: {
  base: string;
  level: "reader" | "dispatcher" | "supervisor";
}) {
  const { can } = usePermissions();
  const vehicles = useApiPagedQuery<EmergencyVehicle>(
    ["emergency", "fleet", base, "vehicles"],
    `/emergency/bases/${base}/vehicles`,
    { enabled: can("emergency.fleet.read"), poll: true },
  );
  const [selected, setSelected] = useState<EmergencyVehicle | null>(null);
  const [creation, setCreation] = useState(0);
  const [checking, setChecking] = useState<EmergencyVehicle | null>(null);
  const manage = level === "supervisor" && can("emergency.fleet.manage");
  if (!can("emergency.fleet.read")) return null;
  return (
    <section className="min-w-0 space-y-4 rounded-xl border bg-card p-5">
      <header className="space-y-2">
        <h2 className="text-xl font-semibold">Fleet readiness</h2>
        <p className="text-sm text-muted-foreground">
          Readiness is evaluated from current certification, duty and measured checks. This board
          does not assign an ambulance. Refresh before an operational decision.
        </p>
      </header>
      <Button
        variant="outline"
        onClick={() => {
          setSelected(null);
          setChecking(null);
          vehicles.retry();
        }}
      >
        Refresh fleet
      </Button>
      {vehicles.isPending && <p role="status">Loading fleet…</p>}
      {vehicles.isError ? (
        <p role="alert">{vehicles.error?.message}</p>
      ) : (
        <ul className="grid gap-4 lg:grid-cols-2">
          {vehicles.items.map((v) => (
            <li key={v.id} className="min-w-0 space-y-3 rounded-xl border p-4">
              <div className="flex flex-wrap justify-between gap-2">
                <h3 className="break-all font-semibold">{v.code}</h3>
                <Badge variant={v.dispatchable ? "secondary" : "outline"}>
                  {v.dispatchable ? `Ready · ${v.effective_capability?.toUpperCase()}` : "Blocked"}
                </Badge>
              </div>
              <p className="text-sm text-muted-foreground">
                Configured{" "}
                {
                  emergencyFleetChoices.classes[
                    v.vehicle_class as keyof typeof emergencyFleetChoices.classes
                  ]
                }{" "}
                · version {v.version}. Computed {formatTime(v.computed_at)}.
              </p>
              <ul className="space-y-1 text-sm">
                {[...new Set(v.blockers)].map((reason) => (
                  <li key={reason}>
                    {emergencyFleetChoices.blockers[reason] ??
                      "Unknown readiness restriction — contact supervisor"}
                  </li>
                ))}
              </ul>
              <div className="flex flex-wrap gap-2">
                {manage && (
                  <Button
                    className="h-auto min-h-11 max-w-full whitespace-normal text-left"
                    variant="outline"
                    onClick={() => setSelected(v)}
                  >
                    Configure {v.code}
                  </Button>
                )}
                {level !== "reader" && can("emergency.fleet.check") && v.duty_id && (
                  <Button
                    className="h-auto min-h-11 max-w-full whitespace-normal text-left"
                    variant="outline"
                    onClick={() => setChecking(v)}
                  >
                    Record readiness for {v.code}
                  </Button>
                )}
              </div>
              {manage &&
                v.open_constraints.map((constraint) => (
                  <ClearConstraint
                    key={`${constraint.id}-${constraint.code}`}
                    base={base}
                    constraint={constraint}
                  />
                ))}
            </li>
          ))}
        </ul>
      )}
      {!vehicles.isPending && !vehicles.isError && vehicles.items.length === 0 && (
        <p>No fleet configured at this base.</p>
      )}
      <LoadMore
        hasMore={vehicles.hasMore}
        isLoading={vehicles.isFetchingMore}
        onClick={vehicles.loadMore}
      />
      {manage && (
        <>
          <VehicleForm
            key={`${selected?.id ?? "new"}-${selected?.version ?? 0}-${creation}`}
            base={base}
            vehicle={selected ?? undefined}
          />
          <Button
            variant="outline"
            onClick={() => {
              setSelected(null);
              setCreation((value) => value + 1);
            }}
          >
            Configure another vehicle
          </Button>
          <CrewForm base={base} />
          <DutyForm base={base} vehicles={vehicles.items} />
        </>
      )}
      {checking && level !== "reader" && can("emergency.fleet.check") && (
        <ReadinessForm
          key={`${checking.id}-${checking.version}-${checking.duty_id}`}
          base={base}
          vehicle={checking}
        />
      )}
    </section>
  );
}
function VehicleForm({ base, vehicle }: { base: string; vehicle?: EmergencyVehicle }) {
  const id = useRef(vehicle?.id ?? newEmergencyId());
  const form = useZodForm(
    emergencyVehicleSchema,
    vehicle
      ? { ...vehicle, certified_until: emergencyTimeInput(vehicle.certified_until) }
      : {
          version: 0,
          code: "",
          vehicle_class: "bls",
          certified_until: "",
          policy_reference: "",
          oxygen_min_bar: undefined,
          readiness_minutes: undefined,
          retired: false,
        },
  );
  const save = useEmergencyCommand<EmergencyVehicleInput, EmergencyVehicle>(
    base,
    () => `vehicles/${id.current}`,
    "PUT",
    "Vehicle saved",
    (data) => form.reset({ ...data, certified_until: emergencyTimeInput(data.certified_until) }),
  );
  return (
    <details open={!!vehicle} className="rounded-lg border p-4">
      <summary className="cursor-pointer font-medium">
        {vehicle ? `Vehicle policy · ${vehicle.code}` : "Configure vehicle"}
      </summary>
      <p className="my-3 text-sm text-muted-foreground">
        A supervisor must verify certification and enter the medical lead’s approved readiness
        policy. No clinical threshold is supplied by the application.
      </p>
      <form
        noValidate
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
        className="space-y-4"
      >
        <fieldset disabled={save.frozen} className="grid gap-4 sm:grid-cols-2">
          <Field
            id="fleet-code"
            label="Vehicle operational code"
            error={form.formState.errors.code?.message}
          >
            <Input id="fleet-code" {...form.register("code")} />
          </Field>
          <FormSelectField
            control={form.control}
            name="vehicle_class"
            id="fleet-class"
            label="Certified vehicle class"
            options={options(emergencyFleetChoices.classes)}
          />
          <Field
            id="fleet-certification"
            label="Vehicle certification valid until"
            error={form.formState.errors.certified_until?.message}
          >
            <Input
              id="fleet-certification"
              type="datetime-local"
              {...form.register("certified_until", { setValueAs: emergencyLocalTime })}
            />
          </Field>
          <Field
            id="fleet-policy"
            label="Vehicle policy reference"
            error={form.formState.errors.policy_reference?.message}
          >
            <Input id="fleet-policy" {...form.register("policy_reference")} />
          </Field>
          <Field
            id="fleet-oxygen"
            label="Minimum oxygen pressure (bar)"
            error={form.formState.errors.oxygen_min_bar?.message}
          >
            <Input
              id="fleet-oxygen"
              type="number"
              {...form.register("oxygen_min_bar", { valueAsNumber: true })}
            />
          </Field>
          <Field
            id="fleet-validity"
            label="Readiness validity (minutes)"
            error={form.formState.errors.readiness_minutes?.message}
          >
            <Input
              id="fleet-validity"
              type="number"
              {...form.register("readiness_minutes", { valueAsNumber: true })}
            />
          </Field>
          <label className="flex min-h-11 items-center gap-3">
            <input type="checkbox" {...form.register("retired")} />
            Vehicle retired
          </label>
        </fieldset>
        <EmergencyCommandFailure message={save.error?.message} uncertain={save.uncertain} />
        <Button type="submit" disabled={save.isPending}>
          Save vehicle
        </Button>
        {save.data && (
          <p role="status">
            Saved configuration version {save.data.version}. A policy change requires a new
            readiness observation.
          </p>
        )}
      </form>
    </details>
  );
}
function CrewForm({ base }: { base: string }) {
  const crew = useApiPagedQuery<EmergencyCrew>(
    ["emergency", "fleet", base, "crew"],
    `/emergency/bases/${base}/crew`,
  );
  const [selected, setSelected] = useState<EmergencyCrew | null>(null);
  const [creation, setCreation] = useState(0);
  return (
    <details className="space-y-4 rounded-lg border p-4">
      <summary className="cursor-pointer font-medium">Verify crew credentials</summary>
      <p className="my-3 text-sm text-muted-foreground">
        Record the verified identity subject and evidence reference. This is an attributed
        supervisor verification; it does not claim a workforce or licensing-authority integration.
      </p>
      {crew.isError && <p role="alert">{crew.error?.message}</p>}
      <ul className="my-3 space-y-2">
        {crew.items.map((person) => (
          <li className="flex flex-wrap items-center justify-between gap-2" key={person.id}>
            <span className="break-all text-sm">
              {person.subject} · {person.qualification} ·{" "}
              {person.suspended ? "Suspended" : `valid until ${formatTime(person.certified_until)}`}
            </span>
            <Button
              className="h-auto min-h-11 max-w-full break-all whitespace-normal text-left"
              variant="outline"
              onClick={() => setSelected(person)}
            >
              Review {person.subject}
            </Button>
          </li>
        ))}
      </ul>
      <LoadMore hasMore={crew.hasMore} isLoading={crew.isFetchingMore} onClick={crew.loadMore} />
      <CrewEditor
        key={`${selected?.id ?? "new"}-${selected?.version ?? 0}-${creation}`}
        base={base}
        person={selected ?? undefined}
      />
      <Button
        variant="outline"
        onClick={() => {
          setSelected(null);
          setCreation((value) => value + 1);
        }}
      >
        Verify another person
      </Button>
    </details>
  );
}
function CrewEditor({ base, person }: { base: string; person?: EmergencyCrew }) {
  const id = useRef(person?.id ?? newEmergencyId());
  const form = useZodForm(
    emergencyCrewSchema,
    person
      ? { ...person, certified_until: emergencyTimeInput(person.certified_until) }
      : {
          version: 0,
          subject: "",
          qualification: "driver",
          certified_until: "",
          evidence_reference: "",
          suspended: false,
        },
  );
  const save = useEmergencyCommand<EmergencyCrewInput, EmergencyCrew>(
    base,
    () => `crew/${id.current}`,
    "PUT",
    "Credential saved",
    (data) => form.reset({ ...data, certified_until: emergencyTimeInput(data.certified_until) }),
  );
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
      className="space-y-4"
    >
      <fieldset disabled={save.frozen} className="grid gap-4 sm:grid-cols-2">
        <Field
          id="crew-subject"
          label="Crew identity subject"
          error={form.formState.errors.subject?.message}
        >
          <Input
            id="crew-subject"
            readOnly={!!person || !!save.data}
            {...form.register("subject")}
          />
        </Field>
        <FormSelectField
          control={form.control}
          name="qualification"
          id="crew-qualification"
          label="Verified qualification"
          options={options(emergencyFleetChoices.qualifications)}
        />
        <Field
          id="crew-validity"
          label="Credential valid until"
          error={form.formState.errors.certified_until?.message}
        >
          <Input
            id="crew-validity"
            type="datetime-local"
            {...form.register("certified_until", { setValueAs: emergencyLocalTime })}
          />
        </Field>
        <Field
          id="crew-evidence"
          label="Credential evidence reference"
          error={form.formState.errors.evidence_reference?.message}
        >
          <Input id="crew-evidence" {...form.register("evidence_reference")} />
        </Field>
        <label className="flex min-h-11 items-center gap-3">
          <input type="checkbox" {...form.register("suspended")} />
          Credential suspended
        </label>
      </fieldset>
      <EmergencyCommandFailure message={save.error?.message} uncertain={save.uncertain} />
      <Button type="submit" disabled={save.isPending}>
        Save credential
      </Button>
    </form>
  );
}
function DutyForm({ base, vehicles }: { base: string; vehicles: EmergencyVehicle[] }) {
  const duties = useApiPagedQuery<EmergencyDuty>(
    ["emergency", "fleet", base, "duties"],
    `/emergency/bases/${base}/duties`,
  );
  const id = useRef(newEmergencyId());
  const form = useZodForm(emergencyDutySchema, {
    vehicle_id: "",
    driver_subject: "",
    clinician_subject: "",
    ends_at: "",
  });
  const save = useEmergencyCommand<EmergencyDutyInput>(
    base,
    () => `duties/${id.current}`,
    "PUT",
    "Duty started",
    () => {
      id.current = newEmergencyId();
      form.reset();
    },
  );
  return (
    <details className="space-y-4 rounded-lg border p-4">
      <summary className="cursor-pointer font-medium">Manage duty</summary>
      <p className="my-3 text-sm text-muted-foreground">
        Two separately verified people are required. End the previous duty explicitly before
        reallocating its vehicle or people. Maximum shift length is 24 hours; readiness expires
        earlier where required.
      </p>
      <form
        noValidate
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
        className="space-y-4"
      >
        <fieldset disabled={save.frozen} className="grid gap-4 sm:grid-cols-2">
          <FormSelectField
            control={form.control}
            name="vehicle_id"
            id="duty-vehicle"
            label="Duty vehicle"
            options={vehicles.map((v) => ({ value: v.id, label: v.code }))}
          />
          <Field
            id="duty-driver"
            label="Duty driver subject"
            error={form.formState.errors.driver_subject?.message}
          >
            <Input id="duty-driver" {...form.register("driver_subject")} />
          </Field>
          <Field
            id="duty-clinician"
            label="Duty clinician subject"
            error={form.formState.errors.clinician_subject?.message}
          >
            <Input id="duty-clinician" {...form.register("clinician_subject")} />
          </Field>
          <Field id="duty-end" label="Duty ends at" error={form.formState.errors.ends_at?.message}>
            <Input
              id="duty-end"
              type="datetime-local"
              {...form.register("ends_at", { setValueAs: emergencyLocalTime })}
            />
          </Field>
        </fieldset>
        <EmergencyCommandFailure message={save.error?.message} uncertain={save.uncertain} />
        <Button type="submit" disabled={save.isPending}>
          Start duty
        </Button>
      </form>
      {duties.isError && <p role="alert">{duties.error?.message}</p>}
      <ul className="space-y-3">
        {duties.items.map((d) => (
          <li key={d.id} className="my-3 rounded-lg border p-3">
            <p className="break-all text-sm">
              {vehicles.find((v) => v.id === d.vehicle_id)?.code ?? d.vehicle_id} ·{" "}
              {d.driver_subject} / {d.clinician_subject}
            </p>
            <p className="text-sm text-muted-foreground">
              Ends {formatTime(d.ends_at)} · {d.ended_at ? "Ended" : "Open"}
            </p>
            {!d.ended_at && <EndDuty base={base} duty={d.id} />}
          </li>
        ))}
      </ul>
      <LoadMore
        hasMore={duties.hasMore}
        isLoading={duties.isFetchingMore}
        onClick={duties.loadMore}
      />
    </details>
  );
}
function EndDuty({ base, duty }: { base: string; duty: string }) {
  const [confirm, setConfirm] = useState(false);
  const end = useEmergencyCommand<Record<string, never>>(
    base,
    () => `duties/${duty}/end`,
    "POST",
    "Duty ended",
    () => setConfirm(false),
  );
  return (
    <>
      <Button variant="outline" onClick={() => setConfirm(true)}>
        End duty
      </Button>
      <EmergencyCommandFailure message={end.error?.message} uncertain={end.uncertain} />
      <ConfirmDialog
        open={confirm}
        onOpenChange={setConfirm}
        title="End this duty?"
        description="The vehicle will lose current-duty readiness. Confirm the crew have finished duty."
        confirmLabel="Confirm end duty"
        busy={end.isPending}
        onConfirm={() => end.mutate({})}
      />
    </>
  );
}
function ReadinessForm({ base, vehicle }: { base: string; vehicle: EmergencyVehicle }) {
  const id = useRef(newEmergencyId());
  const form = useZodForm(emergencyReadinessSchema, {
    vehicle_id: vehicle.id,
    vehicle_version: vehicle.version,
    duty_id: vehicle.duty_id ?? "",
    oxygen_bar: undefined,
    defibrillator_passed: false,
    equipment_complete: false,
    roadworthy: false,
  });
  const save = useEmergencyCommand<EmergencyReadinessInput>(
    base,
    () => `readiness/${id.current}`,
    "PUT",
    "Readiness recorded",
    () => {
      id.current = newEmergencyId();
      form.reset();
    },
  );
  return (
    <section className="space-y-4 rounded-lg border p-4">
      <h3 className="font-medium">Readiness observation · {vehicle.code}</h3>
      <p className="text-sm text-muted-foreground">
        Record actual measurements and checks under {vehicle.policy_reference}. Unchecked items
        record a failure. Passing observations do not clear an existing safety constraint.
      </p>
      <form
        noValidate
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
        className="space-y-4"
      >
        <fieldset disabled={save.frozen} className="space-y-4">
          <Field
            id="readiness-oxygen"
            label="Measured oxygen pressure (bar)"
            error={form.formState.errors.oxygen_bar?.message}
          >
            <Input
              id="readiness-oxygen"
              type="number"
              {...form.register("oxygen_bar", { valueAsNumber: true })}
            />
          </Field>
          <label className="flex min-h-11 items-center gap-3">
            <input type="checkbox" {...form.register("defibrillator_passed")} />
            Defibrillator self-test passed
          </label>
          <label className="flex min-h-11 items-center gap-3">
            <input type="checkbox" {...form.register("equipment_complete")} />
            Reviewed equipment checklist complete
          </label>
          <label className="flex min-h-11 items-center gap-3">
            <input type="checkbox" {...form.register("roadworthy")} />
            Roadworthiness check passed
          </label>
        </fieldset>
        <EmergencyCommandFailure message={save.error?.message} uncertain={save.uncertain} />
        <Button type="submit" disabled={save.isPending}>
          Record readiness
        </Button>
      </form>
    </section>
  );
}
function ClearConstraint({ base, constraint }: { base: string; constraint: EmergencyConstraint }) {
  const form = useZodForm(emergencyConstraintClearanceSchema, { evidence_reference: "" });
  const [confirmation, setConfirmation] = useState<{ evidence_reference: string } | null>(null);
  const clear = useEmergencyCommand<{ evidence_reference: string }>(
    base,
    () => `constraints/${constraint.id}/${constraint.code}/clear`,
    "POST",
    "Constraint cleared",
    () => setConfirmation(null),
  );
  const id = `clear-${constraint.id}-${constraint.code}`;
  return (
    <details className="rounded-lg border p-3">
      <summary className="cursor-pointer text-sm">
        Review {emergencyFleetChoices.blockers[constraint.code]}
      </summary>
      <form
        noValidate
        className="mt-3 space-y-3"
        onSubmit={form.handleSubmit((values) => setConfirmation(values))}
      >
        <Field
          id={id}
          label="Repair evidence reference"
          error={form.formState.errors.evidence_reference?.message}
        >
          <Input id={id} disabled={clear.frozen} {...form.register("evidence_reference")} />
        </Field>
        <Button type="submit" disabled={clear.isPending}>
          Clear constraint
        </Button>
        <EmergencyCommandFailure message={clear.error?.message} uncertain={clear.uncertain} />
      </form>
      <ConfirmDialog
        open={confirmation !== null}
        onOpenChange={(open) => {
          if (!open) setConfirmation(null);
        }}
        title="Clear this safety constraint?"
        description="Confirm that repair evidence has been reviewed and the equipment or vehicle is safe. A passed checklist alone is not repair evidence."
        confirmLabel="Confirm clearance"
        busy={clear.isPending}
        onConfirm={() => {
          if (confirmation) clear.mutate(confirmation);
        }}
      />
    </details>
  );
}
