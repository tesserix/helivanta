"use client";

import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import { z } from "zod";
import {
  useSupplyEvidence,
  newSupplyId,
  usePermissions,
  useApiMutation,
  useTenantTransition,
  onTenantTransition,
  tenantTransitionPending,
  supplyEvidenceFormSchema,
  supplyEvidenceKey,
  prepareSupplyEvidence,
  submitSupplyEvidence,
  changeSupplyEvidence,
  downloadSupplyEvidence,
  type SupplyEvidence,
  type SupplyEvidenceScope,
  type SupplyEvidenceUpload,
} from "@helivanta/api";
import {
  Field,
  useZodForm,
  LoadMore,
  ConfirmDialog,
  formatDateTime,
  RequestFailure,
} from "@helivanta/ui";

const fileSchema = z.object({
  file: z.custom<File | null>().superRefine((file, ctx) => {
    if (
      !file ||
      !supplyEvidenceFormSchema.safeParse({
        expected_license_version: 1,
        filename: file.name,
        content_type: file.type,
        size: file.size,
        sha256: "0".repeat(64),
      }).success
    )
      ctx.addIssue({
        code: "custom",
        message: "Choose a PNG or JPEG image up to 10 MiB with a safe filename.",
      });
  }),
});
const states: Record<SupplyEvidence["status"], string> = {
  upload_pending: "Upload incomplete",
  scan_pending: "Checking image",
  available: "Ready",
  rejected: "Rejected",
  scan_error: "Needs attention",
  revoked: "Revoked",
};

type Operation =
  { kind: "upload" } | { kind: "download" | "retry" | "revoke"; file: SupplyEvidence };
type Props = {
  scope: SupplyEvidenceScope;
  licence: { id: string; version: number; number: string };
  roles: string[];
  onClose: () => void;
};
export function LicenceEvidence({ scope, licence, roles, onClose }: Props) {
  const { can } = usePermissions();
  const switching = useTenantTransition();
  const read =
    can("supply.evidence.read") &&
    roles.some((role) =>
      ["administrator", "purchasing", "finance", "compliance", "auditor"].includes(role),
    );
  const write =
    read &&
    can("supply.evidence.write") &&
    roles.some((role) => ["administrator", "purchasing", "compliance"].includes(role));
  const heading = useRef<HTMLHeadingElement>(null);
  const controller = useRef<AbortController | null>(null);
  const busy = useRef(false);
  const prepared = useRef<SupplyEvidenceUpload | null>(null);
  // Mirrors `prepared` for rendering: a prepared upload whose submission failed is retried as-is.
  const [retryable, setRetryable] = useState(false);
  const input = useRef<HTMLInputElement>(null);
  const urls = useRef(new Set<string>());
  const [revoking, setRevoking] = useState<SupplyEvidence | null>(null);
  const form = useZodForm(fileSchema, { file: null });
  const key = supplyEvidenceKey(scope);
  const files = useSupplyEvidence(scope, read && !switching);
  useEffect(() => {
    heading.current?.scrollIntoView({ block: "start", behavior: "instant" });
    heading.current?.focus({ preventScroll: true });
    const resources = urls.current;
    const unsubscribe = onTenantTransition(() => {
      if (tenantTransitionPending()) {
        controller.current?.abort();
        for (const url of resources) URL.revokeObjectURL(url);
        resources.clear();
      }
    });
    return () => {
      unsubscribe();
      controller.current?.abort();
      for (const url of resources) URL.revokeObjectURL(url);
      resources.clear();
    };
  }, []);
  const mutation = useApiMutation(
    async (operation: Operation) => {
      if (tenantTransitionPending())
        throw new Error("Wait for your workspace to finish switching.");
      const active = new AbortController();
      controller.current = active;
      if (operation.kind === "upload") {
        const file = form.getValues("file");
        if (!file) throw new Error("Choose a licence image first.");
        prepared.current ??= await prepareSupplyEvidence(
          newSupplyId(),
          file,
          licence.version,
          active.signal,
        );
        await submitSupplyEvidence(scope, prepared.current, active.signal);
        active.signal.throwIfAborted();
        prepared.current = null;
        form.reset({ file: null });
        if (input.current) input.current.value = "";
      } else if (operation.kind === "download") {
        const blob = await downloadSupplyEvidence(scope, operation.file, active.signal);
        active.signal.throwIfAborted();
        const url = URL.createObjectURL(blob);
        urls.current.add(url);
        const link = document.createElement("a");
        link.href = url;
        link.download = operation.file.filename;
        link.click();
        setTimeout(() => {
          URL.revokeObjectURL(url);
          urls.current.delete(url);
        }, 1500);
      } else {
        await changeSupplyEvidence(scope, operation.file, operation.kind, active.signal);
        active.signal.throwIfAborted();
        setRevoking(null);
      }
    },
    { invalidate: [key], suppressErrorToast: true },
  );
  function perform(operation: Operation) {
    if (busy.current) return;
    busy.current = true;
    mutation.mutate(operation, {
      onSettled: () => {
        busy.current = false;
        setRetryable(prepared.current !== null);
      },
    });
  }
  const loadingMore = useRef(false);
  function more() {
    if (loadingMore.current || files.isFetching || !files.hasNextPage) return;
    loadingMore.current = true;
    void files.fetchNextPage({ cancelRefetch: false }).finally(() => {
      loadingMore.current = false;
    });
  }
  return (
    <section
      aria-label="Licence evidence"
      className="scroll-mt-32 rounded-2xl border bg-card p-4 sm:p-6"
    >
      <header className="flex items-start justify-between gap-4">
        <div>
          <h5 ref={heading} tabIndex={-1} className="text-lg font-semibold outline-none">
            Licence evidence
          </h5>
          <p className="mt-1 break-words text-sm text-muted-foreground">
            {licence.number} · Current revision {licence.version}
          </p>
        </div>
        <Button variant="ghost" onClick={onClose}>
          Close evidence
        </Button>
      </header>
      <p className="mt-3 text-sm leading-6 text-muted-foreground">
        Images stay private and are checked before download. A clean image does not verify a licence
        or approve an organisation to trade.
      </p>
      {switching ? (
        <p role="status" className="mt-4">
          Switching workspace. Evidence access has been cleared.
        </p>
      ) : !read ? (
        <p role="alert" className="mt-4">
          You do not have access to this organisation’s licence evidence.
        </p>
      ) : (
        <>
          {write && (
            <form
              className="my-5 space-y-3 rounded-xl border bg-muted/20 p-4"
              onSubmit={(event) => void form.handleSubmit(() => perform({ kind: "upload" }))(event)}
            >
              <Field
                label="Licence evidence image"
                id="licence-evidence-file"
                error={form.formState.errors.file?.message}
              >
                <input
                  ref={input}
                  id="licence-evidence-file"
                  type="file"
                  accept="image/png,image/jpeg"
                  disabled={mutation.isPending}
                  className="w-full min-w-0 rounded-md border p-2 text-sm"
                  onChange={(event) => {
                    prepared.current = null;
                    setRetryable(false);
                    mutation.reset();
                    form.setValue("file", event.target.files?.[0] ?? null, {
                      shouldValidate: true,
                    });
                  }}
                />
              </Field>
              <p className="text-xs text-muted-foreground">
                PNG or JPEG · Up to 10 MiB · Saved against revision {licence.version}
              </p>
              <Button type="submit" disabled={mutation.isPending}>
                {mutation.isPending ? "Working…" : retryable ? "Retry upload" : "Upload evidence"}
              </Button>
            </form>
          )}
          {mutation.isError && (
            <p role="alert" className="my-3 text-sm text-destructive">
              {mutation.error.message}
            </p>
          )}
          {files.isError ? (
            <RequestFailure
              message="Could not load private licence evidence."
              retry={() => void files.refetch()}
            />
          ) : files.isPending ? (
            <p role="status" className="mt-5">
              Loading evidence…
            </p>
          ) : (
            <>
              {!files.data?.pages.some((page) => page.data.length) && (
                <p className="mt-5 text-sm text-muted-foreground">No evidence images recorded.</p>
              )}
              <ul className="mt-5 divide-y">
                {files.data?.pages
                  .flatMap((page) => page.data)
                  .map((file) => (
                    <li
                      key={file.id}
                      className="flex flex-wrap items-start justify-between gap-4 py-4"
                    >
                      <div className="min-w-0">
                        <p className="break-all font-medium">{file.filename}</p>
                        <p className="mt-1 text-sm text-muted-foreground">
                          Revision {file.license_version}
                          {file.license_version !== licence.version
                            ? " · historical"
                            : " · current"}{" "}
                          · {states[file.status]}
                        </p>
                        <p className="mt-1 text-xs text-muted-foreground">
                          {formatDateTime(file.created_at)} · {(file.size / 1024).toFixed(1)} KiB
                        </p>
                        {file.status === "rejected" && (
                          <p className="mt-2 text-sm text-destructive">
                            This image cannot be released. Choose a new image.
                          </p>
                        )}
                      </div>
                      <div className="flex flex-wrap gap-2">
                        {file.status === "available" && (
                          <Button
                            variant="outline"
                            disabled={mutation.isPending}
                            onClick={() => perform({ kind: "download", file })}
                          >
                            Download {file.filename}
                          </Button>
                        )}
                        {write && file.status === "available" && (
                          <Button
                            variant="ghost"
                            disabled={mutation.isPending}
                            onClick={() => setRevoking(file)}
                          >
                            Revoke
                          </Button>
                        )}
                        {write && file.status === "scan_error" && (
                          <Button
                            variant="outline"
                            disabled={mutation.isPending}
                            onClick={() => perform({ kind: "retry", file })}
                          >
                            Retry check
                          </Button>
                        )}
                      </div>
                    </li>
                  ))}
              </ul>
              <LoadMore
                hasMore={files.hasNextPage}
                isLoading={files.isFetchingNextPage}
                onClick={more}
              />
            </>
          )}
        </>
      )}
      <ConfirmDialog
        busy={mutation.isPending}
        open={revoking !== null}
        onOpenChange={(open) => {
          if (!open) setRevoking(null);
        }}
        title="Revoke evidence access?"
        description="This will prevent future downloads. The evidence and its history remain retained for audit."
        confirmLabel="Revoke evidence"
        onConfirm={() => {
          if (revoking) perform({ kind: "revoke", file: revoking });
        }}
      />
    </section>
  );
}
