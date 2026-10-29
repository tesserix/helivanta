"use client";

import { useState } from "react";
import { z } from "zod";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  bedClassLabel,
  bedInputSchema,
  newBedId,
  roomInputSchema,
  useApiMutation,
  useApiPagedQuery,
  wardInputSchema,
  type BedClass,
  type Hospital,
  type Room,
  type Ward,
} from "@helivanta/api";
import { Field, FormSelectField, Panel, RequestFailure, useZodForm } from "@helivanta/ui";

const inputClass =
  "w-full rounded-md border bg-background px-3 py-2.5 text-sm outline-none focus-visible:ring-2 focus-visible:ring-(--hms-accent)";

/** A hospital's wards, rooms and beds (#106 slice 1). Codes never change once recorded. */
export function BedSetup({ hospital }: { hospital: Hospital }) {
  const root = `/medicore/hospitals/${hospital.id}`;
  const wards = useApiPagedQuery<Ward>(
    ["medicore", "wards", hospital.id],
    `${root}/wards?limit=200`,
  );
  const rooms = useApiPagedQuery<Room>(
    ["medicore", "rooms", hospital.id],
    `${root}/rooms?limit=200`,
  );
  return (
    <Panel aria-labelledby="bed-setup-heading" className="space-y-6">
      <div>
        <h3 id="bed-setup-heading" className="font-semibold">
          Wards, rooms and beds
        </h3>
        <p className="mt-1 text-sm text-muted-foreground">
          Codes are unique at {hospital.name} and never change once recorded.
        </p>
      </div>
      {wards.isError || rooms.isError ? (
        <RequestFailure
          message="Could not load the wards and rooms."
          retry={() => {
            wards.retry();
            rooms.retry();
          }}
        />
      ) : (
        <>
          <WardForm root={root} hospitalId={hospital.id} />
          <RoomForm root={root} hospitalId={hospital.id} wards={wards.items} />
          <BedForm root={root} hospitalId={hospital.id} rooms={rooms.items} wards={wards.items} />
        </>
      )}
    </Panel>
  );
}

function WardForm({ root, hospitalId }: { root: string; hospitalId: string }) {
  const [id, setId] = useState(() => newBedId());
  const form = useZodForm(wardInputSchema, { code: "", name: "" });
  const save = useApiMutation(
    (values: z.infer<typeof wardInputSchema>) =>
      apiFetch<Ward>(`${root}/wards/${id}`, { method: "PUT", body: JSON.stringify(values) }),
    {
      successToast: "Ward added",
      invalidate: [["medicore", "wards", hospitalId]],
      onSuccess: () => {
        setId(newBedId());
        form.reset();
      },
      suppressErrorToast: true,
    },
  );
  return (
    <form
      noValidate
      aria-label="Add a ward"
      className="grid items-end gap-4 sm:grid-cols-3"
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
    >
      <Field id="ward-code" label="Ward code" error={form.formState.errors.code?.message}>
        <input id="ward-code" className={inputClass} {...form.register("code")} />
      </Field>
      <Field id="ward-name" label="Ward name" error={form.formState.errors.name?.message}>
        <input id="ward-name" className={inputClass} {...form.register("name")} />
      </Field>
      <Button type="submit" variant="outline" disabled={save.isPending}>
        Add ward
      </Button>
      {save.isError && (
        <p role="alert" className="text-sm text-destructive sm:col-span-3">
          {save.error.message}
        </p>
      )}
    </form>
  );
}

function RoomForm({
  root,
  hospitalId,
  wards,
}: {
  root: string;
  hospitalId: string;
  wards: Ward[];
}) {
  const [id, setId] = useState(() => newBedId());
  const form = useZodForm(roomInputSchema, { ward_id: "", code: "" });
  const save = useApiMutation(
    (values: z.infer<typeof roomInputSchema>) =>
      apiFetch<Room>(`${root}/wards/${values.ward_id}/rooms/${id}`, {
        method: "PUT",
        body: JSON.stringify({ code: values.code }),
      }),
    {
      successToast: "Room added",
      invalidate: [["medicore", "rooms", hospitalId]],
      onSuccess: () => {
        setId(newBedId());
        form.reset({ ward_id: form.getValues("ward_id"), code: "" });
      },
      suppressErrorToast: true,
    },
  );
  return (
    <form
      noValidate
      aria-label="Add a room"
      className="grid items-end gap-4 sm:grid-cols-3"
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
    >
      <FormSelectField
        control={form.control}
        name="ward_id"
        id="room-ward"
        label="Ward"
        disabled={!wards.length}
        options={wards.map((w) => ({ value: w.id, label: `${w.code} · ${w.name}` }))}
      />
      <Field id="room-code" label="Room code" error={form.formState.errors.code?.message}>
        <input id="room-code" className={inputClass} {...form.register("code")} />
      </Field>
      <Button type="submit" variant="outline" disabled={save.isPending}>
        Add room
      </Button>
      {save.isError && (
        <p role="alert" className="text-sm text-destructive sm:col-span-3">
          {save.error.message}
        </p>
      )}
    </form>
  );
}

function BedForm({
  root,
  hospitalId,
  rooms,
  wards,
}: {
  root: string;
  hospitalId: string;
  rooms: Room[];
  wards: Ward[];
}) {
  const [id, setId] = useState(() => newBedId());
  const form = useZodForm(bedInputSchema, { room_id: "", code: "", bed_class: "general" });
  const wardCode = (wardId: string) => wards.find((w) => w.id === wardId)?.code ?? "";
  const save = useApiMutation(
    (values: z.infer<typeof bedInputSchema>) =>
      apiFetch(`${root}/rooms/${values.room_id}/beds/${id}`, {
        method: "PUT",
        body: JSON.stringify({ code: values.code, bed_class: values.bed_class }),
      }),
    {
      successToast: "Bed added",
      invalidate: [["medicore", "beds", hospitalId]],
      onSuccess: () => {
        setId(newBedId());
        form.reset({ ...form.getValues(), code: "" });
      },
      suppressErrorToast: true,
    },
  );
  return (
    <form
      noValidate
      aria-label="Add a bed"
      className="grid items-end gap-4 sm:grid-cols-4"
      onSubmit={form.handleSubmit((values) => save.mutate(values))}
    >
      <FormSelectField
        control={form.control}
        name="room_id"
        id="bed-room"
        label="Room"
        disabled={!rooms.length}
        options={rooms.map((r) => ({
          value: r.id,
          label: `${r.code} · Ward ${wardCode(r.ward_id)}`,
        }))}
      />
      <Field id="bed-code" label="Bed code" error={form.formState.errors.code?.message}>
        <input id="bed-code" className={inputClass} {...form.register("code")} />
      </Field>
      <FormSelectField
        control={form.control}
        name="bed_class"
        id="bed-class"
        label="Bed class"
        options={(Object.keys(bedClassLabel) as BedClass[]).map((c) => ({
          value: c,
          label: bedClassLabel[c],
        }))}
      />
      <Button type="submit" variant="outline" disabled={save.isPending}>
        Add bed
      </Button>
      {save.isError && (
        <p role="alert" className="text-sm text-destructive sm:col-span-4">
          {save.error.message}
        </p>
      )}
    </form>
  );
}
