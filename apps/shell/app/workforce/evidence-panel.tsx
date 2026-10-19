"use client";
import { useEffect, useId, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import { FileCheck2 } from "lucide-react";
import {
  useApiMutation,
  useEvidence,
  MAX_EVIDENCE,
  type Evidence,
  type EvidenceClient,
  type EvidenceScope,
  type EvidenceUpload,
} from "@helivanta/api";
import { Field, useZodForm } from "@helivanta/ui";
import { Failure } from "./roster-policy/policy-ui";
import { SaveError } from "./leave-balances/years-panel";

// One per record kind: the client and the words its attach form uses.
export type EvidenceSource = { client: EvidenceClient; form: string; file: string };

const states: Record<Evidence["status"], string> = {
  upload_pending: "Upload incomplete",
  scan_pending: "Scanning",
  available: "Ready",
  scan_error: "Scan failed",
  rejected: "Rejected by scan",
  revoked: "Revoked",
};

// The server refuses any decision while evidence scans; holding the buttons keeps the reason visible.
export const useEvidenceScanning = (client: EvidenceClient, scope: EvidenceScope) =>
  !!useEvidence(client, scope).data?.evidence.some((file) => file.status === "scan_pending");

export const EvidenceScanningNotice = () => (
  <p role="status" className="text-sm text-muted-foreground">
    Evidence is still being scanned. You can decide once every file has finished.
  </p>
);

export function EvidencePanel({
  source: { client, ...words },
  label,
  scope,
  attachable,
}: {
  source: EvidenceSource;
  label: string;
  scope: EvidenceScope;
  attachable: boolean;
}) {
  const evidence = useEvidence(client, scope);
  const form = useZodForm(client.fileSchema, { file: null });
  const file = form.watch("file");
  const fileID = useId();
  const fileInput = useRef<HTMLInputElement>(null);
  const prepared = useRef<EvidenceUpload | null>(null);
  const controller = useRef<AbortController | null>(null);
  const [intent, setIntent] = useState(() => crypto.randomUUID());
  useEffect(() => () => controller.current?.abort(), []);
  const signal = () => {
    controller.current?.abort();
    controller.current = new AbortController();
    return controller.current.signal;
  };
  const settle = { invalidate: [client.key(scope)], suppressErrorToast: true };
  const upload = useApiMutation(
    async () => {
      const active = signal();
      prepared.current ??= await client.prepare(scope, intent, file!, active);
      return client.submit(scope, prepared.current, active);
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
    (f: Evidence) => client.retry(scope, f, f.scan_attempt + 1, signal()),
    { ...settle, successToast: "Scan retried" },
  );
  const download = useApiMutation(
    async (f: Evidence) => {
      const blob = await client.download(scope, f, signal());
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
  const full = files.length >= MAX_EVIDENCE;
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
                        type="button"
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
                        type="button"
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
              This proposal has the maximum of {MAX_EVIDENCE} evidence files.
            </p>
          )}
          {attachable && storage && !full && (
            <form
              noValidate
              aria-label={words.form}
              className="mt-4"
              onSubmit={form.handleSubmit(() => {
                if (!busy) upload.mutate();
              })}
            >
              <Field id={fileID} label={words.file} error={errors.file?.message}>
                <input
                  id={fileID}
                  ref={fileInput}
                  type="file"
                  accept="image/png,image/jpeg"
                  aria-invalid={!!errors.file}
                  aria-describedby={errors.file ? `${fileID}-error` : undefined}
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
