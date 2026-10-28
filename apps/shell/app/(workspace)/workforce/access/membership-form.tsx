"use client";

import { useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Field, Panel, useZodForm } from "@helivanta/ui";
import {
  ApiError,
  enrollStaffMember,
  staffMembershipSubjectContract,
  useApiMutation,
} from "@helivanta/api";

const schema = z.object({
  subject: z
    .string()
    .trim()
    .min(1, "Enter an existing account ID.")
    .pipe(staffMembershipSubjectContract),
});

export function StaffMembershipForm({ tenantId }: { tenantId: string }) {
  const form = useZodForm(schema, { subject: "" });
  const [submitted, setSubmitted] = useState<string | null>(null);
  const save = useApiMutation((subject: string) => enrollStaffMember(tenantId, subject), {
    suppressErrorToast: true,
  });
  const refused =
    save.error instanceof ApiError && [400, 401, 403, 404, 409].includes(save.error.status);

  return (
    <Panel className="mb-6 space-y-4">
      <h2 className="text-lg font-semibold">Enroll existing account</h2>
      <p>
        Enroll an existing login identity as a staff member of this organization. This grants no
        business permissions or hospital access. Staff records, reviewed account links and access
        assignments are separate steps.
      </p>
      {save.isSuccess ? (
        <>
          <p role="status">
            Membership recorded for {submitted}. Membership ID: {save.data.id}.
          </p>
          <p>
            Access synchronization may still be pending. This does not verify login or activate
            hospital access.
          </p>
          <Button
            variant="outline"
            onClick={() => {
              save.reset();
              setSubmitted(null);
              form.reset();
            }}
          >
            Enroll another account
          </Button>
        </>
      ) : (
        <form
          aria-label="Staff membership enrollment"
          noValidate
          className="space-y-4"
          onSubmit={form.handleSubmit(({ subject }) => {
            const command = submitted ?? subject;
            setSubmitted(command);
            save.mutate(command);
          })}
        >
          <Field
            id="membership-subject"
            label="Existing account ID"
            error={form.formState.errors.subject?.message}
          >
            <Input
              id="membership-subject"
              autoComplete="off"
              disabled={submitted !== null}
              aria-invalid={!!form.formState.errors.subject}
              aria-describedby={
                form.formState.errors.subject ? "membership-subject-error" : undefined
              }
              {...form.register("subject")}
            />
          </Field>
          <p>
            Use the identity provider’s user ID. An email address or staff record ID is not a
            substitute.
          </p>
          {save.isError && (
            <p role="alert">
              {refused
                ? `${save.error.message} Refresh the page and verify the account and workspace before continuing.`
                : "The membership may have been recorded. Retry the same request to reconcile the result."}
            </p>
          )}
          <Button type="submit" disabled={save.isPending || refused}>
            {save.isPending
              ? "Enrolling…"
              : save.isError
                ? "Retry same enrollment"
                : "Enroll staff member"}
          </Button>
        </form>
      )}
    </Panel>
  );
}
