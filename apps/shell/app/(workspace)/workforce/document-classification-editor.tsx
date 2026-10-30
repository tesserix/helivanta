"use client";

import { useEffect, useRef, useState } from "react";
import { Button } from "@tesserix/web";
import { FormSelectField, useZodForm, Panel } from "@helivanta/ui";
import {
  ApiError,
  usePermissions,
  useTenantTransition,
  onTenantTransition,
  tenantTransitionPending,
  useApiMutation,
  useApiPagedQuery,
  useDocumentClassification,
  useDocumentClassificationHistory,
  saveDocumentClassification,
  documentClassificationInputSchema,
  documentClassificationQueryKey,
  type DocumentScope,
  type StaffDocument,
  type DocumentClassification,
  type DocumentClassificationInput,
  type HRMaster,
} from "@helivanta/api";

type Props = { scope: DocumentScope; file: StaffDocument; onClose: () => void };

export function DocumentClassificationEditor({ scope, file, onClose }: Props) {
  const { can } = usePermissions();
  const switching = useTenantTransition();
  const allowed =
    can("workforce.documents.manage") &&
    file.hospital_id === scope.hospitalId &&
    file.staff_id === scope.staffId;
  const current = useDocumentClassification(scope, file.id, allowed && !switching);
  const [notice, setNotice] = useState("");
  if (switching) return <p role="status">Switching organisation…</p>;
  if (!allowed) return <p role="alert">You do not have access to document classifications.</p>;
  if (current.isPending) return <p role="status">Loading protected document classification…</p>;
  if (current.isError || !current.data)
    return (
      <section aria-label="Document classification">
        <p role="alert">Document classification is not available. Reload before making changes.</p>
        <Button type="button" variant="outline" onClick={() => void current.refetch()}>
          Try again
        </Button>
        <Button type="button" variant="ghost" onClick={onClose}>
          Close classification
        </Button>
      </section>
    );
  return (
    <Panel aria-label="Document classification" className="space-y-4">
      <div>
        <h3 className="text-lg font-semibold">Document classification</h3>
        <p className="break-words text-sm text-muted-foreground">{file.filename}</p>
      </div>
      <p className="text-sm text-muted-foreground">
        This optional HR label does not change the evidence purpose, file review status or
        permission to download.
      </p>
      {notice && <p role="status">{notice}</p>}
      <ClassificationForm
        key={`${scope.tenantId}:${scope.subject}:${scope.hospitalId}:${scope.staffId}:${file.id}:${current.data.version}`}
        scope={scope}
        file={file}
        value={current.data}
        refreshing={current.isFetching}
        onReload={() => void current.refetch()}
        onSaved={() => {
          setNotice("Document classification saved.");
          void current.refetch();
        }}
        onClose={onClose}
      />
    </Panel>
  );
}

function ClassificationForm({
  scope,
  file,
  value,
  refreshing,
  onReload,
  onSaved,
  onClose,
}: Props & {
  value: DocumentClassification;
  refreshing: boolean;
  onReload: () => void;
  onSaved: () => void;
}) {
  const form = useZodForm(documentClassificationInputSchema, {
    expected_version: value.version,
    classification_revision_id: value.classification_revision_id,
  });
  const controller = useRef<AbortController | null>(null);
  const [command, setCommand] = useState<DocumentClassificationInput | null>(null);
  const [historyVisible, setHistoryVisible] = useState(false);
  const [cursors, setCursors] = useState<(string | null)[]>([null]);
  const cursor = cursors[cursors.length - 1] ?? null;
  const history = useDocumentClassificationHistory(scope, file.id, cursor, historyVisible);
  const masters = useApiPagedQuery<HRMaster>(
    ["workforce", "document-classification-choices", scope.tenantId, scope.hospitalId],
    `/workforce/hospitals/${scope.hospitalId}/hr/masters?kind=document_category`,
    { request: { headers: { "X-Helivanta-Expected-Tenant": scope.tenantId }, cache: "no-store" } },
  );
  const save = useApiMutation(
    (input: DocumentClassificationInput) => {
      if (tenantTransitionPending())
        throw new Error("The organisation is changing. Reload before saving.");
      controller.current?.abort();
      controller.current = new AbortController();
      return saveDocumentClassification(scope, file.id, input, controller.current.signal);
    },
    {
      onSuccess: onSaved,
      invalidate: [[...documentClassificationQueryKey(scope, file.id), "history"]],
      suppressErrorToast: true,
    },
  );
  useEffect(() => {
    const stop = onTenantTransition(() => controller.current?.abort());
    return () => {
      stop();
      controller.current?.abort();
    };
  }, []);
  const definitive =
    save.error instanceof ApiError && save.error.status >= 400 && save.error.status < 500;
  const uncertain = save.isError && !definitive;
  const stale = save.error instanceof ApiError && save.error.status === 409;
  const denied =
    save.error instanceof ApiError && (save.error.status === 403 || save.error.status === 404);
  const blocked = save.isPending || refreshing || uncertain || stale || denied;
  const controlDisabled =
    blocked || masters.isPending || masters.isFetchingMore || masters.isRefreshing;
  useEffect(() => {
    if (!controlDisabled) form.setFocus("classification_revision_id");
  }, [controlDisabled, form]);

  const choices = masters.isError
    ? []
    : masters.items.filter((row) => row.kind === "document_category" && row.active);
  const selected = form.watch("classification_revision_id");
  const unknownSelection =
    selected !== "" &&
    selected !== value.classification_revision_id &&
    !choices.some((row) => row.revision_id === selected);
  const reload = () => {
    setCommand(null);
    save.reset();
    form.reset({
      expected_version: value.version,
      classification_revision_id: value.classification_revision_id,
    });
    masters.retry();
    onReload();
  };
  return (
    <div className="space-y-4">
      <form
        className="space-y-4"
        onSubmit={form.handleSubmit((input) => {
          const next = { ...input };
          setCommand(next);
          save.mutate(next);
        })}
      >
        <FormSelectField
          control={form.control}
          name="classification_revision_id"
          id={`document-classification-${file.id}`}
          label="Document classification"
          disabled={blocked || masters.isPending || masters.isFetchingMore || masters.isRefreshing}
          onValueChange={() => save.reset()}
          options={[
            { value: "", label: "Unclassified" },
            ...choices.map((row) => ({
              value: row.revision_id,
              label: `${row.name} · ${row.code}`,
            })),
            ...(value.classification_revision_id &&
            !choices.some((row) => row.revision_id === value.classification_revision_id)
              ? [
                  {
                    value: value.classification_revision_id,
                    label: `Recorded: ${value.classification_name}`,
                  },
                ]
              : []),
          ]}
        />
        {(masters.isPending || masters.isFetchingMore || masters.isRefreshing) && (
          <p role="status">Loading document categories…</p>
        )}
        {masters.isError && (
          <div role="alert">
            Document categories could not be loaded. You can retain the recorded value or clear it.{" "}
            <Button
              type="button"
              variant="outline"
              disabled={blocked}
              onClick={() => masters.retry()}
            >
              Retry categories
            </Button>
          </div>
        )}
        {masters.hasMore && (
          <Button
            type="button"
            variant="outline"
            disabled={
              blocked || masters.isPending || masters.isFetchingMore || masters.isRefreshing
            }
            onClick={() => masters.loadMore()}
          >
            Load more document categories
          </Button>
        )}
        {unknownSelection && (
          <p role="alert">Choose an available document category or clear the classification.</p>
        )}
        {save.isError && (
          <div role="alert" className="space-y-2">
            <p>
              {uncertain
                ? "The save was not confirmed. Retry the same request or reload the latest classification."
                : stale
                  ? "Document classification changed. Reload before saving again."
                  : denied
                    ? "Document classification access is no longer available. Reload to verify access."
                    : save.error.message}
            </p>
            {uncertain && (
              <Button
                type="button"
                variant="outline"
                onClick={() => {
                  if (command) save.mutate(command);
                }}
              >
                Retry previous save
              </Button>
            )}
            <Button type="button" variant="outline" onClick={reload}>
              Reload classification
            </Button>
          </div>
        )}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={blocked || unknownSelection}>
            {save.isPending ? "Saving classification…" : "Save classification"}
          </Button>
          <Button type="button" variant="ghost" disabled={save.isPending} onClick={onClose}>
            Close classification
          </Button>
        </div>
      </form>
      <Button
        type="button"
        variant="outline"
        disabled={save.isPending || uncertain}
        onClick={() => setHistoryVisible((shown) => !shown)}
      >
        {historyVisible ? "Hide classification history" : "Show classification history"}
      </Button>
      {historyVisible && (
        <section aria-label="Classification history" className="space-y-3">
          <h4 className="font-medium">Classification history</h4>
          {history.isPending || history.isFetching ? (
            <p role="status">Loading classification history…</p>
          ) : history.isError ? (
            <div role="alert">
              Classification history is not available.{" "}
              <Button type="button" variant="outline" onClick={() => void history.refetch()}>
                Retry history
              </Button>
            </div>
          ) : (
            history.data && (
              <>
                {history.data.data.length === 0 ? (
                  <p className="text-sm text-muted-foreground">
                    No classification changes recorded.
                  </p>
                ) : (
                  <ol className="space-y-2">
                    {history.data.data.map((row) => (
                      <Panel as="li" key={row.id} className="text-sm">
                        <span className="font-medium">
                          Version {row.version} · {row.classification_name || "Unclassified"}
                        </span>
                        <time className="ml-2 text-muted-foreground" dateTime={row.created_at}>
                          {new Date(row.created_at).toLocaleString()}
                        </time>
                      </Panel>
                    ))}
                  </ol>
                )}
                <div className="flex gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    disabled={cursors.length <= 1}
                    onClick={() => setCursors((items) => items.slice(0, -1))}
                  >
                    Previous changes
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    disabled={!history.data.page.has_more || !history.data.page.next_cursor}
                    onClick={() => {
                      const next = history.data?.page.next_cursor;
                      if (next) setCursors((items) => [...items, next]);
                    }}
                  >
                    Next changes
                  </Button>
                </div>
              </>
            )
          )}
        </section>
      )}
    </div>
  );
}
