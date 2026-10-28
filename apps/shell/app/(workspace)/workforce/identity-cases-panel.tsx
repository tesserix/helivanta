"use client";

import { useState } from "react";
import { Button } from "@tesserix/web";
import { LoadMore, Panel } from "@helivanta/ui";
import {
  IdentityCaseContract,
  identityCaseRequest,
  identityCasesKey,
  identityCasesPath,
  useApiPagedQuery,
  type IdentityCase,
  type IdentityCaseScope,
  type IdentitySource,
} from "@helivanta/api";
import { IdentityCaseEditor } from "./identity-case-editor";

const labels = { identity: "Identity", background: "Background", photograph: "Photograph" };

function readCases(values: unknown[], scope: IdentityCaseScope, id?: string) {
  const parsed = IdentityCaseContract.array().safeParse(values);
  if (
    !parsed.success ||
    parsed.data.some(
      (item) =>
        item.hospital_id !== scope.hospitalId ||
        item.staff_id !== scope.staffId ||
        (id && item.id !== id),
    )
  )
    return null;
  return parsed.data;
}

export function StaffIdentityCases({
  scope,
  source,
}: {
  scope: IdentityCaseScope;
  source: IdentitySource;
}) {
  const cases = useApiPagedQuery<unknown>(identityCasesKey(scope), identityCasesPath(scope), {
    request: identityCaseRequest(scope),
    gcTime: 0,
  });
  const [editor, setEditor] = useState<{ item?: IdentityCase } | null>(null);
  const [history, setHistory] = useState<string | null>(null);
  const rows = readCases(cases.items, scope);
  const canManage = source.manage_checks_allowed === true && source.employment_status === "active";
  function closeEditor() {
    setEditor(null);
    cases.retry();
  }
  return (
    <section aria-label="Staff review drafts" className="space-y-4 border-t pt-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h4 className="font-semibold">Review drafts</h4>
          <p className="mt-1 text-sm text-muted-foreground">
            Identity, background and photograph preparation. Evidence review and card issuance are
            separate steps.
          </p>
        </div>
        {canManage && (
          <Button disabled={editor !== null} onClick={() => setEditor({})}>
            Prepare review draft
          </Button>
        )}
      </div>
      {source.employment_status !== "active" && (
        <p className="text-sm text-muted-foreground">
          Staff is inactive. History remains available; new draft changes require active employment.
        </p>
      )}
      {editor && (
        <IdentityCaseEditor
          key={editor.item?.id ?? "new"}
          scope={scope}
          source={source}
          item={editor.item}
          onClose={closeEditor}
          onSaved={closeEditor}
        />
      )}
      {cases.isPending ? (
        <p role="status">Loading review drafts…</p>
      ) : cases.isError || !rows ? (
        <div role="alert">
          <p>Could not load or validate review drafts. Current identity access may have changed.</p>
          <Button variant="outline" onClick={cases.retry}>
            Retry review drafts
          </Button>
        </div>
      ) : rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          No review drafts recorded for this staff member.
        </p>
      ) : (
        <ul className="space-y-3">
          {rows.map((item) => (
            <Panel as="li" key={item.id} className="space-y-3">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <h5 className="font-medium">{labels[item.kind]} check</h5>
                  <p className="text-sm">{item.policy_reference}</p>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {item.status === "draft" ? "Unreviewed draft" : item.status} · Version{" "}
                    {item.version}
                  </p>
                </div>
                <div className="flex flex-wrap gap-2">
                  <Button
                    variant="outline"
                    aria-label={`History for ${item.policy_reference}`}
                    onClick={() => setHistory(history === item.id ? null : item.id)}
                  >
                    History
                  </Button>
                  {canManage && item.status === "draft" && (
                    <Button
                      variant="outline"
                      disabled={editor !== null}
                      aria-label={`Edit draft ${item.policy_reference}`}
                      onClick={() => setEditor({ item })}
                    >
                      Edit draft
                    </Button>
                  )}
                </div>
              </div>
              <p className="text-sm whitespace-pre-wrap">{item.check_reference}</p>
              <dl className="grid gap-3 text-sm sm:grid-cols-2">
                <div>
                  <dt className="text-xs text-muted-foreground">Observation date (UTC)</dt>
                  <dd>{item.observed_on}</dd>
                </div>
                <div>
                  <dt className="text-xs text-muted-foreground">Valid until</dt>
                  <dd>
                    {new Date(item.valid_until).toLocaleString(undefined, {
                      timeZoneName: "short",
                    })}
                  </dd>
                </div>
              </dl>
              {(item.identity_hash !== source.identity_hash ||
                item.employment_epoch !== source.employment_epoch) && (
                <p className="text-sm text-muted-foreground">
                  Identity or employment details changed since this draft.
                </p>
              )}
              {history === item.id && (
                <CaseHistory scope={scope} id={item.id} onClose={() => setHistory(null)} />
              )}
            </Panel>
          ))}
        </ul>
      )}
      <LoadMore hasMore={cases.hasMore} isLoading={cases.isFetchingMore} onClick={cases.loadMore} />
    </section>
  );
}

function CaseHistory({
  scope,
  id,
  onClose,
}: {
  scope: IdentityCaseScope;
  id: string;
  onClose: () => void;
}) {
  const query = useApiPagedQuery<unknown>(
    [...identityCasesKey(scope), id, "history"],
    `${identityCasesPath(scope)}/${id}/history`,
    { request: identityCaseRequest(scope), gcTime: 0 },
  );
  const rows = readCases(query.items, scope, id);
  return (
    <section aria-label="Review draft history" className="space-y-3 border-t pt-3">
      <div className="flex items-center justify-between gap-3">
        <h6 className="font-semibold">Review draft history</h6>
        <Button variant="ghost" onClick={onClose}>
          Close draft history
        </Button>
      </div>
      {query.isPending ? (
        <p role="status">Loading draft history…</p>
      ) : query.isError || !rows ? (
        <div role="alert">
          <p>Could not load or validate draft history.</p>
          <Button variant="outline" onClick={query.retry}>
            Retry draft history
          </Button>
        </div>
      ) : (
        <ol className="space-y-3">
          {rows.map((item) => (
            <Panel as="li" key={item.revision_id} className="text-sm">
              <p className="font-medium">
                Version {item.version} · {item.status}
              </p>
              <p>{item.policy_reference}</p>
              <p className="whitespace-pre-wrap">{item.check_reference}</p>
              <p className="mt-2 text-xs text-muted-foreground">
                Recorded{" "}
                {new Date(item.updated_at).toLocaleString(undefined, { timeZoneName: "short" })} ·
                Account {item.actor_subject}
              </p>
            </Panel>
          ))}
        </ol>
      )}
      <LoadMore hasMore={query.hasMore} isLoading={query.isFetchingMore} onClick={query.loadMore} />
    </section>
  );
}
