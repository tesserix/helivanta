"use client";

import { useState, useSyncExternalStore } from "react";
import { Button } from "@tesserix/web";
import { Field, PageHeading, Panel, SelectControl } from "@helivanta/ui";
import {
  useApiQuery,
  useTenantTransition,
  useEmploymentReviewScopes,
  useEmploymentReviewInbox,
  type EmploymentReviewHospitalScope,
  type EmploymentReviewHospitalWorkspace,
  type EmploymentReviewSummary,
  type PermissionsResponse,
} from "@helivanta/api";
import { EmploymentReviewDetail } from "./review-detail";

const subscribeHydration = () => () => {};
type Workspace = Pick<EmploymentReviewHospitalWorkspace, "tenantId" | "subject">;

export default function EmploymentReviews() {
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
      <PageHeading description="Examine appointment or contract terms and selected evidence. Approved terms require separate employment activation and access decisions." />
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
  const scopes = useEmploymentReviewScopes(workspace, cursors.at(-1) ?? null);
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
        <p role="status">Checking employment-review access…</p>
      ) : scopes.isError ? (
        <p role="alert">Could not verify employment-review access. Refresh access to try again.</p>
      ) : (
        <>
          {hospital ? (
            <>
              <Field id="employment-review-hospital" label="Hospital">
                <SelectControl
                  id="employment-review-hospital"
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
              <HospitalReviews
                key={`${hospital.hospital_id}:${revision}`}
                workspace={{
                  ...workspace,
                  hospitalId: hospital.hospital_id,
                  mode: hospital.can_review ? "review" : "prepare",
                }}
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
            <p>No hospitals on this page are currently available for employment review.</p>
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
  workspace: EmploymentReviewHospitalWorkspace;
  hospital: EmploymentReviewHospitalScope;
  busy: boolean;
  onBusy: (value: boolean) => void;
  onAccessLost: () => void;
}) {
  const [mode, setMode] = useState<"prepare" | "review">(workspace.mode);
  const activeWorkspace = { ...workspace, mode };
  const [status, setStatus] = useState<EmploymentReviewSummary["status"]>("pending");
  const [cursors, setCursors] = useState<(string | null)[]>([null]);
  const [selected, setSelected] = useState<EmploymentReviewSummary | null>(null);
  const [notice, setNotice] = useState("");
  const records = useEmploymentReviewInbox(activeWorkspace, status, cursors.at(-1) ?? null);
  const next = records.data?.page.next_cursor;
  return (
    <Panel className="space-y-4 p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="font-semibold">Employment review inbox</h2>
          <p className="text-sm text-muted-foreground">
            Recorded terms are checked again when you open them.
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
      {hospital.can_submit && hospital.can_review && (
        <Field id="employment-review-mode" label="Workspace role">
          <SelectControl
            id="employment-review-mode"
            value={mode}
            disabled={busy}
            options={[
              { value: "review", label: "Independent reviewer" },
              { value: "prepare", label: "HR preparer" },
            ]}
            onValueChange={(value) => {
              setMode(value as "prepare" | "review");
              setSelected(null);
              setCursors([null]);
              setNotice("");
            }}
          />
        </Field>
      )}
      <Field id="employment-review-status" label="Recorded status">
        <SelectControl
          id="employment-review-status"
          disabled={busy}
          value={status}
          onValueChange={(value) => {
            setStatus(value as EmploymentReviewSummary["status"]);
            setCursors([null]);
            setSelected(null);
            setNotice("");
          }}
          options={[
            { value: "pending", label: "Pending review" },
            { value: "approved", label: "Approved terms" },
            { value: "rejected", label: "Rejected" },
            { value: "withdrawn", label: "Withdrawn" },
            { value: "revoked", label: "Revoked" },
          ]}
        />
      </Field>
      {notice && <p role="status">{notice}</p>}
      {records.isPending || records.isFetching ? (
        <p role="status">Loading employment reviews…</p>
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
        <EmploymentReviewDetail
          key={`${selected.id}:${selected.version}`}
          scope={
            mode === "review"
              ? { ...workspace, staffId: selected.staff_id, employmentReview: true }
              : { ...workspace, staffId: selected.staff_id, hrIntake: true }
          }
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
