"use client";
import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import { FileCheck2 } from "lucide-react";
import {
  downloadTaxEvidence,
  prepareTaxEvidence,
  retryTaxEvidence,
  submitTaxEvidence,
  taxEvidenceFileSchema,
  taxEvidenceKey,
  useApiMutation,
  useTaxEvidence,
  MAX_TAX_EVIDENCE,
  type TaxEvidence,
  type TaxEvidenceScope,
  type TaxEvidenceUpload,
} from "@helivanta/api";
import { Field, useZodForm } from "@helivanta/ui";
import { Failure } from "../roster-policy/policy-ui";
import { SaveError } from "../leave-balances/years-panel";

const states: Record<TaxEvidence["status"], string> = {
  upload_pending: "Upload incomplete",
  scan_pending: "Scanning",
  available: "Ready",
  scan_error: "Scan failed",
  rejected: "Rejected by scan",
  revoked: "Revoked",
};

export const evidenceScanning = (files: TaxEvidence[] | undefined) =>
  !!files?.some((file) => file.status === "scan_pending");

export function DeclarationEvidence({
  label,
  scope,
  attachable,
}: {
  label: string;
  scope: TaxEvidenceScope;
  attachable: boolean;
}) {
  const evidence = useTaxEvidence(scope);
  const form = useZodForm(taxEvidenceFileSchema, { file: null });
  const file = form.watch("file");
  const fileInput = useRef<HTMLInputElement>(null);
  const prepared = useRef<TaxEvidenceUpload | null>(null);
  const controller = useRef<AbortController | null>(null);
  const [intent, setIntent] = useState(() => crypto.randomUUID());
  useEffect(() => () => controller.current?.abort(), []);
  const signal = () => {
    controller.current?.abort();
    controller.current = new AbortController();
    return controller.current.signal;
  };
  const settle = { invalidate: [taxEvidenceKey(scope)], suppressErrorToast: true };
  const upload = useApiMutation(
    async () => {
      const active = signal();
      prepared.current ??= await prepareTaxEvidence(scope, intent, file!, active);
      return submitTaxEvidence(scope, prepared.current, active);
    },
    {
      ...settle,
      successToast: "Evidence uploaded",
      onSuccess: () => {
        prepared.current = null;
        setIntent(crypto.randomUUID());
        form.reset({ file: null });
        if (fileInput.current) fileInput.current.value = "";
      },
    },
  );
  const retry = useApiMutation(
    (f: TaxEvidence) => retryTaxEvidence(scope, f, f.scan_attempt + 1, signal()),
    { ...settle, successToast: "Scan retried" },
  );
  const download = useApiMutation(
    async (f: TaxEvidence) => {
      const blob = await downloadTaxEvidence(scope, f, signal());
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = f.filename;
      link.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 0);
    },
    { suppressErrorToast: true, successToast: "Evidence downloaded" },
  );
  const busy = upload.isPending || retry.isPending || download.isPending;
  const failure = upload.error ?? retry.error ?? download.error;
  const files = evidence.data?.evidence ?? [];
  const storage = evidence.data?.uploads_available ?? false;
  const full = files.length >= MAX_TAX_EVIDENCE;
  const errors = form.formState.errors;
  return (
    <section aria-label={label} className="mt-4 rounded-xl border p-4">
      <h5 className="mb-3 flex items-center gap-2 text-sm font-semibold">
        <FileCheck2 className="size-4 text-(--hms-accent)" />
        {label}
      </h5>
      {evidence.isPending ? (
        <p role="status" className="text-sm">
          Loading evidence…
        </p>
      ) : evidence.isError ? (
        <Failure
          message="Could not load this proposal's evidence."
          retry={() => evidence.refetch()}
        />
      ) : (
        <>
          {files.length === 0 ? (
            <p className="text-sm text-muted-foreground">No evidence attached.</p>
          ) : (
            <ul className="divide-y rounded-lg border text-sm">
              {files.map((f) => (
                <li key={f.id} className="flex flex-wrap items-center justify-between gap-3 p-3">
                  <span className="min-w-0 break-all">
                    {f.filename}{" "}
                    <span className="text-xs text-muted-foreground">
                      · {(f.size / 1024).toFixed(1)} KB
                    </span>
                  </span>
                  <span className="flex items-center gap-2">
                    <span
                      className={`rounded-full px-2 py-0.5 text-xs font-medium ${f.status === "available" ? "bg-(--hms-accent-dim) text-(--hms-accent)" : f.status === "rejected" || f.status === "scan_error" ? "bg-destructive/10 text-destructive" : "bg-muted text-muted-foreground"}`}
                    >
                      {states[f.status]}
                    </span>
                    {f.status === "available" && (
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={!storage || busy}
                        aria-label={`Download ${f.filename}`}
                        onClick={() => download.mutate(f)}
                      >
                        Download
                      </Button>
                    )}
                    {f.status === "scan_error" && attachable && (
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={!storage || busy}
                        aria-label={`Retry scan of ${f.filename}`}
                        onClick={() => retry.mutate(f)}
                      >
                        Retry scan
                      </Button>
                    )}
                  </span>
                </li>
              ))}
            </ul>
          )}
          {attachable && !storage && (
            <p role="status" className="mt-3 text-xs text-muted-foreground">
              File storage is not available, so evidence cannot be attached right now.
            </p>
          )}
          {attachable && storage && full && (
            <p className="mt-3 text-xs text-muted-foreground">
              This proposal has the maximum of {MAX_TAX_EVIDENCE} evidence files.
            </p>
          )}
          {attachable && storage && !full && (
            <form
              noValidate
              aria-label="Attach declaration evidence"
              className="mt-4"
              onSubmit={form.handleSubmit(() => {
                if (!busy) upload.mutate();
              })}
            >
              <Field
                id="tax-evidence-file"
                label="Rent receipt, interest certificate or investment proof"
                error={errors.file?.message}
              >
                <input
                  id="tax-evidence-file"
                  ref={fileInput}
                  type="file"
                  accept="image/png,image/jpeg"
                  aria-invalid={!!errors.file}
                  aria-describedby={errors.file ? "tax-evidence-file-error" : undefined}
                  disabled={busy}
                  onChange={(ev) => {
                    prepared.current = null;
                    setIntent(crypto.randomUUID());
                    upload.reset();
                    form.setValue("file", ev.target.files?.[0] ?? null, { shouldValidate: true });
                  }}
                  className="w-full rounded-lg border bg-background p-2 text-sm file:mr-4 file:rounded-md file:border-0 file:bg-muted file:px-3 file:py-1.5 file:font-medium"
                />
              </Field>
              <p className="mt-1 text-xs text-muted-foreground">
                PNG or JPEG · Up to 10 MB. Each file is scanned before anyone can open it.
              </p>
              <div className="mt-3 flex justify-end">
                <Button type="submit" size="sm" disabled={busy}>
                  {upload.isPending ? "Uploading…" : "Upload evidence"}
                </Button>
              </div>
            </form>
          )}
          {failure && <SaveError message={failure.message} />}
        </>
      )}
    </section>
  );
}
