"use client";

import { useEffect, useId, useRef, useState } from "react";
import { useNow } from "@/lib/use-now";
import { Button, Input } from "@tesserix/web";
import { Field, LoadMore, RefreshButton, RequestFailure, useLeaveGuard } from "@helivanta/ui";
import {
  ApiError,
  canRetryDocument,
  downloadGovernanceEmploymentDocument,
  governanceEmploymentDocumentKey,
  onTenantTransition,
  prepareGovernanceEmploymentDocument,
  retryGovernanceEmploymentDocument,
  revokeGovernanceEmploymentDocument,
  submitGovernanceEmploymentDocument,
  tenantTransitionPending,
  useApiMutation,
  useGovernanceEmploymentDocuments,
  type DocumentUpload,
  type GovernanceEmploymentDocumentScope,
  type StaffDocument,
} from "@helivanta/api";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "../staff-access-cutoff";

type Operation =
  | { kind: "upload"; id: string; file: File; original?: StaffDocument }
  | { kind: "retry" | "revoke" | "download"; row: StaffDocument };
export function GovernanceEmploymentDocuments({
  scope,
  onBusy,
  onClose,
  onAccessLost,
}: {
  scope: GovernanceEmploymentDocumentScope;
  onBusy: (busy: boolean) => void;
  onClose: () => void;
  onAccessLost: () => void;
}) {
  const id = useId();
  const now = useNow();
  const documents = useGovernanceEmploymentDocuments(scope);
  const [file, setFile] = useState<File | null>(null);
  const [pending, setPending] = useState<Operation | null>(null);
  const [revoking, setRevoking] = useState<StaffDocument | null>(null);
  const [notice, setNotice] = useState("");
  const [resuming, setResuming] = useState<StaffDocument | null>(null);
  const prepared = useRef<DocumentUpload | null>(null);
  const controller = useRef<AbortController | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const urls = useRef(new Set<string>());
  useEffect(() => {
    const activeUrls = urls.current;
    const unsubscribe = onTenantTransition(() => {
      if (tenantTransitionPending()) controller.current?.abort();
    });
    return () => {
      unsubscribe();
      controller.current?.abort();
      for (const url of activeUrls) URL.revokeObjectURL(url);
    };
  }, []);
  const action = useApiMutation(
    async (operation: Operation) => {
      if (tenantTransitionPending()) throw new DOMException("Workspace changed", "AbortError");
      const active = new AbortController();
      controller.current = active;
      if (operation.kind === "upload") {
        prepared.current ??= await prepareGovernanceEmploymentDocument(
          scope,
          operation.id,
          operation.file,
          active.signal,
        );
        if (
          operation.original &&
          (["filename", "content_type", "size", "sha256"] as const).some(
            (key) => prepared.current!.input[key] !== operation.original![key],
          )
        ) {
          throw new Error("Choose the same original file to resume this upload.");
        }
        return submitGovernanceEmploymentDocument(scope, prepared.current, active.signal);
      }
      if (operation.kind === "retry")
        return retryGovernanceEmploymentDocument(scope, operation.row, active.signal);
      if (operation.kind === "revoke")
        return revokeGovernanceEmploymentDocument(scope, operation.row, active.signal);
      const blob = await downloadGovernanceEmploymentDocument(scope, operation.row, active.signal);
      active.signal.throwIfAborted();
      const url = URL.createObjectURL(blob);
      urls.current.add(url);
      const link = document.createElement("a");
      link.href = url;
      link.download = operation.row.filename;
      link.click();
      window.setTimeout(() => {
        URL.revokeObjectURL(url);
        urls.current.delete(url);
      }, 0);
      return operation.row;
    },
    {
      suppressErrorToast: true,
      gcTime: 0,
      invalidate: [governanceEmploymentDocumentKey(scope)],
      onSuccess: () => {
        if (tenantTransitionPending()) return;
        setNotice(
          pending?.kind === "upload"
            ? "File received. Wait for scanning before attaching it to terms."
            : "File action confirmed.",
        );
        setPending(null);
        setResuming(null);
        setRevoking(null);
        setFile(null);
        prepared.current = null;
        if (input.current) input.current.value = "";
      },
    },
  );
  const cutoff = useStaffAccessCutoff([documents.error, action.error], onAccessLost);
  const locked = pending !== null || action.isPending;
  const { confirm } = useLeaveGuard(
    file !== null || locked || revoking !== null || resuming !== null,
    "employment files",
    action.isPending,
  );
  useEffect(() => {
    onBusy(locked || file !== null || revoking !== null || resuming !== null);
    return () => onBusy(false);
  }, [locked, file, revoking, resuming, onBusy]);
  useEffect(() => {
    if (cutoff) controller.current?.abort();
  }, [cutoff]);
  function start(operation: Operation) {
    setNotice("");
    setPending(operation);
    action.mutate(operation);
  }
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  return (
    <section aria-label="Prepare employment files" className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h5 className="font-semibold">Employment evidence files</h5>
        <Button variant="ghost" disabled={locked} onClick={() => confirm(onClose)}>
          Close employment files
        </Button>
      </div>
      <p className="text-sm text-muted-foreground">
        Upload a PNG or JPEG up to 10 MB. Scanning must finish before a file can be attached to an
        exact terms revision. Uploading does not approve employment or grant access.
      </p>
      {notice && <p role="status">{notice}</p>}
      {resuming && <p role="status">Choose the original file to resume {resuming.filename}.</p>}
      <Field id={id} label="Employment evidence image">
        <Input
          ref={input}
          id={id}
          type="file"
          accept="image/png,image/jpeg"
          disabled={locked}
          onChange={(event) => {
            setFile(event.target.files?.[0] ?? null);
            action.reset();
            setNotice("");
          }}
        />
      </Field>
      {!locked && (
        <Button
          disabled={!file || documents.isError || documents.isPending || documents.isFetching}
          onClick={() =>
            file &&
            start({
              kind: "upload",
              id: resuming?.id ?? crypto.randomUUID(),
              file,
              original: resuming ?? undefined,
            })
          }
        >
          Upload employment file
        </Button>
      )}
      {action.isPending && <p role="status">Checking file action…</p>}
      {action.isError && (
        <div className="space-y-2">
          <p role="alert">
            {action.error instanceof ApiError && action.error.status === 409
              ? "The file or Staff basis changed. Reopen the selected Staff record before continuing."
              : "Could not confirm the file action. Retry the same action to recover its result."}
          </p>
          {pending && !(action.error instanceof ApiError && action.error.status === 409) && (
            <Button disabled={action.isPending} onClick={() => action.mutate(pending)}>
              Retry same file action
            </Button>
          )}
          <Button
            variant="ghost"
            onClick={() =>
              confirm(() => {
                setPending(null);
                setRevoking(null);
                setFile(null);
                prepared.current = null;
                action.reset();
                onClose();
              })
            }
          >
            Leave file action
          </Button>
        </div>
      )}
      <RefreshButton
        busy={documents.isFetching || locked}
        error={documents.error}
        refresh={documents.refetch}
      >
        Refresh employment files
      </RefreshButton>
      {documents.isPending || documents.isFetching ? (
        <p role="status">Checking employment files…</p>
      ) : documents.isError ? (
        <RequestFailure
          message="Could not verify employment files."
          retry={documents.refetch}
          error={documents.error}
        />
      ) : (
        <>
          {documents.data.pages.every((page) => page.data.length === 0) && (
            <p>No employment files prepared.</p>
          )}
          <ul className="space-y-3">
            {documents.data.pages
              .flatMap((page) => page.data)
              .map((row) => (
                <li key={row.id} className="space-y-2 rounded-md border p-3">
                  <p className="break-all font-medium">{row.filename}</p>
                  <p className="text-sm text-muted-foreground">{row.status}</p>
                  <div className="flex flex-wrap gap-2">
                    {row.status === "available" && (
                      <>
                        <Button
                          variant="outline"
                          disabled={locked || revoking !== null}
                          onClick={() => start({ kind: "download", row })}
                        >
                          Download
                        </Button>
                        <Button
                          variant="outline"
                          disabled={locked || revoking !== null}
                          onClick={() => setRevoking(row)}
                        >
                          Revoke download
                        </Button>
                      </>
                    )}
                    {canRetryDocument(row, now) && (
                      <Button
                        variant="outline"
                        disabled={locked || revoking !== null}
                        onClick={() => start({ kind: "retry", row })}
                      >
                        Retry scan
                      </Button>
                    )}
                    {row.status === "upload_pending" && (
                      <Button
                        variant="outline"
                        disabled={locked || revoking !== null || resuming !== null}
                        onClick={() => {
                          setResuming(row);
                          setFile(null);
                          prepared.current = null;
                          setNotice("");
                          if (input.current) input.current.value = "";
                        }}
                      >
                        Resume upload
                      </Button>
                    )}
                    {row.status === "upload_pending" && (
                      <p className="text-sm">
                        Upload incomplete. The original file is required to recover an interrupted
                        upload.
                      </p>
                    )}
                  </div>
                </li>
              ))}
          </ul>
          <LoadMore
            hasMore={documents.hasNextPage}
            isLoading={documents.isFetchingNextPage || locked}
            onClick={() => void documents.fetchNextPage()}
          />
        </>
      )}
      {revoking && !locked && (
        <div className="space-y-3 border-t pt-4">
          <p>
            Revoke download access to {revoking.filename}? The file and audit history will be
            retained.
          </p>
          <Button variant="outline" onClick={() => setRevoking(null)}>
            Keep file available
          </Button>
          <Button onClick={() => start({ kind: "revoke", row: revoking })}>
            Confirm revocation
          </Button>
        </div>
      )}
    </section>
  );
}
