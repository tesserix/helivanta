"use client";
import { useState } from "react";
import {
  newSupplierWorkflowId,
  hospitalSupplierReceivingKey,
  useSupplierWorkflowQuery,
  listHospitalSupplierCorrectionsPage,
  listHospitalSupplierClearancesPage,
  listHospitalSupplierReleasesPage,
  listHospitalReceivingLocationsPage,
  ReceivingCorrectionInputContract,
  ReceivingClosureInputContract,
  ReceivingClearanceInputContract,
  ReceivingReleaseInputContract,
  type HospitalSupplierReceivingScope,
  type ReceivingReceipt,
  type ReceivingSummary,
} from "@helivanta/api";
import {
  Field,
  Panel,
  RequestFailure,
  SelectControl,
  useZodForm,
  useLeaveGuard,
  useConfirmLeave,
} from "@helivanta/ui";
import { Button, Input } from "@tesserix/web";
import type { ReceivingCommands } from "./receiving-commands";
type Scope = {
  scope: HospitalSupplierReceivingScope;
  shipment: string;
  commands: ReceivingCommands;
};
export function ReceivingClosureForm({
  summary,
  commands,
}: Scope & { summary: ReceivingSummary | null }) {
  const form = useZodForm(ReceivingClosureInputContract.pick({ reason: true }), { reason: "" });
  useLeaveGuard(form.formState.isDirty, "receiving closure draft", commands.frozen);
  if (summary?.closed) return null;
  return (
    <form
      aria-label="Close receiving"
      className="space-y-3"
      onSubmit={(event) => {
        void form.handleSubmit((value) => {
          void commands
            .run({
              kind: "closure",
              id: newSupplierWorkflowId(),
              input: { expected_counted_units: summary?.counted_units ?? 0, reason: value.reason },
            })
            .then((saved) => {
              if (saved) form.reset();
            });
        })(event);
      }}
    >
      <h5 className="font-medium">Close receiving and confirm shortages</h5>
      <p>
        Recorded count: {summary?.counted_units ?? 0}. Closing records the unreceived quantity as
        short; it does not change stock or settle an invoice.
      </p>
      <Field
        id="receiving-close-reason"
        label="Closure and shortage reason"
        error={form.formState.errors.reason?.message}
      >
        <Input
          id="receiving-close-reason"
          disabled={commands.frozen}
          {...form.register("reason")}
        />
      </Field>
      <Button type="submit" disabled={commands.frozen}>
        Close receiving
      </Button>
    </form>
  );
}
export function ReceivingReceiptInspection({
  scope,
  shipment,
  receipt,
  summary,
  commands,
}: Scope & { receipt: ReceivingReceipt; summary: ReceivingSummary | null }) {
  const confirm = useConfirmLeave();
  const [clearanceCursor, setClearanceCursor] = useState<string | null>(null);
  const [cursor, setCursor] = useState<string | null>(null);
  const corrections = useSupplierWorkflowQuery(
    hospitalSupplierReceivingKey(scope, shipment, receipt.id, "corrections", cursor ?? ""),
    (signal) =>
      listHospitalSupplierCorrectionsPage(scope, shipment, receipt.id, { cursor }, signal),
  );
  const clearances = useSupplierWorkflowQuery(
    hospitalSupplierReceivingKey(scope, shipment, receipt.id, "clearances", clearanceCursor ?? ""),
    (signal) =>
      listHospitalSupplierClearancesPage(
        scope,
        shipment,
        receipt.id,
        { cursor: clearanceCursor },
        signal,
      ),
  );
  const [releaseCursor, setReleaseCursor] = useState<string | null>(null);
  const releases = useSupplierWorkflowQuery(
    hospitalSupplierReceivingKey(scope, shipment, receipt.id, "releases", releaseCursor ?? ""),
    (signal) =>
      listHospitalSupplierReleasesPage(
        scope,
        shipment,
        receipt.id,
        { cursor: releaseCursor },
        signal,
      ),
  );
  const effective = corrections.isSuccess && !cursor ? corrections.data.data[0] : null;
  const accepted = effective?.input.accepted_units ?? receipt.input.accepted_units;
  const version = effective?.version ?? 1;
  return (
    <Panel className="space-y-3" aria-label={`Receipt ${receipt.id}`}>
      <p>
        Original receipt: {receipt.input.counted_units} counted · {receipt.input.accepted_units}{" "}
        accepted · {receipt.input.rejected_units} rejected.
      </p>
      <p>
        {receipt.input.reason} · {receipt.actor_subject} ·{" "}
        {new Date(receipt.created_at).toLocaleString("en-IN")}
      </p>
      <p>Original evidence is retained alongside corrections and physical release history.</p>
      {corrections.isError ? (
        <RequestFailure
          message="Correction history could not be verified."
          error={corrections.error}
          retry={corrections.refetch}
        />
      ) : corrections.isPending ? (
        <p role="status">Loading corrections…</p>
      ) : (
        <>
          {corrections.data.data.length ? (
            <ul>
              {corrections.data.data.map((correction) => (
                <li key={correction.id}>
                  Correction {correction.version}: {correction.input.accepted_units} accepted,{" "}
                  {correction.input.rejected_units} rejected · {correction.input.reason} ·{" "}
                  {correction.actor_subject}
                </li>
              ))}
            </ul>
          ) : (
            <p>No corrections recorded.</p>
          )}
          <HistoryPage
            cursor={cursor}
            next={corrections.data.page.next_cursor}
            disabled={commands.frozen}
            change={(value) => confirm(() => setCursor(value))}
          />
          {!cursor && (
            <>
              <p>Effective accepted units for this receipt: {accepted}.</p>
              {!summary?.closed && (
                <CorrectionForm
                  scope={scope}
                  shipment={shipment}
                  receipt={receipt}
                  expectedVersion={version}
                  counts={{
                    accepted_units: effective?.input.accepted_units ?? receipt.input.accepted_units,
                    rejected_units: effective?.input.rejected_units ?? receipt.input.rejected_units,
                  }}
                  priorSubject={effective?.actor_subject ?? receipt.actor_subject}
                  summaryVersion={summary?.version ?? 0}
                  commands={commands}
                />
              )}
              {summary?.closed && accepted > 0 && (
                <>
                  <ClearanceForm
                    scope={scope}
                    shipment={shipment}
                    receipt={receipt}
                    summary={summary}
                    commands={commands}
                  />
                  <ReceiptReleaseForm
                    scope={scope}
                    shipment={shipment}
                    receipt={receipt}
                    summary={summary}
                    commands={commands}
                  />
                </>
              )}
            </>
          )}
        </>
      )}
      <h6 className="font-medium">Independent review evidence</h6>
      {clearances.isError ? (
        <RequestFailure
          message="Clearance history could not be verified."
          error={clearances.error}
          retry={clearances.refetch}
        />
      ) : clearances.isPending ? (
        <p role="status">Loading independent review…</p>
      ) : clearances.data.data.length ? (
        <>
          <ul>
            {clearances.data.data.map((clearance) => (
              <li key={clearance.id}>
                Review at receiving version {clearance.summary_version}: {clearance.reason} ·{" "}
                {clearance.actor_subject}.{" "}
                {clearance.summary_version === summary?.version
                  ? "Current version."
                  : "Earlier version; cannot authorize current release."}
              </li>
            ))}
          </ul>
        </>
      ) : (
        <p>No independent clearance recorded.</p>
      )}
      {clearances.isSuccess && (
        <HistoryPage
          cursor={clearanceCursor}
          next={clearances.data.page.next_cursor}
          disabled={commands.frozen}
          change={(value) => confirm(() => setClearanceCursor(value))}
        />
      )}
      <h6 className="font-medium">Actual physical releases</h6>
      {releases.isError ? (
        <RequestFailure
          message="Physical release history could not be verified."
          error={releases.error}
          retry={releases.refetch}
        />
      ) : releases.isPending ? (
        <p role="status">Loading physical releases…</p>
      ) : (
        <>
          {releases.data.data.length ? (
            <ul>
              {releases.data.data.map((release) => (
                <li key={release.id}>
                  {release.input.quantity_units} units physically released · {release.input.reason}{" "}
                  · {release.actor_subject} · {new Date(release.created_at).toLocaleString("en-IN")}
                </li>
              ))}
            </ul>
          ) : (
            <p>No physical release recorded.</p>
          )}
          <HistoryPage
            cursor={releaseCursor}
            next={releases.data.page.next_cursor}
            change={(value) => confirm(() => setReleaseCursor(value))}
            disabled={commands.frozen}
          />
        </>
      )}
    </Panel>
  );
}
function HistoryPage({
  cursor,
  next,
  change,
  disabled,
}: {
  cursor: string | null;
  next: string | null;
  change: (cursor: string | null) => void;
  disabled: boolean;
}) {
  return (
    <div className="flex gap-2">
      {cursor && (
        <Button type="button" variant="outline" disabled={disabled} onClick={() => change(null)}>
          Latest evidence
        </Button>
      )}
      {next && (
        <Button type="button" variant="outline" disabled={disabled} onClick={() => change(next)}>
          Earlier evidence
        </Button>
      )}
    </div>
  );
}
function CorrectionForm({
  scope,
  receipt,
  expectedVersion,
  summaryVersion,
  commands,
  counts,
  priorSubject,
}: Scope & {
  receipt: ReceivingReceipt;
  expectedVersion: number;
  summaryVersion: number;
  counts: { accepted_units: number; rejected_units: number };
  priorSubject: string;
}) {
  const confirm = useConfirmLeave();
  const [snapshot, setSnapshot] = useState(() => ({
    receiptVersion: expectedVersion,
    summaryVersion,
    counts,
  }));
  const changed =
    snapshot.receiptVersion !== expectedVersion || snapshot.summaryVersion !== summaryVersion;
  const selfReview = scope.subject === receipt.actor_subject || scope.subject === priorSubject;
  const frozen = commands.frozen || changed || selfReview;
  const form = useZodForm(
    ReceivingCorrectionInputContract.pick({
      accepted_units: true,
      rejected_units: true,
      reason: true,
      identity_confirmed: true,
    }),
    {
      accepted_units: snapshot.counts.accepted_units,
      rejected_units: snapshot.counts.rejected_units,
      identity_confirmed: false,
      reason: "",
    },
  );
  useLeaveGuard(form.formState.isDirty, "receipt correction draft", commands.frozen);
  return (
    <form
      aria-label={`Correct receipt ${receipt.id}`}
      className="space-y-3"
      onSubmit={(event) => {
        void form.handleSubmit((value) => {
          if (frozen) return;
          void commands
            .run({
              kind: "correction",
              receipt: receipt.id,
              id: newSupplierWorkflowId(),
              input: {
                ...value,
                expected_summary_version: snapshot.summaryVersion,
                expected_receipt_version: snapshot.receiptVersion,
                counted_units: value.accepted_units + value.rejected_units,
              },
            })
            .then((saved) => {
              if (saved) form.reset(value);
            });
        })(event);
      }}
    >
      <h6 className="font-medium">Correct the original count</h6>
      {selfReview && (
        <p>An independent person must correct the original or their previous reviewed count.</p>
      )}
      {changed && (
        <>
          <p role="alert">
            Receiving evidence changed. Recheck the latest count before correcting it.
          </p>
          <Button
            type="button"
            variant="outline"
            disabled={commands.frozen}
            onClick={() =>
              confirm(() => {
                setSnapshot({ receiptVersion: expectedVersion, summaryVersion, counts });
                form.reset({ ...counts, identity_confirmed: false, reason: "" });
              })
            }
          >
            Reload latest correction evidence
          </Button>
        </>
      )}

      {(
        [
          ["accepted_units", "Corrected accepted units"],
          ["rejected_units", "Corrected rejected units"],
        ] as const
      ).map(([name, label]) => (
        <Field
          key={name}
          id={`${receipt.id}-${name}`}
          label={label}
          error={form.formState.errors[name]?.message}
        >
          <Input
            id={`${receipt.id}-${name}`}
            type="number"
            min="0"
            step="1"
            disabled={frozen}
            {...form.register(name, { valueAsNumber: true })}
          />
        </Field>
      ))}
      <Field
        id={`${receipt.id}-identity-recheck`}
        label="I re-inspected medicine identity before increasing accepted units"
        error={form.formState.errors.identity_confirmed?.message}
      >
        <Input
          id={`${receipt.id}-identity-recheck`}
          type="checkbox"
          disabled={frozen}
          {...form.register("identity_confirmed")}
        />
      </Field>
      <Field
        id={`${receipt.id}-correction-reason`}
        label="Correction evidence and reason"
        error={form.formState.errors.reason?.message}
      >
        <Input
          id={`${receipt.id}-correction-reason`}
          disabled={frozen}
          {...form.register("reason")}
        />
      </Field>
      <Button type="submit" disabled={frozen}>
        Record corrected count
      </Button>
    </form>
  );
}
function ClearanceForm({
  scope,
  receipt,
  summary,
  commands,
}: Scope & { receipt: ReceivingReceipt; summary: ReceivingSummary }) {
  const form = useZodForm(ReceivingClearanceInputContract.pick({ reason: true }), { reason: "" });
  useLeaveGuard(form.formState.isDirty, "independent receipt review draft", commands.frozen);
  const sameRecorder = receipt.actor_subject === scope.subject;
  return (
    <form
      aria-label={`Review receipt ${receipt.id}`}
      className="space-y-3"
      onSubmit={(event) => {
        void form.handleSubmit((value) => {
          void commands
            .run({
              kind: "clearance",
              receipt: receipt.id,
              id: newSupplierWorkflowId(),
              input: { expected_summary_version: summary.version, reason: value.reason },
            })
            .then((saved) => {
              if (saved) form.reset();
            });
        })(event);
      }}
    >
      <h6 className="font-medium">Independent review at version {summary.version}</h6>
      <p>Review does not move stock. The original recorder cannot review their receipt.</p>
      <Field
        id={`${receipt.id}-clearance-reason`}
        label="Independent review evidence"
        error={form.formState.errors.reason?.message}
      >
        <Input
          id={`${receipt.id}-clearance-reason`}
          disabled={commands.frozen || sameRecorder}
          {...form.register("reason")}
        />
      </Field>
      <Button type="submit" disabled={commands.frozen || sameRecorder}>
        Record independent clearance
      </Button>
    </form>
  );
}
function ReceiptReleaseForm({
  scope,
  shipment,
  receipt,
  summary,
  commands,
}: Scope & { receipt: ReceivingReceipt; summary: ReceivingSummary }) {
  const [cursor, setCursor] = useState<string | null>(null),
    [location, setLocation] = useState("");
  const shelves = useSupplierWorkflowQuery(
    hospitalSupplierReceivingKey(scope, shipment, "sellable-locations", cursor ?? ""),
    (signal) => listHospitalReceivingLocationsPage(scope, shipment, "sellable", { cursor }, signal),
  );
  const form = useZodForm(
    ReceivingReleaseInputContract.pick({ quantity_units: true, reason: true }),
    { quantity_units: 1, reason: "" },
  );
  useLeaveGuard(
    form.formState.isDirty || !!location,
    "receipt physical release draft",
    commands.frozen,
  );
  const sameRecorder = receipt.actor_subject === scope.subject;
  return (
    <form
      aria-label={`Release receipt ${receipt.id}`}
      className="space-y-3"
      onSubmit={(event) => {
        void form.handleSubmit((value) => {
          if (!location) return;
          void commands
            .run({
              kind: "release",
              receipt: receipt.id,
              id: newSupplierWorkflowId(),
              input: {
                ...value,
                expected_summary_version: summary.version,
                to_location_id: location,
              },
            })
            .then((saved) => {
              if (saved) {
                form.reset();
                setLocation("");
              }
            });
        })(event);
      }}
    >
      <h6 className="font-medium">Physically release reviewed quarantine stock</h6>
      <p>
        Choose the actual sellable shelf and shelved quantity. Units must fit this receipt’s
        unreleased accepted stock. Current clearance and safety checks remain required.
      </p>
      {shelves.isError ? (
        <RequestFailure
          message="Sellable shelves could not be verified."
          error={shelves.error}
          retry={shelves.refetch}
        />
      ) : shelves.isPending ? (
        <p role="status">Loading sellable shelves…</p>
      ) : (
        <>
          <Field id={`${receipt.id}-release-shelf`} label="Actual sellable shelf">
            <SelectControl
              id={`${receipt.id}-release-shelf`}
              value={location}
              disabled={commands.frozen || sameRecorder}
              placeholder="Choose actual shelf"
              options={shelves.data.data.map((value) => ({
                value: value.id,
                label: `${value.code} · ${value.name}`,
              }))}
              onValueChange={setLocation}
            />
          </Field>
          <HistoryPage
            cursor={cursor}
            next={shelves.data.page.next_cursor}
            disabled={commands.frozen}
            change={(value) => {
              setCursor(value);
              setLocation("");
            }}
          />
        </>
      )}
      <Field
        id={`${receipt.id}-release-quantity`}
        label="Physically shelved units"
        error={form.formState.errors.quantity_units?.message}
      >
        <Input
          id={`${receipt.id}-release-quantity`}
          type="number"
          min="1"
          step="1"
          disabled={commands.frozen || sameRecorder}
          {...form.register("quantity_units", { valueAsNumber: true })}
        />
      </Field>
      <Field
        id={`${receipt.id}-release-reason`}
        label="Physical release evidence"
        error={form.formState.errors.reason?.message}
      >
        <Input
          id={`${receipt.id}-release-reason`}
          disabled={commands.frozen || sameRecorder}
          {...form.register("reason")}
        />
      </Field>
      <Button
        type="submit"
        disabled={commands.frozen || sameRecorder || !location || !shelves.isSuccess}
      >
        Record physical release
      </Button>
    </form>
  );
}
