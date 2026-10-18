"use client";

import { useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  apiFetch,
  emergencyBaseSchema,
  emergencyChoices,
  emergencyGrantSchema,
  newEmergencyId,
  revokeEmergencyGrant,
  useApiMutation,
  type EmergencyBase,
  type EmergencyBaseInput,
} from "@helivanta/api";
import { ConfirmDialog, Field, FormSelectField, useZodForm } from "@helivanta/ui";
import type { z } from "zod";

export function EmergencyBaseAdmin({ base }: { base?: EmergencyBase }) {
  const id = useRef(base?.id ?? newEmergencyId());
  const form = useZodForm(
    emergencyBaseSchema,
    base ?? {
      name: "",
      timezone: "Asia/Kolkata",
      policy_reference: "",
      contact_retention_days: 30,
    },
  );
  const grant = useZodForm(emergencyGrantSchema, { subject: "", level: "reader" });
  const [revokeSubject, setRevokeSubject] = useState<string | null>(null);
  const save = useApiMutation(
    (values: EmergencyBaseInput) =>
      apiFetch<EmergencyBase>(`/emergency/bases/${id.current}`, {
        method: "PUT",
        body: JSON.stringify(values),
      }),
    { successToast: "Base saved", invalidate: [["emergency", "bases"]] },
  );
  const allow = useApiMutation(
    (values: z.infer<typeof emergencyGrantSchema>) =>
      apiFetch(`/emergency/bases/${id.current}/access/${encodeURIComponent(values.subject)}`, {
        method: "PUT",
        body: JSON.stringify({ level: values.level }),
      }),
    { successToast: "Access granted", invalidate: [["emergency", "bases"]] },
  );
  const revoke = useApiMutation((subject: string) => revokeEmergencyGrant(id.current, subject), {
    successToast: "Access revoked",
    invalidate: [["emergency"]],
    onSuccess: () => setRevokeSubject(null),
  });
  return (
    <section className="space-y-5 rounded-xl border bg-card p-5">
      <h3 className="font-semibold">
        {base ? `Configure ${base.name}` : "Configure an operating base"}
      </h3>
      <p className="text-sm text-muted-foreground">
        Use an approved policy and retention period. Administrator privileges do not grant case
        access.
      </p>
      <form
        noValidate
        onSubmit={form.handleSubmit((values) => save.mutate(values))}
        className="grid gap-4 sm:grid-cols-2"
      >
        <Field
          id="base-name"
          label="Operating base name"
          error={form.formState.errors.name?.message}
        >
          <Input id="base-name" {...form.register("name")} />
        </Field>
        <Field
          id="base-timezone"
          label="IANA timezone"
          error={form.formState.errors.timezone?.message}
        >
          <Input id="base-timezone" {...form.register("timezone")} />
        </Field>
        <Field
          id="base-policy"
          label="Reviewed policy reference"
          error={form.formState.errors.policy_reference?.message}
        >
          <Input id="base-policy" {...form.register("policy_reference")} />
        </Field>
        <Field
          id="base-retention"
          label="Caller contact retention (days)"
          error={form.formState.errors.contact_retention_days?.message}
        >
          <Input
            id="base-retention"
            type="number"
            {...form.register("contact_retention_days", { valueAsNumber: true })}
          />
        </Field>
        <Button type="submit" disabled={save.isPending}>
          Save base
        </Button>
      </form>
      {(base || save.data) && (
        <form
          noValidate
          className="space-y-4 border-t pt-4"
          onSubmit={grant.handleSubmit((values) => allow.mutate(values))}
        >
          <p className="text-sm text-muted-foreground">
            Verify the subject belongs to this tenant and holds the matching emergency role. A base
            grant alone never grants API permissions.
          </p>
          <Field
            id="grant-subject"
            label="Verified identity subject"
            error={grant.formState.errors.subject?.message}
          >
            <Input id="grant-subject" autoComplete="off" {...grant.register("subject")} />
          </Field>
          <FormSelectField
            control={grant.control}
            name="level"
            id="grant-level"
            label="Base access level"
            options={Object.entries(emergencyChoices.access_levels).map(([value, label]) => ({
              value,
              label,
            }))}
          />
          <div className="flex flex-wrap gap-3">
            <Button type="submit" disabled={allow.isPending || revoke.isPending}>
              Grant access
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={revoke.isPending}
              onClick={() => {
                void grant.handleSubmit((values) => setRevokeSubject(values.subject))();
              }}
            >
              Revoke access
            </Button>
          </div>
        </form>
      )}
      <ConfirmDialog
        open={revokeSubject !== null}
        onOpenChange={(open) => {
          if (!open) setRevokeSubject(null);
        }}
        title="Revoke base access?"
        description="This subject will lose access to this base's cases immediately. Their other grants remain independent."
        confirmLabel="Confirm revoke"
        busy={revoke.isPending}
        onConfirm={() => {
          if (revokeSubject) revoke.mutate(revokeSubject);
        }}
      />
    </section>
  );
}
