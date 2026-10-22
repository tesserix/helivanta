"use client";

import { useEffect, useRef, useState } from "react";
import { useNow } from "@/lib/use-now";
import { z } from "zod";
import { Button } from "@tesserix/web";
import { FileCheck2, FileText, Loader2, ShieldCheck, X } from "lucide-react";
import {
  ApiError,
  useApiQuery,
  useApiMutation,
  usePermissions,
  useTenantTransition,
  onTenantTransition,
  tenantTransitionPending,
  documentQueryKey,
  useDocumentRecords,
  useStaffDocuments,
  prepareDocumentUpload,
  submitDocumentUpload,
  retryDocumentScan,
  revokeStaffDocument,
  downloadStaffDocument,
  staffDocumentSchema,
  type DocumentInput,
  type DocumentScope,
  type DocumentUpload,
  type StaffDocument,
  type Staff,
  type Hospital,
} from "@helivanta/api";
import { Field, SelectControl, ConfirmDialog, useZodForm } from "@helivanta/ui";

const fileMetadataSchema = staffDocumentSchema.pick({
  filename: true,
  content_type: true,
  size: true,
});
const fileFormSchema = z.object({
  file: z.custom<File | null>().superRefine((file, ctx) => {
    if (!file) {
      ctx.addIssue({ code: "custom", message: "Choose a file first." });
      return;
    }
    const result = fileMetadataSchema.safeParse({
      filename: file.name,
      content_type: file.type,
      size: file.size,
    });
    if (!result.success) {
      const issue = result.error.issues[0];
      ctx.addIssue({
        code: "custom",
        message:
          issue.path[0] === "filename" ? issue.message : "Choose a PNG or JPEG image up to 10 MB.",
      });
    }
  }),
});

const purposes: Record<DocumentInput["category"], string> = {
  employment_evidence: "Employment evidence",
  identity_evidence: "Identity evidence",
  credential_evidence: "Professional credential evidence",
  qualification_evidence: "Qualification evidence",
};
const states: Record<StaffDocument["status"], string> = {
  upload_pending: "Upload incomplete",
  scan_pending: "Processing",
  available: "Ready",
  scan_error: "Needs attention",
  rejected: "Rejected",
  revoked: "Revoked",
};
type Operation =
  { kind: "upload" } | { kind: "retry" | "revoke" | "download"; document: StaffDocument };

export function StaffDocumentPanel({
  hospital,
  staff,
  onClose,
}: {
  hospital: Hospital;
  staff: Staff;
  onClose: () => void;
}) {
  const { can } = usePermissions();
  const switching = useTenantTransition();
  const memberships = useApiQuery<{ data: { tenant_id: string; current: boolean }[] }>(
    ["iam", "me", "tenants"],
    "/iam/me/tenants",
  );
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.scrollIntoView({ block: "start", behavior: "smooth" });
    heading.current?.focus();
  }, []);
  const tenant = memberships.data?.data.find((item) => item.current)?.tenant_id;
  const allowed = hospital.access_level === "manager" && can("workforce.documents.manage");
  return (
    <section
      aria-labelledby="staff-documents-heading"
      className="mt-6 scroll-mt-24 overflow-hidden rounded-2xl border bg-card shadow-sm"
    >
      <header className="flex items-start justify-between gap-4 border-b px-6 py-5">
        <div>
          <h3
            id="staff-documents-heading"
            ref={heading}
            tabIndex={-1}
            className="text-xl font-semibold outline-none"
          >
            Staff documents
          </h3>
          <p className="mt-1 text-sm text-muted-foreground">
            {staff.display_name} · {staff.employee_code} · {hospital.name}
          </p>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose} aria-label="Close staff documents">
          <X className="size-4" />
        </Button>
      </header>
      {switching ? (
        <p role="status" className="p-6">
          Switching tenant. This document workspace has been cleared.
        </p>
      ) : !allowed ? (
        <p role="alert" className="p-6">
          You do not have permission to manage documents in this hospital.
        </p>
      ) : memberships.isPending ? (
        <p role="status" className="p-6">
          Confirming your active workspace…
        </p>
      ) : !tenant || memberships.isError ? (
        <div className="p-6">
          <p role="alert">Your active tenant could not be confirmed.</p>
          <Button variant="outline" onClick={() => void memberships.refetch()}>
            Try again
          </Button>
        </div>
      ) : (
        <DocumentWorkspace
          key={`${tenant}:${hospital.id}:${staff.id}`}
          scope={{ tenantId: tenant, hospitalId: hospital.id, staffId: staff.id }}
        />
      )}
    </section>
  );
}

function DocumentWorkspace({ scope }: { scope: DocumentScope }) {
  const records = useDocumentRecords(scope, true);
  const [cursor, setCursor] = useState<string | null>(null);
  const documents = useStaffDocuments(scope, cursor, true);
  const now = useNow();
  const [purpose, setPurpose] = useState<DocumentInput["category"]>("employment_evidence");
  const [recordKey, setRecordKey] = useState("");
  const form = useZodForm(fileFormSchema, { file: null });
  const file = form.watch("file");
  const [intentID, setIntentID] = useState(() => crypto.randomUUID());
  const [attempted, setAttempted] = useState(false);
  const [notice, setNotice] = useState("");
  const [resume, setResume] = useState<StaffDocument | null>(null);
  const [revoking, setRevoking] = useState<StaffDocument | null>(null);
  const prepared = useRef<DocumentUpload | null>(null);
  const controller = useRef<AbortController | null>(null);
  const retryAttempts = useRef(new Map<string, number>());
  // Render mirror of `retryAttempts`: which documents have an unconfirmed retry to replay.
  const [pendingRetries, setPendingRetries] = useState<ReadonlySet<string>>(new Set());
  const fileInput = useRef<HTMLInputElement>(null);
  const urls = useRef(new Set<string>());
  useEffect(() => {
    const currentUrls = urls.current;
    const unsubscribe = onTenantTransition(() => {
      if (tenantTransitionPending()) controller.current?.abort();
    });
    return () => {
      unsubscribe();
      controller.current?.abort();
      for (const url of currentUrls) URL.revokeObjectURL(url);
    };
  }, []);
  const options = (records.data?.records ?? []).filter((record) =>
    record.categories.includes(purpose),
  );
  const selected =
    options.find((record) => `${record.record_kind}:${record.record_id}` === recordKey) ??
    options[0];
  const storageAvailable = records.data?.uploads_available === true;

  function resetUpload() {
    form.reset({ file: null });
    setResume(null);
    prepared.current = null;
    setAttempted(false);
    setIntentID(crypto.randomUUID());
    if (fileInput.current) fileInput.current.value = "";
  }
  const action = useApiMutation(
    async (operation: Operation) => {
      if (tenantTransitionPending()) throw new DOMException("Workspace changed", "AbortError");
      const active = new AbortController();
      controller.current = active;
      let result: StaffDocument;
      if (operation.kind === "upload") {
        if (!file || (!selected && !resume))
          throw new Error("Choose a saved record and file first.");
        setAttempted(true);
        const binding = resume
          ? {
              record_kind: resume.record_kind,
              record_id: resume.record_id,
              label: "Original upload record",
              categories: [resume.category],
            }
          : selected!;
        prepared.current ??= await prepareDocumentUpload(
          resume?.id ?? intentID,
          file,
          binding,
          resume?.category ?? purpose,
          active.signal,
        );
        if (
          resume &&
          (prepared.current.input.sha256 !== resume.sha256 ||
            prepared.current.input.filename !== resume.filename ||
            prepared.current.input.size !== resume.size ||
            prepared.current.input.content_type !== resume.content_type)
        )
          throw new Error("Choose the same original file to resume this upload.");
        result = await submitDocumentUpload(scope, prepared.current, active.signal);
      } else if (operation.kind === "retry") {
        const attempt =
          retryAttempts.current.get(operation.document.id) ?? operation.document.scan_attempt + 1;
        retryAttempts.current.set(operation.document.id, attempt);
        setPendingRetries(new Set(retryAttempts.current.keys()));
        result = await retryDocumentScan(scope, operation.document.id, attempt, active.signal);
        retryAttempts.current.delete(operation.document.id);
        setPendingRetries(new Set(retryAttempts.current.keys()));
      } else if (operation.kind === "revoke") {
        result = await revokeStaffDocument(scope, operation.document.id, active.signal);
      } else {
        const blob = await downloadStaffDocument(scope, operation.document, active.signal);
        active.signal.throwIfAborted();
        const url = URL.createObjectURL(blob);
        urls.current.add(url);
        const link = document.createElement("a");
        link.href = url;
        link.download = operation.document.filename;
        link.click();
        window.setTimeout(() => {
          URL.revokeObjectURL(url);
          urls.current.delete(url);
        }, 0);
        result = operation.document;
      }
      active.signal.throwIfAborted();
      return { kind: operation.kind, result };
    },
    {
      suppressErrorToast: true,
      invalidate: [documentQueryKey(scope)],
      onSuccess: ({ kind, result }) => {
        if (tenantTransitionPending()) return;
        if (kind === "upload") {
          resetUpload();
          setNotice(
            result.status === "scan_pending"
              ? "File received. Processing will continue securely."
              : "The existing upload request has been confirmed.",
          );
        }
        if (kind === "retry") setNotice("The scan request has been confirmed.");
        if (kind === "revoke") {
          setRevoking(null);
          setNotice("Download access has been revoked.");
        }
      },
    },
  );
  const locked = attempted || action.isPending;
  const error =
    action.isError && action.error.name !== "AbortError"
      ? action.error instanceof ApiError
        ? action.error.message
        : action.error.message.startsWith("Choose")
          ? action.error.message
          : "The file action could not be completed. Try again."
      : null;

  function selectFile(value: File | undefined) {
    action.reset();
    setNotice("");
    prepared.current = null;
    setAttempted(false);
    setIntentID(resume?.id ?? crypto.randomUUID());
    if (!value) {
      form.setValue("file", null);
      return;
    }
    form.setValue("file", value, { shouldValidate: true });
  }
  return (
    <div className="p-6">
      <div className="mb-6 flex items-start gap-3 rounded-xl bg-muted/40 p-4 text-sm">
        <ShieldCheck className="mt-0.5 size-5 shrink-0 text-(--hms-accent)" />
        <p>
          Files are checked before they can be downloaded. Access stays within this hospital’s
          authorised staff-document team.
        </p>
      </div>
      {records.isPending ? (
        <p role="status">Loading saved record choices…</p>
      ) : records.isError ? (
        <div role="alert" className="mb-5">
          Could not load document choices.{" "}
          <Button variant="outline" onClick={() => void records.refetch()}>
            Try again
          </Button>
        </div>
      ) : (
        <>
          {!storageAvailable && (
            <p role="status" className="mb-5 rounded-xl border bg-muted/30 p-4 text-sm">
              File storage is not available. Existing records are shown; file actions are
              temporarily disabled.
            </p>
          )}
          <div className="rounded-xl border p-5">
            <h4 className="mb-4 flex items-center gap-2 font-semibold">
              <FileCheck2 className="size-4 text-(--hms-accent)" />
              {resume ? "Resume incomplete upload" : "Attach evidence"}
            </h4>
            {resume ? (
              <p className="mb-4 text-sm">
                Choose the same original file:{" "}
                <span className="font-medium">{resume.filename}</span>. Its record and purpose will
                stay unchanged.
              </p>
            ) : (
              <div className="mb-4 grid gap-4 sm:grid-cols-2">
                <Field id="document-purpose" label="Evidence purpose">
                  <SelectControl
                    id="document-purpose"
                    value={purpose}
                    disabled={locked || !storageAvailable}
                    onValueChange={(value) => {
                      setPurpose(value as DocumentInput["category"]);
                      setRecordKey("");
                      prepared.current = null;
                      setIntentID(crypto.randomUUID());
                      action.reset();
                    }}
                    options={Object.entries(purposes).map(([value, label]) => ({ value, label }))}
                  />
                </Field>
                <Field id="document-record" label="Related saved record">
                  <SelectControl
                    id="document-record"
                    value={selected ? `${selected.record_kind}:${selected.record_id}` : ""}
                    disabled={locked || !storageAvailable || options.length === 0}
                    onValueChange={(value) => {
                      setRecordKey(value);
                      prepared.current = null;
                      setIntentID(crypto.randomUUID());
                      action.reset();
                    }}
                    placeholder="No saved record available"
                    options={options.map((record) => ({
                      value: `${record.record_kind}:${record.record_id}`,
                      label: record.label,
                    }))}
                  />
                </Field>
              </div>
            )}
            {!resume && options.length === 0 && (
              <p className="mb-4 text-sm text-muted-foreground">
                Add the related credential or qualification to enrollment before attaching its
                evidence.
              </p>
            )}
            <form
              noValidate
              onSubmit={form.handleSubmit(() => {
                setNotice("");
                if (!action.isPending) action.mutate({ kind: "upload" });
              })}
            >
              <Field
                id="staff-document-file"
                label="Choose file"
                error={form.formState.errors.file?.message}
              >
                <input
                  id="staff-document-file"
                  ref={fileInput}
                  type="file"
                  accept="image/png,image/jpeg"
                  name="file"
                  onBlur={() => void form.trigger("file")}
                  aria-invalid={!!form.formState.errors.file}
                  aria-describedby={
                    form.formState.errors.file ? "staff-document-file-error" : undefined
                  }
                  disabled={locked || !storageAvailable || (!selected && !resume)}
                  onChange={(event) => selectFile(event.target.files?.[0])}
                  className="w-full rounded-lg border bg-background p-3 text-sm file:mr-4 file:rounded-md file:border-0 file:bg-muted file:px-3 file:py-2 file:font-medium"
                />
              </Field>
              <p className="mt-2 text-xs text-muted-foreground">
                PNG or JPEG · Up to 10 MB. Files stay protected while processing.
              </p>
              {file && (
                <p className="mt-3 break-all text-sm">
                  Selected: <span className="font-medium">{file.name}</span> ·{" "}
                  {(file.size / 1024).toFixed(1)} KB
                </p>
              )}
              <div className="mt-5 flex flex-wrap items-center gap-3">
                <Button
                  type="submit"
                  disabled={
                    !file ||
                    !!form.formState.errors.file ||
                    !storageAvailable ||
                    action.isPending ||
                    (!selected && !resume)
                  }
                >
                  {action.isPending && action.variables?.kind === "upload" ? (
                    <>
                      <Loader2 className="mr-2 size-4 animate-spin" />
                      Uploading…
                    </>
                  ) : attempted ? (
                    "Retry upload"
                  ) : (
                    "Upload file"
                  )}
                </Button>
                {(file || resume || attempted) && (
                  <Button
                    variant="outline"
                    type="button"
                    disabled={action.isPending}
                    onClick={() => {
                      resetUpload();
                      action.reset();
                    }}
                  >
                    Start over
                  </Button>
                )}
              </div>
            </form>
          </div>
        </>
      )}
      {error && (
        <p
          role="alert"
          className="mt-4 rounded-lg border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive"
        >
          {error}
        </p>
      )}
      {notice && (
        <p role="status" className="mt-4 text-sm text-(--hms-accent)">
          {notice}
        </p>
      )}
      <div className="mt-7 flex items-center justify-between gap-3">
        <h4 className="font-semibold">Saved documents</h4>
        <Button
          variant="ghost"
          size="sm"
          disabled={documents.isFetching}
          onClick={() => void documents.refetch()}
        >
          Refresh status
        </Button>
      </div>
      {documents.isPending ? (
        <p role="status" className="py-6 text-sm">
          Loading protected documents…
        </p>
      ) : documents.isError ? (
        <div role="alert" className="py-6">
          Could not load documents.{" "}
          <Button variant="outline" onClick={() => void documents.refetch()}>
            Try again
          </Button>
        </div>
      ) : documents.data?.data.length === 0 ? (
        <div className="mt-4 rounded-xl border border-dashed px-5 py-8 text-center">
          <FileText className="mx-auto mb-3 size-7 text-muted-foreground" />
          <p className="font-medium">No evidence files yet</p>
          <p className="mt-1 text-sm text-muted-foreground">
            Attach a file to a saved staff record above.
          </p>
        </div>
      ) : (
        <ul className="mt-4 divide-y rounded-xl border">
          {documents.data?.data.map((document) => {
            const expired =
              document.status === "upload_pending" && Date.parse(document.expires_at) < now;
            return (
              <li key={document.id} className="p-5">
                <div className="flex flex-wrap items-start justify-between gap-4">
                  <div className="min-w-0">
                    <p className="break-all font-medium">{document.filename}</p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {purposes[document.category]} · {(document.size / 1024).toFixed(1)} KB ·{" "}
                      {new Date(document.created_at).toLocaleDateString()}
                    </p>
                  </div>
                  <span
                    className={`rounded-full px-3 py-1 text-xs font-medium ${document.status === "available" ? "bg-(--hms-accent-dim) text-(--hms-accent)" : document.status === "rejected" || document.status === "scan_error" ? "bg-destructive/10 text-destructive" : "bg-muted text-muted-foreground"}`}
                  >
                    {expired ? "Upload expired" : states[document.status]}
                  </span>
                </div>
                {document.status === "rejected" && (
                  <p className="mt-3 text-sm text-destructive">
                    The file did not pass validation. Choose a different supported image for a new
                    upload.
                  </p>
                )}
                {document.status === "scan_error" && (
                  <p className="mt-3 text-sm text-muted-foreground">
                    Processing could not finish. Try scanning again once the service is available.
                  </p>
                )}
                <div className="mt-3 flex flex-wrap gap-2">
                  {document.status === "available" && (
                    <>
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={!storageAvailable || action.isPending}
                        onClick={() => action.mutate({ kind: "download", document })}
                      >
                        Download
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        disabled={!storageAvailable || action.isPending}
                        onClick={() => setRevoking(document)}
                      >
                        Revoke access
                      </Button>
                    </>
                  )}
                  {document.status === "scan_error" && (
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={!storageAvailable || action.isPending}
                      onClick={() => {
                        action.reset();
                        setNotice("");
                        action.mutate({ kind: "retry", document });
                      }}
                    >
                      {pendingRetries.has(document.id) ? "Retry previous request" : "Retry scan"}
                    </Button>
                  )}
                  {document.status === "upload_pending" && !expired && (
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={!storageAvailable || action.isPending}
                      onClick={() => {
                        resetUpload();
                        setResume(document);
                        setIntentID(document.id);
                        action.reset();
                        fileInput.current?.scrollIntoView({ block: "center", behavior: "smooth" });
                      }}
                    >
                      Resume upload
                    </Button>
                  )}
                </div>
              </li>
            );
          })}
        </ul>
      )}
      <div className="mt-4 flex justify-end gap-2">
        {cursor && (
          <Button size="sm" variant="outline" onClick={() => setCursor(null)}>
            First page
          </Button>
        )}
        {documents.data?.page.has_more && (
          <Button
            size="sm"
            variant="outline"
            onClick={() => setCursor(documents.data?.page.next_cursor ?? null)}
          >
            Next page
          </Button>
        )}
      </div>
      <ConfirmDialog
        open={!!revoking}
        onOpenChange={(open) => {
          if (!open) setRevoking(null);
        }}
        title="Revoke file access?"
        description="This file will remain in the audit history, but it will no longer be available to download."
        confirmLabel="Revoke access"
        busy={action.isPending}
        onConfirm={() => {
          if (revoking) action.mutate({ kind: "revoke", document: revoking });
        }}
      />
    </div>
  );
}
