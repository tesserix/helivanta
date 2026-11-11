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
  credentialRequest,
  parseReminderDays,
  saveCredentialReminderPolicy,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type CredentialOwner,
  type CredentialReminder,
  type CredentialReminderPolicy,
  type CredentialReminderStatus,
  type Hospital,
} from "@helivanta/api";
import {
  Field,
  LoadMore,
  PageHeading,
  SelectControl,
  useZodForm,
  RequestFailure,
} from "@helivanta/ui";
import { inputClass } from "../roster-policy/policy-ui";
import { credentialPlace, useAccessChange } from "../credential-panel";

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
      <PageHeading
        description="Renewal reminders for approved licences and registrations, raised on the hospital calendar at the days before expiry you choose."
        actions={
          hospital && (
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
          )
        }
      />
      {hospitals.isPending ? (
        <p role="status">Loading hospitals…</p>
      ) : hospitals.isError ? (
        <RequestFailure
          message="Could not load hospitals."
          retry={hospitals.retry}
          error={hospitals.error}
        />
      ) : hospital ? (
        <CredentialReminders
          key={hospital.id}
          owner={{ hospitalId: hospital.id }}
          ownerName={hospital.name}
        />
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

type Owned = { owner: CredentialOwner; onContextChanged?: () => void };

export function CredentialReminders({
  owner,
  ownerName,
  onContextChanged,
}: Owned & { ownerName: string }) {
  const { can } = usePermissions();
  const allowed = can("workforce.enrollment.manage");
  const policy = useApiQuery<CredentialReminderPolicy>(
    credentialReminderPolicyKey(owner),
    credentialReminderPolicyPath(owner),
    { enabled: allowed, gcTime: 0, refetchOnWindowFocus: false, request: credentialRequest(owner) },
  );
  useAccessChange(onContextChanged, policy.error);
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
        <RequestFailure
          message="Could not load the reminder days."
          retry={() => void policy.refetch()}
          error={policy.error}
        />
      ) : (
        <PolicyForm
          key={policy.data.version}
          {...{ owner, onContextChanged }}
          policy={policy.data}
        />
      )}
      <Inbox {...{ owner, ownerName, onContextChanged }} />
    </div>
  );
}

function PolicyForm({
  owner,
  onContextChanged,
  policy,
}: Owned & { policy: CredentialReminderPolicy }) {
  const form = useZodForm(credentialReminderPolicyFormSchema, {
    days: policy.thresholds.join(", "),
  });
  const save = useApiMutation(
    (days: number[]) => saveCredentialReminderPolicy(owner, policy.version, days),
    {
      invalidate: [credentialReminderPolicyKey(owner)],
      successToast: "Reminder days saved",
      suppressErrorToast: true,
    },
  );
  useAccessChange(onContextChanged, save.error);
  return (
    <section aria-label="Reminder days" className="rounded-2xl border bg-card p-6">
      <h3 className="text-lg font-semibold">Reminder days</h3>
      <p className="mt-1 text-sm text-muted-foreground">
        {policy.version === 0
          ? `Reminders are off at this ${credentialPlace(owner)} until you set the days before expiry to raise them.`
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

function Inbox({ owner, ownerName, onContextChanged }: Owned & { ownerName: string }) {
  const [status, setStatus] = useState<CredentialReminderStatus>("open");
  const rows = useApiPagedQuery<CredentialReminder>(
    [...credentialRemindersKey(owner), status],
    credentialRemindersPath(owner, status),
    { request: credentialRequest(owner) },
  );
  const acknowledge = useApiMutation(
    (reminder: string) => acknowledgeCredentialReminder(owner, reminder),
    {
      invalidate: [credentialRemindersKey(owner)],
      successToast: "Reminder acknowledged",
    },
  );
  useAccessChange(onContextChanged, rows.error, acknowledge.error);
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
        <RequestFailure message="Could not load reminders." retry={rows.retry} error={rows.error} />
      ) : rows.items.length === 0 ? (
        <p className="rounded-xl border p-6 text-sm">
          No {statuses.find((s) => s.value === status)?.label.toLowerCase()} reminders.
        </p>
      ) : (
        <div className="overflow-x-auto rounded-2xl border bg-card">
          <table className="w-full text-left text-sm">
            <caption className="sr-only">Credential reminders at {ownerName}</caption>
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
