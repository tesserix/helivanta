"use client";
import { useState } from "react";
import { Button } from "@tesserix/web";
import {
  ApiError,
  acknowledgeCredentialReminder,
  credentialReminderPolicyFormSchema,
  credentialReminderPolicyKey,
  credentialReminderPolicyPath,
  credentialRemindersKey,
  credentialRemindersPath,
  parseReminderDays,
  saveCredentialReminderPolicy,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type CredentialReminder,
  type CredentialReminderPolicy,
  type CredentialReminderStatus,
  type Hospital,
} from "@helivanta/api";
import { Field, LoadMore, SelectControl, useZodForm } from "@helivanta/ui";
import { Failure, inputClass } from "../roster-policy/policy-ui";

const statuses: { value: CredentialReminderStatus; label: string }[] = [
  { value: "open", label: "Open" },
  { value: "acknowledged", label: "Acknowledged" },
  { value: "superseded", label: "Superseded" },
];

function failureText(error: Error) {
  if (error instanceof ApiError && error.status === 409) {
    if (error.detail.reason === "version_conflict")
      return "Someone changed the reminder days since you opened them. Reload to see the latest policy.";
    if (error.detail.reason === "reminder_superseded")
      return "This reminder was replaced by a newer one.";
  }
  return error.message;
}

export function CredentialRemindersWorkspace() {
  const hospitals = useApiPagedQuery<Hospital>(
    ["workforce", "planning-hospitals"],
    "/workforce/hospitals",
  );
  const managed = hospitals.items.filter((h) => h.access_level === "manager");
  const [selected, setSelected] = useState("");
  const hospital = managed.find((h) => h.id === selected) ?? managed[0];
  return (
    <>
      <div className="mb-8 flex flex-wrap items-end justify-between gap-6">
        <div>
          <p className="mb-2 text-xs font-semibold text-(--hms-accent)">People & operations</p>
          <h2 className="text-3xl font-semibold tracking-tight">Credential reminders</h2>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            Renewal reminders for approved licences and registrations, raised on the hospital
            calendar at the days before expiry you choose.
          </p>
        </div>
        {hospital && (
          <div className="w-full sm:w-72">
            <Field id="reminder-hospital" label="Hospital">
              <SelectControl
                id="reminder-hospital"
                value={hospital.id}
                onValueChange={setSelected}
                options={managed.map((h) => ({ value: h.id, label: h.name }))}
              />
            </Field>
          </div>
        )}
      </div>
      {hospitals.isPending ? (
        <p role="status">Loading hospitals…</p>
      ) : hospitals.isError ? (
        <Failure message="Could not load hospitals." retry={hospitals.retry} />
      ) : hospital ? (
        <Reminders key={hospital.id} hospital={hospital} />
      ) : (
        <p className="rounded-2xl border bg-card p-8">
          Ask your tenant administrator for hospital management access to manage credential
          reminders.
        </p>
      )}
      {hospitals.hasMore && (
        <Button
          className="mt-4"
          variant="outline"
          disabled={hospitals.isFetchingMore}
          onClick={hospitals.loadMore}
        >
          Load more hospitals
        </Button>
      )}
    </>
  );
}

function Reminders({ hospital }: { hospital: Hospital }) {
  const { can } = usePermissions();
  const allowed = can("workforce.enrollment.manage");
  const policy = useApiQuery<CredentialReminderPolicy>(
    credentialReminderPolicyKey(hospital.id),
    credentialReminderPolicyPath(hospital.id),
    { enabled: allowed, gcTime: 0, refetchOnWindowFocus: false, request: { cache: "no-store" } },
  );
  if (!allowed)
    return (
      <p role="alert" className="rounded-2xl border bg-card p-8">
        You need enrollment management access to manage credential reminders.
      </p>
    );
  return (
    <div className="space-y-10">
      {policy.isPending ? (
        <p role="status">Loading reminder days…</p>
      ) : policy.isError ? (
        <Failure message="Could not load the reminder days." retry={() => void policy.refetch()} />
      ) : (
        <PolicyForm key={policy.data.version} hospital={hospital} policy={policy.data} />
      )}
      <Inbox hospital={hospital} />
    </div>
  );
}

function PolicyForm({
  hospital,
  policy,
}: {
  hospital: Hospital;
  policy: CredentialReminderPolicy;
}) {
  const form = useZodForm(credentialReminderPolicyFormSchema, {
    days: policy.thresholds.join(", "),
  });
  const save = useApiMutation(
    (days: number[]) => saveCredentialReminderPolicy(hospital.id, policy.version, days),
    {
      invalidate: [credentialReminderPolicyKey(hospital.id)],
      successToast: "Reminder days saved",
      suppressErrorToast: true,
    },
  );
  return (
    <section aria-label="Reminder days" className="rounded-2xl border bg-card p-6">
      <h3 className="text-lg font-semibold">Reminder days</h3>
      <p className="mt-1 text-sm text-muted-foreground">
        {policy.version === 0
          ? "Reminders are off at this hospital until you set the days before expiry to raise them."
          : `Version ${policy.version}: a reminder is raised ${policy.thresholds.join(", ")} days before each expiry.`}
      </p>
      <form
        noValidate
        autoComplete="off"
        aria-label="Reminder days"
        className="mt-4 flex flex-wrap items-end gap-3"
        onSubmit={form.handleSubmit((values) => save.mutate(parseReminderDays(values.days)))}
      >
        <div className="w-full sm:w-72">
          <Field
            id="reminder-days"
            label="Days before expiry"
            error={form.formState.errors.days?.message}
          >
            <input
              id="reminder-days"
              placeholder="90, 30, 0"
              className={inputClass}
              {...form.register("days")}
            />
          </Field>
        </div>
        <Button type="submit" disabled={save.isPending}>
          Save reminder days
        </Button>
      </form>
      {save.error && (
        <p role="alert" className="mt-3 text-sm text-destructive">
          {failureText(save.error)}
        </p>
      )}
    </section>
  );
}

function Inbox({ hospital }: { hospital: Hospital }) {
  const [status, setStatus] = useState<CredentialReminderStatus>("open");
  const rows = useApiPagedQuery<CredentialReminder>(
    [...credentialRemindersKey(hospital.id), status],
    credentialRemindersPath(hospital.id, status),
    { request: { cache: "no-store" } },
  );
  const acknowledge = useApiMutation(
    (reminder: string) => acknowledgeCredentialReminder(hospital.id, reminder),
    { invalidate: [credentialRemindersKey(hospital.id)], successToast: "Reminder acknowledged" },
  );
  return (
    <section aria-label="Reminder inbox" className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <h3 className="text-lg font-semibold">Inbox</h3>
        <div className="w-full sm:w-56">
          <Field id="reminder-status" label="Status">
            <SelectControl
              id="reminder-status"
              value={status}
              onValueChange={(value) => setStatus(value as CredentialReminderStatus)}
              options={statuses}
            />
          </Field>
        </div>
      </div>
      {acknowledge.error && (
        <p role="alert" className="text-sm text-destructive">
          {failureText(acknowledge.error)}
        </p>
      )}
      {rows.isPending ? (
        <p role="status">Loading reminders…</p>
      ) : rows.isError ? (
        <Failure message="Could not load reminders." retry={rows.retry} />
      ) : rows.items.length === 0 ? (
        <p className="rounded-xl border p-6 text-sm">
          No {statuses.find((s) => s.value === status)?.label.toLowerCase()} reminders.
        </p>
      ) : (
        <div className="overflow-x-auto rounded-2xl border bg-card">
          <table className="w-full text-left text-sm">
            <caption className="sr-only">Credential reminders at {hospital.name}</caption>
            <thead className="bg-muted/40 text-xs text-muted-foreground">
              <tr>
                {["Staff", "Credential", "Valid until", "Due", "Status", ""].map((label, i) => (
                  <th key={i} scope="col" className="px-4 py-3 font-medium">
                    {label === "" ? <span className="sr-only">Actions</span> : label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y">
              {rows.items.map((row) => (
                <tr key={row.id}>
                  <td className="px-4 py-3">
                    {row.display_name}
                    <span className="block font-mono text-xs text-muted-foreground">
                      {row.employee_code}
                    </span>
                  </td>
                  <td className="px-4 py-3">{row.type}</td>
                  <td className="px-4 py-3">{row.valid_until}</td>
                  <td className="px-4 py-3">
                    {row.due_on}
                    <span className="block text-xs text-muted-foreground">
                      {row.threshold_days === 0
                        ? "On expiry"
                        : `${row.threshold_days} days before expiry`}
                    </span>
                  </td>
                  <td className="px-4 py-3">
                    {row.status === "acknowledged"
                      ? `Acknowledged by ${row.acknowledged_by_subject ?? "unknown"}`
                      : statuses.find((s) => s.value === row.status)?.label}
                  </td>
                  <td className="px-4 py-3 text-right">
                    {row.status === "open" && (
                      <Button
                        variant="outline"
                        disabled={acknowledge.isPending}
                        onClick={() => acknowledge.mutate(row.id)}
                        aria-label={`Acknowledge ${row.type} reminder for ${row.display_name}`}
                      >
                        Acknowledge
                      </Button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          <LoadMore
            hasMore={rows.hasMore}
            isLoading={rows.isFetchingMore}
            onClick={rows.loadMore}
          />
        </div>
      )}
    </section>
  );
}
