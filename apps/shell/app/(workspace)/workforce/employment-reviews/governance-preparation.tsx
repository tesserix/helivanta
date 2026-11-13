"use client";

import { useEffect, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  Field,
  Panel,
  RefreshButton,
  RequestFailure,
  useConfirmLeave,
  useZodForm,
} from "@helivanta/ui";
import {
  PersonReviewTargetSearchInputContract,
  getGovernanceEmploymentSource,
  governanceEmploymentPreparationKey,
  useGovernanceEmploymentTargets,
  useGovernanceEmploymentSource,
  useGovernanceEmploymentDrafts,
  useGovernanceEmploymentDraft,
  useGovernanceEmploymentSponsors,
  saveGovernanceEmploymentDraft,
  type GovernanceEmploymentTargetScope,
  type GovernanceEmploymentPreparationScope,
  type PersonReviewTarget,
  type PersonReviewTargetSearchInput,
  type EmploymentPeriodDraft,
} from "@helivanta/api";
import { GovernanceEmploymentDocuments } from "./governance-documents";
import { EmploymentTermsForm } from "../employment-periods-panel";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "../staff-access-cutoff";

export function GovernanceEmploymentPreparation({
  scope,
  onBusy,
  onAccessLost,
}: {
  scope: GovernanceEmploymentTargetScope;
  onBusy: (busy: boolean) => void;
  onAccessLost: () => void;
}) {
  const form = useZodForm(PersonReviewTargetSearchInputContract, { employee_code: "" });
  const [input, setInput] = useState<PersonReviewTargetSearchInput | null>(null);
  const [generation, setGeneration] = useState(0);
  const [cursors, setCursors] = useState<(string | null)[]>([null]);
  const [selected, setSelected] = useState<PersonReviewTarget | null>(null);
  const confirm = useConfirmLeave();
  const targets = useGovernanceEmploymentTargets(
    scope,
    input,
    cursors.at(-1) ?? null,
    generation,
    selected === null,
  );
  const cutoff = useStaffAccessCutoff([targets.error], onAccessLost);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  return (
    <section aria-label="Hospital employment preparation" className="space-y-4">
      <h3 className="font-semibold">Prepare hospital employment terms</h3>
      <p className="text-sm text-muted-foreground">
        Select the hospital Staff record to prepare appointment or contract terms. Saving a draft
        does not activate employment or grant access.
      </p>
      {selected ? (
        <HospitalTerms
          key={selected.id}
          scope={{ ...scope, staffId: selected.id }}
          target={selected}
          onBusy={onBusy}
          onAccessLost={onAccessLost}
          onClose={() => confirm(() => setSelected(null))}
        />
      ) : (
        <>
          <form
            aria-label="Find employment applicant"
            noValidate
            className="flex flex-wrap items-end gap-3"
            onSubmit={form.handleSubmit((value) => {
              setInput(value);
              setCursors([null]);
              setGeneration((value) => value + 1);
            })}
          >
            <Field
              id="employment-preparation-code"
              label="Staff code (leave empty to browse)"
              error={form.formState.errors.employee_code?.message}
            >
              <Input
                id="employment-preparation-code"
                autoComplete="off"
                maxLength={40}
                {...form.register("employee_code")}
              />
            </Field>
            <Button type="submit" disabled={targets.isFetching}>
              Find Staff
            </Button>
          </form>
          {input !== null &&
            (targets.isPending || targets.isFetching ? (
              <p role="status">Checking employment preparation access…</p>
            ) : targets.isError ? (
              <RequestFailure
                message="Could not verify Staff selection."
                retry={targets.refetch}
                error={targets.error}
              />
            ) : (
              <>
                {targets.data.data.length === 0 && <p>No Staff match this search.</p>}
                <ul className="space-y-2">
                  {targets.data.data.map((row) => (
                    <li
                      key={row.id}
                      className="flex flex-wrap items-center justify-between gap-3 rounded-md border p-3"
                    >
                      <span>
                        {row.display_name} · {row.employee_code}
                      </span>
                      <Button
                        variant="outline"
                        aria-label={`Select ${row.display_name}`}
                        onClick={() => setSelected(row)}
                      >
                        Select
                      </Button>
                    </li>
                  ))}
                </ul>
                <div className="flex gap-2">
                  {cursors.length > 1 && (
                    <Button
                      variant="outline"
                      onClick={() => setCursors((value) => value.slice(0, -1))}
                    >
                      Previous Staff
                    </Button>
                  )}
                  {targets.data.page.next_cursor && (
                    <Button
                      variant="outline"
                      onClick={() =>
                        setCursors((value) => [...value, targets.data.page.next_cursor])
                      }
                    >
                      Next Staff
                    </Button>
                  )}
                </div>
              </>
            ))}
        </>
      )}
    </section>
  );
}

function HospitalTerms({
  scope,
  target,
  onBusy,
  onAccessLost,
  onClose,
}: {
  scope: GovernanceEmploymentPreparationScope;
  target: PersonReviewTarget;
  onBusy: (busy: boolean) => void;
  onAccessLost: () => void;
  onClose: () => void;
}) {
  const base = [...governanceEmploymentPreparationKey(scope), scope.staffId];
  const source = useGovernanceEmploymentSource(scope);
  const periods = useGovernanceEmploymentDrafts(scope);
  const [editor, setEditor] = useState<{ item?: EmploymentPeriodDraft } | null>(null);
  const [saved, setSaved] = useState(false);
  const [documents, setDocuments] = useState(false);
  const [documentBusy, setDocumentBusy] = useState(false);
  const cutoff = useStaffAccessCutoff([source.error, periods.error], onAccessLost);
  useEffect(() => {
    onBusy(editor !== null || documentBusy);
    return () => onBusy(false);
  }, [editor, documentBusy, onBusy]);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  return (
    <Panel className="space-y-4 p-4">
      <div className="flex items-center justify-between gap-3">
        <h4 className="font-semibold">
          {target.display_name} · {target.employee_code}
        </h4>
        <Button variant="ghost" disabled={editor !== null || documentBusy} onClick={onClose}>
          Choose another Staff record
        </Button>
      </div>
      {source.isPending || source.isFetching || periods.isPending || periods.isFetching ? (
        <p role="status">Checking current employment terms…</p>
      ) : source.isError ? (
        <RequestFailure
          message="Could not verify employment preparation source."
          error={source.error}
          retry={source.refetch}
        />
      ) : periods.isError ? (
        <RequestFailure
          message="Could not verify employment terms."
          error={periods.error}
          retry={periods.refetch}
        />
      ) : source.data.staff_version !== target.staff_version ? (
        <p role="alert">
          Staff details changed. Choose the Staff record again before preparing terms.
        </p>
      ) : (
        <>
          {saved && <p role="status">Employment terms draft saved.</p>}
          {documents ? (
            <GovernanceEmploymentDocuments
              key={`${source.data.staff_version}:${source.data.employment_epoch}`}
              scope={{
                ...scope,
                staffVersion: source.data.staff_version,
                employmentEpoch: source.data.employment_epoch,
              }}
              onBusy={setDocumentBusy}
              onClose={() => setDocuments(false)}
              onAccessLost={onAccessLost}
            />
          ) : editor ? (
            <GovernanceTermsEditor
              scope={scope}
              source={source.data}
              item={editor.item}
              onClose={() => setEditor(null)}
              onSaved={() => {
                setSaved(true);
                setEditor(null);
              }}
              onAccessLost={onAccessLost}
              invalidate={[
                [...base, "periods"],
                [...base, "source"],
              ]}
            />
          ) : (
            <>
              <RefreshButton
                busy={source.isFetching || periods.isFetching}
                error={source.error ?? periods.error}
                refresh={() => {
                  setSaved(false);
                  void Promise.all([source.refetch(), periods.refetch()]);
                }}
              >
                Refresh terms
              </RefreshButton>
              <Button variant="outline" onClick={() => setDocuments(true)}>
                Prepare employment files
              </Button>
              <Button
                onClick={() => {
                  setSaved(false);
                  setEditor({});
                }}
              >
                Prepare employment terms
              </Button>
              <ul className="space-y-3">
                {periods.data.pages
                  .flatMap((page) => page.data)
                  .map((row) => (
                    <li key={row.id} className="rounded-md border p-3">
                      <p>
                        {row.evidence_reference} · Revision {row.version} · {row.status}
                      </p>
                      <p className="text-sm text-muted-foreground">{row.reason}</p>
                      {row.status === "draft" && (
                        <Button
                          variant="outline"
                          onClick={() => {
                            setSaved(false);
                            setEditor({ item: row });
                          }}
                        >
                          Edit {row.evidence_reference}
                        </Button>
                      )}
                    </li>
                  ))}
              </ul>
              {periods.hasNextPage && (
                <Button
                  variant="outline"
                  disabled={periods.isFetchingNextPage}
                  onClick={() => void periods.fetchNextPage()}
                >
                  More terms
                </Button>
              )}
            </>
          )}
        </>
      )}
    </Panel>
  );
}

function GovernanceTermsEditor({
  scope,
  source,
  item,
  onClose,
  onSaved,
  onAccessLost,
  invalidate,
}: {
  scope: GovernanceEmploymentPreparationScope;
  source: Awaited<ReturnType<typeof getGovernanceEmploymentSource>>;
  item?: EmploymentPeriodDraft;
  onClose: () => void;
  onSaved: () => void;
  onAccessLost: () => void;
  invalidate: unknown[][];
}) {
  const current = useGovernanceEmploymentDraft(scope, item?.id);
  const cutoff = useStaffAccessCutoff([current.error], onAccessLost);
  const sponsors = useGovernanceEmploymentSponsors(scope);
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (item && (current.isPending || current.isFetching))
    return <p role="status">Rechecking the selected terms…</p>;
  if (item && (current.isError || current.data?.status !== "draft"))
    return (
      <div className="space-y-3">
        {current.isError ? (
          <RequestFailure
            message="Could not verify the selected draft."
            error={current.error}
            retry={current.refetch}
          />
        ) : (
          <p role="alert">
            These terms are no longer a draft. Close this form and refresh the terms.
          </p>
        )}
        <Button variant="ghost" onClick={onClose}>
          Close draft form
        </Button>
      </div>
    );
  return (
    <EmploymentTermsForm
      scope={scope}
      source={source}
      basisAvailable={!sponsors.isError && !sponsors.isPending && !sponsors.isFetching}
      item={item ? current.data : undefined}
      onClose={onClose}
      onSaved={onSaved}
      onAccessChanged={onAccessLost}
      sponsors={{
        items: sponsors.data?.pages.flatMap((page) => page.data) ?? [],
        hasMore: sponsors.hasNextPage,
        loadMore: () => void sponsors.fetchNextPage(),
        isFetchingMore: sponsors.isFetchingNextPage,
        isError: sponsors.isError,
        error: sponsors.error,
        retry: () => void sponsors.refetch(),
      }}
      saveDraft={(id, input) => saveGovernanceEmploymentDraft(scope, id, input)}
      invalidate={invalidate}
    />
  );
}
