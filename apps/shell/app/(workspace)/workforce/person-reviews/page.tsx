"use client";

import { useState, useSyncExternalStore } from "react";
import { Button } from "@tesserix/web";
import { Field, PageHeading, Panel, SelectControl } from "@helivanta/ui";
import {
  useApiQuery,
  useTenantTransition,
  usePersonReviewScopes,
  usePersonReviewInbox,
  type PersonReviewHospitalScope,
  type PersonReviewHospitalWorkspace,
  type PersonReviewSummary,
  type PermissionsResponse,
} from "@helivanta/api";
import { PersonReviewDetail } from "./review-detail";
import { ManagerPersonReviewPreparation } from "./manager-preparation";

const subscribeHydration = () => () => {};
type Workspace = Pick<PersonReviewHospitalWorkspace, "tenantId" | "subject">;

export default function PersonReviews() {
  const hydrated = useSyncExternalStore(
    subscribeHydration,
    () => true,
    () => false,
  );
  const switching = useTenantTransition();
  const identity = useApiQuery<PermissionsResponse>(
    ["iam", "me", "permissions"],
    "/iam/me/permissions",
    {
      enabled: !switching,
      gcTime: 0,
      staleTime: 0,
      refetchOnWindowFocus: false,
      request: { cache: "no-store" },
    },
  );
  return (
    <>
      <PageHeading description="Review person findings under the hospital’s identity policy. Employment and access require separate approval." />
      {!hydrated || switching || identity.isPending || identity.isFetching ? (
        <p role="status">Checking your workspace…</p>
      ) : identity.isError || !identity.data?.tenant_id || !identity.data.subject ? (
        <p role="alert">Could not verify your workspace. Refresh this page to try again.</p>
      ) : (
        <ReviewWorkspace
          key={`${identity.data.tenant_id}:${identity.data.subject}`}
          workspace={{ tenantId: identity.data.tenant_id, subject: identity.data.subject }}
        />
      )}
    </>
  );
}

function ReviewWorkspace({ workspace }: { workspace: Workspace }) {
  const [cursors, setCursors] = useState<(string | null)[]>([null]);
  const [selected, setSelected] = useState("");
  const [busy, setBusy] = useState(false);
  const [blocked, setBlocked] = useState(false);
  const [revision, setRevision] = useState(0);
  const [notice, setNotice] = useState("");
  const scopes = usePersonReviewScopes(workspace, cursors.at(-1) ?? null);
  const hospital =
    scopes.data?.data.find((row) => row.hospital_id === selected) ?? scopes.data?.data[0];
  const next = scopes.data?.page.next_cursor;
  return (
    <div className="space-y-5">
      <Button
        variant="outline"
        disabled={busy || scopes.isFetching}
        onClick={() => {
          setBlocked(false);
          setNotice("");
          setSelected("");
          setRevision((value) => value + 1);
          setCursors([null]);
          void scopes.refetch();
        }}
      >
        Refresh access
      </Button>
      {notice && !blocked && <p role="status">{notice}</p>}
      {blocked ? (
        <p role="alert">
          Your access or workspace changed. Refresh access before opening another review.
        </p>
      ) : scopes.isPending || scopes.isFetching ? (
        <p role="status">Checking person-review access…</p>
      ) : scopes.isError ? (
        <p role="alert">Could not verify person-review access. Refresh access to try again.</p>
      ) : (
        <>
          {hospital ? (
            <>
              <Field id="person-review-hospital" label="Hospital">
                <SelectControl
                  id="person-review-hospital"
                  value={hospital.hospital_id}
                  disabled={busy}
                  onValueChange={(value) => {
                    setNotice("");
                    setSelected(value);
                  }}
                  options={scopes.data.data.map((row) => ({
                    value: row.hospital_id,
                    label: row.name,
                  }))}
                />
              </Field>
              {hospital.can_submit && (
                <ManagerPersonReviewPreparation
                  key={`prepare:${hospital.hospital_id}:${revision}`}
                  workspace={{ ...workspace, hospitalId: hospital.hospital_id }}
                  busy={busy}
                  onBusy={setBusy}
                  onAccessLost={() => {
                    setBusy(false);
                    setBlocked(true);
                    setNotice("");
                  }}
                  onSubmitted={() => {
                    setBusy(false);
                    setNotice(
                      "Person finding submitted for independent review. Employment and access remain separate decisions.",
                    );
                    setRevision((value) => value + 1);
                  }}
                />
              )}
              <HospitalReviews
                key={`${hospital.hospital_id}:${revision}`}
                workspace={{ ...workspace, hospitalId: hospital.hospital_id }}
                hospital={hospital}
                busy={busy}
                onBusy={setBusy}
                onAccessLost={() => {
                  setBusy(false);
                  setBlocked(true);
                }}
              />
            </>
          ) : (
            <p>No hospitals on this page are currently available for person review.</p>
          )}
          <div className="flex flex-wrap gap-2">
            {cursors.length > 1 && (
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => {
                  setSelected("");
                  setNotice("");
                  setCursors((value) => value.slice(0, -1));
                }}
              >
                Previous hospitals
              </Button>
            )}
            {next && (
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => {
                  setSelected("");
                  setNotice("");
                  setCursors((value) => [...value, next]);
                }}
              >
                Next hospitals
              </Button>
            )}
          </div>
        </>
      )}
    </div>
  );
}

function HospitalReviews({
  workspace,
  hospital,
  busy,
  onBusy,
  onAccessLost,
}: {
  workspace: PersonReviewHospitalWorkspace;
  hospital: PersonReviewHospitalScope;
  busy: boolean;
  onBusy: (value: boolean) => void;
  onAccessLost: () => void;
}) {
  const [status, setStatus] = useState<PersonReviewSummary["status"]>("pending");
  const [cursors, setCursors] = useState<(string | null)[]>([null]);
  const [selected, setSelected] = useState<PersonReviewSummary | null>(null);
  const [notice, setNotice] = useState("");
  const records = usePersonReviewInbox(workspace, status, cursors.at(-1) ?? null);
  const next = records.data?.page.next_cursor;
  return (
    <Panel className="space-y-4 p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="font-semibold">Person review inbox</h2>
          <p className="text-sm text-muted-foreground">
            Recorded findings are checked again when you open them.
          </p>
        </div>
        <Button
          variant="outline"
          disabled={busy || records.isFetching}
          onClick={() => {
            setSelected(null);
            void records.refetch();
          }}
        >
          Refresh inbox
        </Button>
      </div>
      <Field id="person-review-status" label="Recorded status">
        <SelectControl
          id="person-review-status"
          disabled={busy}
          value={status}
          onValueChange={(value) => {
            setStatus(value as PersonReviewSummary["status"]);
            setCursors([null]);
            setSelected(null);
            setNotice("");
          }}
          options={[
            { value: "pending", label: "Pending review" },
            { value: "approved", label: "Approved findings" },
            { value: "rejected", label: "Rejected" },
            { value: "withdrawn", label: "Withdrawn" },
            { value: "revoked", label: "Revoked" },
          ]}
        />
      </Field>
      {notice && <p role="status">{notice}</p>}
      {records.isPending || records.isFetching ? (
        <p role="status">Loading person reviews…</p>
      ) : records.isError ? (
        <p role="alert">
          Could not verify the review inbox. Refresh access or the inbox to try again.
        </p>
      ) : (
        <>
          {records.data.data.length === 0 ? (
            <p>No reviews with this recorded status are shown on this page.</p>
          ) : (
            <ul className="divide-y">
              {records.data.data.map((row) => (
                <li key={row.id} className="flex flex-wrap items-center justify-between gap-3 py-4">
                  <div>
                    <p className="font-medium">{row.display_name}</p>
                    <p className="text-sm text-muted-foreground">
                      {row.employee_code} · {row.status}
                    </p>
                  </div>
                  <Button
                    variant="outline"
                    disabled={busy}
                    onClick={() => {
                      setNotice("");
                      setSelected(row);
                    }}
                  >
                    Open review for {row.display_name}
                  </Button>
                </li>
              ))}
            </ul>
          )}
          <div className="flex gap-2">
            {cursors.length > 1 && (
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => {
                  setSelected(null);
                  setCursors((value) => value.slice(0, -1));
                }}
              >
                Previous reviews
              </Button>
            )}
            {next && (
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => {
                  setSelected(null);
                  setCursors((value) => [...value, next]);
                }}
              >
                Next reviews
              </Button>
            )}
          </div>
        </>
      )}
      {selected && !records.isError && !records.isFetching && (
        <PersonReviewDetail
          key={`${selected.id}:${selected.version}`}
          scope={{ ...workspace, staffId: selected.staff_id }}
          item={selected}
          capabilities={hospital}
          onBusy={onBusy}
          onAccessLost={onAccessLost}
          onClose={() => setSelected(null)}
          onDone={(receipt) => {
            onBusy(false);
            setSelected(null);
            setNotice(
              `Recorded receipt: ${receipt.status}. Reopen the review to check current effectiveness.`,
            );
            void records.refetch();
          }}
        />
      )}
    </Panel>
  );
}
