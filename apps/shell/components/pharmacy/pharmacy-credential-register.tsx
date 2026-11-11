"use client";

import { useEffect, useId, useRef } from "react";
import { X } from "lucide-react";
import { Button } from "@tesserix/web";
import {
  ApiError,
  credentialRegisterKey,
  credentialRemindersKey,
  pharmacyTimeZoneFormSchema,
  pharmacyTimeZoneKey,
  savePharmacyTimeZone,
  useApiMutation,
  usePharmacyTimeZone,
  type EnrollmentRequestScope,
  type PharmacyCredentialOwner,
  type PharmacyStaffSource,
  type PharmacyTimeZone,
} from "@helivanta/api";
import { Field, Panel, RefreshButton, RequestFailure, useZodForm } from "@helivanta/ui";
import { useAccessChange } from "@/app/(workspace)/workforce/credential-panel";
import { CredentialRegister } from "@/app/(workspace)/workforce/credentials/register-workspace";
import { CredentialReminders } from "@/app/(workspace)/workforce/credential-reminders/reminders-workspace";
import { inputClass } from "@/app/(workspace)/workforce/roster-policy/policy-ui";

type Props = {
  scope: EnrollmentRequestScope;
  pharmacy: PharmacyStaffSource;
  onContextChanged: () => void;
  onClose: () => void;
};

// The pharmacy's own calendar decides credential standing, the expiry filter and reminders (ADR-0054).
export function PharmacyCredentialRegister({ scope, pharmacy, onContextChanged, onClose }: Props) {
  const owner: PharmacyCredentialOwner = {
    pharmacyId: pharmacy.pharmacy_id,
    tenantId: scope.tenantId,
    subject: scope.subject,
  };
  const zone = usePharmacyTimeZone(owner);
  useAccessChange(onContextChanged, zone.error);
  const headingId = useId();
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.scrollIntoView({ block: "start", behavior: "smooth" });
    heading.current?.focus();
  }, []);
  return (
    <Panel flush aria-labelledby={headingId} className="mt-6 scroll-mt-24">
      <header className="flex items-start justify-between gap-4 border-b px-6 py-5">
        <div>
          <h3
            id={headingId}
            ref={heading}
            tabIndex={-1}
            className="text-xl font-semibold outline-none"
          >
            Credential register
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">{pharmacy.name}</p>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose} aria-label="Close credential register">
          <X className="size-4" />
        </Button>
      </header>
      <div className="space-y-10 p-6">
        {zone.isPending ? (
          <p role="status">Loading the pharmacy time zone…</p>
        ) : zone.isError ? (
          <RequestFailure
            message="Could not load the pharmacy time zone."
            retry={() => void zone.refetch()}
            error={zone.error}
          />
        ) : (
          <>
            <TimeZoneForm
              key={zone.data.version}
              {...{ owner, onContextChanged }}
              zone={zone.data}
              refresh={zone.refetch}
              readError={zone.error}
              reloading={zone.isFetching}
            />
            <CredentialRegister
              {...{ owner, onContextChanged }}
              ownerName={pharmacy.name}
              filterBlocked={
                zone.data.version === 0
                  ? "Set this pharmacy's time zone to filter credentials by expiry."
                  : undefined
              }
            />
            <CredentialReminders {...{ owner, onContextChanged }} ownerName={pharmacy.name} />
          </>
        )}
      </div>
    </Panel>
  );
}

function TimeZoneForm({
  owner,
  zone,
  onContextChanged,
  refresh,
  readError,
  reloading,
}: {
  owner: PharmacyCredentialOwner;
  zone: PharmacyTimeZone;
  onContextChanged: () => void;
  refresh: () => unknown;
  readError: unknown;
  reloading: boolean;
}) {
  const form = useZodForm(pharmacyTimeZoneFormSchema, { time_zone: zone.time_zone ?? "" });
  const save = useApiMutation(
    (timeZone: string) => savePharmacyTimeZone(owner, zone.version, timeZone),
    {
      invalidate: [
        pharmacyTimeZoneKey(owner),
        credentialRegisterKey(owner),
        credentialRemindersKey(owner),
      ],
      successToast: "Time zone saved",
      suppressErrorToast: true,
    },
  );
  useAccessChange(onContextChanged, save.error);
  const stale =
    save.error instanceof ApiError &&
    save.error.status === 409 &&
    save.error.detail.reason === "version_conflict";
  const error = form.formState.errors.time_zone?.message;
  return (
    <div className="space-y-3">
      <h4 className="text-lg font-semibold">Time zone</h4>
      <p className="text-sm text-muted-foreground">
        {zone.version === 0
          ? "No time zone is saved. Until one is, credentials show no standing, the expiry filter is off and no reminders are raised."
          : `Version ${zone.version}: expiry standing, the register filter and reminders use ${zone.time_zone}.`}
      </p>
      <p className="text-xs text-muted-foreground">A change reaches reminders within a day.</p>
      <form
        noValidate
        autoComplete="off"
        aria-label="Pharmacy time zone"
        className="flex flex-wrap items-end gap-3"
        onSubmit={form.handleSubmit((values) => save.mutate(values.time_zone))}
      >
        <div className="w-full sm:w-72">
          <Field id="pharmacy-time-zone" label="Time zone" error={error}>
            <input
              id="pharmacy-time-zone"
              placeholder="Asia/Kolkata"
              aria-invalid={!!error}
              disabled={save.isPending}
              className={inputClass}
              {...form.register("time_zone")}
            />
          </Field>
        </div>
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : "Save time zone"}
        </Button>
      </form>
      {save.error && (
        <p role="alert" className="text-sm text-destructive">
          {save.error.message}
        </p>
      )}
      {stale && (
        <RefreshButton refresh={refresh} error={readError} busy={reloading}>
          Reload time zone
        </RefreshButton>
      )}
    </div>
  );
}
