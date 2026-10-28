"use client";

import { useRef } from "react";
import Link from "next/link";
import { Button, Input, Textarea } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  emergencyChoices,
  emergencyIntakeSchema,
  newEmergencyId,
  useApiMutation,
  type EmergencyBase,
  type EmergencyCase,
  type EmergencyIntakeInput,
} from "@helivanta/api";
import { Field, FormSelectField, useZodForm } from "@helivanta/ui";

export function EmergencyIntake({ base }: { base: EmergencyBase }) {
  const form = useZodForm(emergencyIntakeSchema, {
    channel: "phone",
    requested_service: "unknown",
    processing_basis: "medical_emergency",
    contact: { caller_name: "", callback_phone: "", relationship: "unknown" },
    incident: { location_text: "", patient_description: "" },
  });
  const attempt = useRef<{ id: string; values: EmergencyIntakeInput } | null>(null);
  const receive = useApiMutation(
    async (values: EmergencyIntakeInput) => {
      attempt.current ??= { id: newEmergencyId(), values };
      try {
        return await apiFetch<EmergencyCase>(
          `/emergency/bases/${base.id}/cases/${attempt.current.id}`,
          {
            method: "PUT",
            body: JSON.stringify(attempt.current.values),
          },
        );
      } catch (error) {
        if (error instanceof ApiError && [400, 413].includes(error.status)) attempt.current = null;
        throw error;
      }
    },
    { successToast: "Report received", invalidate: [["emergency", "cases", base.id]] },
  );
  if (receive.data)
    return (
      <section className="space-y-3 rounded-xl border bg-card p-5">
        <h3 className="font-semibold">Report received</h3>
        <p>No ambulance has been assigned. Clinical review and dispatch are separate steps.</p>
        <Link
          className="text-primary underline"
          href={`/emergency/bases/${base.id}/cases/${receive.data.id}`}
        >
          Open received case {receive.data.case_number}
        </Link>
        <Button
          onClick={() => {
            attempt.current = null;
            receive.reset();
            form.reset();
          }}
        >
          Receive another report
        </Button>
      </section>
    );
  const options = (group: Record<string, string>) =>
    Object.entries(group).map(([value, label]) => ({ value, label }));
  const rejected = receive.error instanceof ApiError && [400, 413].includes(receive.error.status);
  return (
    <section className="space-y-4 rounded-xl border bg-card p-5">
      <div>
        <h3 className="font-semibold">Receive a report</h3>
        <p className="text-sm text-muted-foreground">
          Unknown callers and patients are accepted. Record only necessary details under{" "}
          {base.policy_reference}. This does not dispatch an ambulance.
        </p>
      </div>
      <form
        noValidate
        onSubmit={form.handleSubmit((values) => receive.mutate(values))}
        className="space-y-4"
      >
        <fieldset
          disabled={receive.isPending || (receive.isError && !rejected)}
          className="grid gap-4 sm:grid-cols-2"
        >
          <FormSelectField
            control={form.control}
            name="channel"
            id="intake-channel"
            label="Report channel"
            options={options(emergencyChoices.channels)}
          />
          <FormSelectField
            control={form.control}
            name="requested_service"
            id="intake-service"
            label="Requested service (not a clinical assessment)"
            options={options(emergencyChoices.services)}
          />
          <FormSelectField
            control={form.control}
            name="processing_basis"
            id="intake-basis"
            label="Processing basis under the reviewed policy"
            options={options(emergencyChoices.bases)}
          />
          <FormSelectField
            control={form.control}
            name="contact.relationship"
            id="intake-relationship"
            label="Caller relationship"
            options={options(emergencyChoices.relationships)}
          />
          <Field
            id="intake-name"
            label="Caller name (optional)"
            error={form.formState.errors.contact?.caller_name?.message}
          >
            <Input id="intake-name" autoComplete="off" {...form.register("contact.caller_name")} />
          </Field>
          <Field
            id="intake-phone"
            label="Callback phone (optional)"
            error={form.formState.errors.contact?.callback_phone?.message}
          >
            <Input
              id="intake-phone"
              type="tel"
              autoComplete="off"
              {...form.register("contact.callback_phone")}
            />
          </Field>
          <Field
            id="intake-location"
            label="Incident location (optional)"
            error={form.formState.errors.incident?.location_text?.message}
          >
            <Textarea
              id="intake-location"
              autoComplete="off"
              {...form.register("incident.location_text")}
            />
          </Field>
          <Field
            id="intake-description"
            label="Reported situation (optional)"
            error={form.formState.errors.incident?.patient_description?.message}
          >
            <Textarea
              id="intake-description"
              autoComplete="off"
              {...form.register("incident.patient_description")}
            />
          </Field>
        </fieldset>
        {receive.isError && (
          <div role="alert" className="text-sm text-destructive">
            {receive.error.message}.{" "}
            {rejected
              ? "The report was rejected; correct its fields and submit again."
              : "Retry sends the original report with the same reference; do not create a duplicate."}
          </div>
        )}
        <Button type="submit" disabled={receive.isPending}>
          {receive.isPending ? "Receiving…" : "Receive report"}
        </Button>
      </form>
    </section>
  );
}
