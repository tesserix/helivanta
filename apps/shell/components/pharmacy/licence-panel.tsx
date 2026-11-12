"use client";

import { useEffect, useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  downloadLicenceEvidence,
  newPharmacyId,
  pharmacyKey,
  pharmacyLicencesRoot,
  pharmacyLicenceEvidenceRoot,
  pharmacyLicenceInputSchema,
  pharmacyLicenceReviewInputSchema,
  pharmacyLicenceRenewalNoticeSchema,
  prepareLicenceEvidence,
  recordPharmacyLicence,
  retryLicenceEvidence,
  reviewPharmacyLicence,
  submitLicenceEvidence,
  useApiMutation,
  useApiPagedQuery,
  useApiQuery,
  usePermissions,
  type Page,
  type LicenceEvidenceUpload,
  type PharmacyLicence,
  type PharmacyLicenceEvidence,
  type PharmacyLicenceInput,
  type PharmacyLicenceRenewalNotice,
  type PharmacyScope,
} from "@helivanta/api";
import {
  Field,
  FormSelectField,
  LoadMore,
  Panel,
  RefreshButton,
  RequestFailure,
  useZodForm,
} from "@helivanta/ui";
import { useReportState, type FormState } from "@/lib/use-list-detail";

type Reports = (key: string) => FormState;

function useEvidenceController() {
  const controllerRef = useRef(new AbortController());
  useEffect(() => {
    // Development Strict Mode replays setup after cleanup. Each replay needs a
    // live signal; cleanup still aborts the latest user-selected file command.
    if (controllerRef.current.signal.aborted) controllerRef.current = new AbortController();
    return () => controllerRef.current.abort();
  }, []);
  return controllerRef;
}
const noticeLabels: Record<PharmacyLicenceRenewalNotice["date_status"], string> = {
  not_yet_valid: "Not yet within declared validity",
  expired: "Declared validity has expired",
  expires_today: "Declared validity ends today",
  renewal_due_30: "Renewal due within 30 days",
  renewal_due_90: "Renewal due within 90 days",
  renewal_due_180: "Renewal due within 180 days",
  current: "Within declared validity",
};

export function LicencePanel({
  scope,
  subject,
  today,
  formState,
}: {
  scope: PharmacyScope;
  subject: string;
  today: string;
  formState?: Reports;
}) {
  const { can } = usePermissions();
  const licences = useApiPagedQuery<PharmacyLicence>(
    pharmacyKey(scope, "licences"),
    pharmacyLicencesRoot(scope),
  );
  const renewals = useApiPagedQuery<PharmacyLicenceRenewalNotice>(
    pharmacyKey(scope, "licences", "renewals"),
    `${pharmacyLicencesRoot(scope)}/renewals`,
  );
  const notices = renewals.isError
    ? []
    : renewals.items.flatMap((value) => {
        const parsed = pharmacyLicenceRenewalNoticeSchema.safeParse(value);
        return parsed.success ? [parsed.data] : [];
      });
  const invalidNotice = notices.length !== renewals.items.length;
  return (
    <Panel className="space-y-5">
      <div>
        <h3 className="font-semibold">Drug licences</h3>
        <p className="mt-1 text-sm text-muted-foreground">
          Record the issued details and retain a scanned image. Another authorized owner reviews the
          declaration.
        </p>
      </div>
      {licences.isError ? (
        <RequestFailure
          message="Could not load this outlet's licences. Check your outlet access and try again."
          retry={licences.retry}
          error={licences.error}
        />
      ) : licences.isPending ? (
        <p role="status">Loading licences…</p>
      ) : (
        <>
          {(renewals.isError || invalidNotice) && (
            <RequestFailure
              message="Renewal advice is unavailable."
              retry={renewals.retry}
              error={renewals.error ?? new Error("The renewal advice response is invalid.")}
            />
          )}
          {licences.items.length === 0 && (
            <p className="text-sm">No licence declaration recorded.</p>
          )}
          {licences.items.map((licence) => (
            <LicenceRecord
              key={licence.id}
              scope={scope}
              subject={subject}
              licence={licence}
              notice={notices.find((notice) => notice.licence_id === licence.id)}
              canReview={can("pharmacy.licence.review")}
              canManage={can("pharmacy.licence.manage")}
              canReadEvidence={can("pharmacy.licence.evidence.read")}
              formState={formState}
            />
          ))}
          <LoadMore
            label="Load more licences"
            hasMore={licences.hasMore}
            isLoading={licences.isFetchingMore}
            onClick={() => {
              licences.loadMore();
              renewals.loadMore();
            }}
          />
          <LoadMore
            label="Load more expiry reminders"
            hasMore={renewals.hasMore}
            isLoading={renewals.isFetchingMore}
            onClick={renewals.loadMore}
          />
          <RefreshButton
            refresh={renewals.retry}
            error={renewals.error}
            busy={renewals.isRefreshing}
          >
            Refresh expiry reminders
          </RefreshButton>
          {can("pharmacy.licence.manage") && (
            <LicenceForm scope={scope} today={today} report={formState?.("licence-create")} />
          )}
        </>
      )}
    </Panel>
  );
}

function LicenceForm({
  scope,
  today,
  report,
}: {
  scope: PharmacyScope;
  today: string;
  report?: FormState;
}) {
  const defaults: PharmacyLicenceInput = {
    licence_kind: "retail",
    form_label: "",
    licence_number: "",
    issuing_authority: "",
    valid_from: today,
    valid_until: "",
    pharmacist_name: "",
    pharmacist_registration: "",
  };
  const [command, setCommand] = useState(() => newPharmacyId());
  const form = useZodForm(pharmacyLicenceInputSchema, defaults);
  const save = useApiMutation(
    (input: PharmacyLicenceInput) => recordPharmacyLicence(scope, command, input),
    {
      successToast: "Licence declaration recorded; add its image before review",
      invalidate: [pharmacyKey(scope, "licences")],
      onSuccess: () => {
        form.reset(defaults);
        setCommand(newPharmacyId());
      },
    },
  );
  useReportState(report, form.formState.isDirty, save.isPending);
  const fields = [
    ["form_label", "Form label", "text"],
    ["licence_number", "Licence number", "text"],
    ["issuing_authority", "Issuing authority", "text"],
    ["valid_from", "Valid from", "date"],
    ["valid_until", "Valid until", "date"],
    ["pharmacist_name", "Pharmacist name", "text"],
    ["pharmacist_registration", "Pharmacist registration", "text"],
  ] as const;
  return (
    <form
      noValidate
      className="space-y-4 border-t pt-4"
      onSubmit={form.handleSubmit((input) => save.mutate(input))}
    >
      <h4 className="font-medium">Record a licence declaration</h4>
      <FormSelectField
        control={form.control}
        name="licence_kind"
        id="licence-kind"
        label="Licence kind"
        options={[
          { value: "retail", label: "Retail" },
          { value: "wholesale", label: "Wholesale" },
        ]}
      />
      <div className="grid gap-4 sm:grid-cols-2">
        {fields.map(([name, label, type]) => (
          <Field
            key={name}
            id={`licence-${name}`}
            label={label}
            error={
              form.formState.errors[name] ? `Enter a valid ${label.toLowerCase()}.` : undefined
            }
          >
            <Input id={`licence-${name}`} type={type} {...form.register(name)} />
          </Field>
        ))}
      </div>
      {save.isError && (
        <p role="alert" className="text-sm text-destructive">
          {save.error.message}
        </p>
      )}
      <Button type="submit" disabled={save.isPending}>
        {save.isPending ? "Recording…" : "Record licence declaration"}
      </Button>
    </form>
  );
}

function LicenceRecord({
  scope,
  subject,
  licence,
  notice,
  canReview,
  canManage,
  canReadEvidence,
  formState,
}: {
  scope: PharmacyScope;
  subject: string;
  licence: PharmacyLicence;
  notice?: PharmacyLicenceRenewalNotice;
  canReview: boolean;
  canManage: boolean;
  canReadEvidence: boolean;
  formState?: Reports;
}) {
  const [open, setOpen] = useState(false);
  const images = useApiQuery<Page<PharmacyLicenceEvidence>>(
    pharmacyKey(scope, "licences", licence.id, "evidence"),
    pharmacyLicenceEvidenceRoot(scope, licence.id) + "?limit=1",
    {
      enabled: open && canReadEvidence,
      poll: (snapshot) => snapshot?.data.some((file) => file.status === "scan_pending") ?? true,
    },
  );
  const image = images.data?.data[0];
  return (
    <details
      className="rounded-md border p-4"
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary className="cursor-pointer font-medium">
        {licence.licence_number} ·{" "}
        {licence.decision === "approved"
          ? "Approved"
          : licence.decision === "rejected"
            ? "Rejected"
            : "Awaiting independent review"}
      </summary>
      <div className="mt-4 space-y-4 text-sm">
        <p>
          {licence.form_label} · {licence.issuing_authority} · {licence.valid_from} to{" "}
          {licence.valid_until}
        </p>
        <p>
          {licence.pharmacist_name} · {licence.pharmacist_registration}
        </p>
        {notice ? (
          <p role="status">
            {noticeLabels[notice.date_status]}
            {notice.days_remaining > 0 ? ` · ${notice.days_remaining} days remaining` : ""}. As of{" "}
            {notice.local_date} ({notice.time_zone}). These reminders use declared dates and do not
            confirm operating authority.
          </p>
        ) : (
          <p>Renewal advice has not been loaded for this declaration.</p>
        )}
        {licence.decision && (
          <p>
            {licence.decision_reason}
            {!licence.reviewed_evidence_file_id
              ? " · Historical decision: no retained image was tied to this review."
              : ""}
          </p>
        )}
        {!canReadEvidence ? (
          <p>Ask an authorized pharmacy owner to inspect the retained evidence.</p>
        ) : images.isError ? (
          <RequestFailure
            message="Could not load the licence image."
            retry={images.refetch}
            error={images.error}
          />
        ) : images.isPending ? (
          <p role="status">Loading image status…</p>
        ) : (
          <>
            {image ? <EvidenceStatus scope={scope} image={image} /> : <p>No retained image yet.</p>}
            {!licence.decision && canManage && (!image || image.status === "upload_pending") && (
              <EvidenceUpload
                scope={scope}
                licence={licence.id}
                existing={image}
                report={formState?.(`licence-upload-${licence.id}`)}
              />
            )}
          </>
        )}
        {!licence.decision &&
          (licence.entered_by === subject ? (
            <p>Another authorized owner must review this declaration.</p>
          ) : canReview ? (
            <LicenceReview
              scope={scope}
              licence={licence.id}
              ready={image?.status === "available" && !images.isError}
              report={formState?.(`licence-review-${licence.id}`)}
            />
          ) : null)}
      </div>
    </details>
  );
}

function EvidenceStatus({
  scope,
  image,
}: {
  scope: PharmacyScope;
  image: PharmacyLicenceEvidence;
}) {
  const controllerRef = useEvidenceController();
  const retry = useApiMutation(
    () => retryLicenceEvidence(scope, image, controllerRef.current.signal),
    { invalidate: [pharmacyKey(scope, "licences")], successToast: "Scan retry requested" },
  );
  const download = useApiMutation(async () => {
    const blob = await downloadLicenceEvidence(scope, image, controllerRef.current.signal);
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = image.filename;
    document.body.appendChild(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  });
  return (
    <div className="space-y-2">
      <p>
        Image: {image.filename} ·{" "}
        {image.status === "available"
          ? "Clean scan complete"
          : image.status === "scan_pending"
            ? "Waiting for scan"
            : image.status === "scan_error"
              ? "Scan unavailable"
              : image.status === "rejected"
                ? "Image rejected; record a corrected licence declaration"
                : "Waiting for exact image upload"}
      </p>
      {image.status === "available" && (
        <Button
          type="button"
          variant="outline"
          disabled={download.isPending}
          onClick={() => download.mutate()}
        >
          Download licence image
        </Button>
      )}
      {image.status === "scan_error" && (
        <Button
          type="button"
          variant="outline"
          disabled={retry.isPending}
          onClick={() => retry.mutate()}
        >
          Retry licence scan
        </Button>
      )}
    </div>
  );
}

function EvidenceUpload({
  scope,
  licence,
  existing,
  report,
}: {
  scope: PharmacyScope;
  licence: string;
  existing?: PharmacyLicenceEvidence;
  report?: FormState;
}) {
  const [file, setFile] = useState<File | null>(null);
  const prepared = useRef<LicenceEvidenceUpload | null>(null);
  const controllerRef = useEvidenceController();
  const upload = useApiMutation(
    async () => {
      if (!file) throw new Error("Choose the licence image first.");
      const intent =
        prepared.current ??
        (await prepareLicenceEvidence(
          file,
          existing?.id ?? newPharmacyId(),
          controllerRef.current.signal,
        ));
      prepared.current = intent;
      return submitLicenceEvidence(scope, licence, intent, controllerRef.current.signal);
    },
    {
      invalidate: [pharmacyKey(scope, "licences")],
      invalidateOnError: true,
      successToast: "Licence image uploaded; waiting for its scan",
      onSuccess: () => {
        setFile(null);
        prepared.current = null;
      },
    },
  );
  useReportState(report, file !== null, upload.isPending);
  return (
    <div className="space-y-2">
      <label htmlFor={`licence-image-${licence}`} className="block space-y-2">
        Licence image (PNG or JPEG, up to 10 MiB)
        <Input
          id={`licence-image-${licence}`}
          type="file"
          accept="image/png,image/jpeg"
          disabled={upload.isPending}
          onChange={(event) => {
            controllerRef.current.abort();
            controllerRef.current = new AbortController();
            prepared.current = null;
            setFile(event.target.files?.[0] ?? null);
          }}
        />
      </label>
      {existing && (
        <p>
          Choose the original {existing.filename}; its retained intent cannot be replaced with
          different bytes.
        </p>
      )}
      <Button
        type="button"
        variant="outline"
        disabled={!file || upload.isPending}
        onClick={() => upload.mutate()}
      >
        {upload.isPending ? "Uploading…" : "Upload licence image"}
      </Button>
    </div>
  );
}

function LicenceReview({
  scope,
  licence,
  ready,
  report,
}: {
  scope: PharmacyScope;
  licence: string;
  ready: boolean;
  report?: FormState;
}) {
  const form = useZodForm(pharmacyLicenceReviewInputSchema, { decision: "approved", reason: "" });
  const review = useApiMutation(
    (input: Parameters<typeof reviewPharmacyLicence>[2]) =>
      reviewPharmacyLicence(scope, licence, input),
    {
      invalidate: [pharmacyKey(scope, "licences")],
      invalidateOnError: true,
      successToast: "Licence review recorded",
      onSuccess: () => form.reset(),
    },
  );
  useReportState(report, form.formState.isDirty, review.isPending);
  return (
    <form
      noValidate
      className="space-y-3 border-t pt-3"
      onSubmit={form.handleSubmit((input) => review.mutate(input))}
    >
      {!ready && <p>A clean retained image is required before this declaration can be reviewed.</p>}
      <FormSelectField
        control={form.control}
        name="decision"
        id={`licence-decision-${licence}`}
        label="Review decision"
        options={[
          { value: "approved", label: "Approve" },
          { value: "rejected", label: "Reject" },
        ]}
        disabled={!ready || review.isPending}
      />
      <Field
        id={`licence-reason-${licence}`}
        label="Licence review reason"
        error={form.formState.errors.reason ? "Give the reason for this review." : undefined}
      >
        <Input
          id={`licence-reason-${licence}`}
          maxLength={500}
          disabled={!ready || review.isPending}
          {...form.register("reason")}
        />
      </Field>
      <Button type="submit" disabled={!ready || review.isPending}>
        Record independent licence review
      </Button>
    </form>
  );
}
