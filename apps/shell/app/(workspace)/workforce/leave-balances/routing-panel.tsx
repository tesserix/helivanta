"use client";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  leaveRoutingPolicySchema,
  useApiMutation,
  useApiQuery,
  LEAVE_ROUTING_MODES,
  type Hospital,
  type LeaveRoutingPolicy,
  type LeaveRoutingPolicyInput,
} from "@helivanta/api";
import { Field, Panel, SelectControl, useZodForm, RequestFailure } from "@helivanta/ui";
import { inputClass } from "../roster-policy/policy-ui";
import { LeaveReview, SaveError } from "./years-panel";

const modeOptions = Object.entries(LEAVE_ROUTING_MODES).map(([value, label]) => ({
  value,
  label,
}));

export function LeaveRoutingPanel({
  hospital,
  writable,
}: {
  hospital: Hospital;
  writable: boolean;
}) {
  const path = `/workforce/hospitals/${hospital.id}/hr/leave-routing`;
  const policy = useApiQuery<LeaveRoutingPolicy>(["workforce", "leave-routing", hospital.id], path);
  return (
    <Panel aria-label="Leave approval routing">
      <h3 className="font-semibold">Leave approval routing</h3>
      {policy.isPending ? (
        <p role="status" className="mt-3">
          Loading routing policy…
        </p>
      ) : policy.isError ? (
        <div className="mt-3">
          <RequestFailure
            message="Could not load the routing policy."
            retry={() => void policy.refetch()}
            error={policy.error}
          />
        </div>
      ) : (
        <>
          <PolicySummary policy={policy.data} />
          {writable && (
            <RoutingForm
              key={policy.data.version}
              hospital={hospital.id}
              path={path}
              policy={policy.data}
              reload={() => void policy.refetch()}
            />
          )}
        </>
      )}
    </Panel>
  );
}

function PolicySummary({ policy }: { policy: LeaveRoutingPolicy }) {
  if (policy.version === 0)
    return (
      <p className="mt-2 text-sm text-muted-foreground">
        No routing policy is saved, so the hospital leave desk decides every request.
      </p>
    );
  return (
    <div className="mt-2 space-y-1 text-sm">
      <p>
        Version {policy.version} · {LEAVE_ROUTING_MODES[policy.mode]}
        {policy.escalation_hours !== undefined &&
          ` · escalates every ${policy.escalation_hours} hours`}
      </p>
      <p className="text-muted-foreground">
        Source: {policy.source_reference}
        {policy.reviewed_by_subject && ` · reviewed by ${policy.reviewed_by_subject}`}
        {policy.created_at && ` · ${new Date(policy.created_at).toLocaleString()}`}
      </p>
      <p className="text-muted-foreground">
        A change applies to requests filed after it is saved. Filed requests keep their route.
      </p>
    </div>
  );
}

function RoutingForm({
  hospital,
  path,
  policy,
  reload,
}: {
  hospital: string;
  path: string;
  policy: LeaveRoutingPolicy;
  reload: () => void;
}) {
  const form = useZodForm(leaveRoutingPolicySchema, {
    expected_version: policy.version,
    mode: policy.mode,
    escalation_hours: policy.escalation_hours,
    source_reference: policy.version ? policy.source_reference : "",
    reviewed: false,
  });
  const save = useApiMutation(
    (input: LeaveRoutingPolicyInput) =>
      apiFetch<LeaveRoutingPolicy>(path, { method: "PUT", body: JSON.stringify(input) }),
    {
      invalidate: [["workforce", "leave-routing", hospital]],
      successToast: "Routing policy saved",
      suppressErrorToast: true,
    },
  );
  const errors = form.formState.errors;
  const mode = form.watch("mode");
  return (
    <form
      noValidate
      aria-label="Edit leave approval routing"
      onSubmit={form.handleSubmit((input) => save.mutate(input))}
      className="mt-4 space-y-5"
    >
      <fieldset disabled={save.isPending} className="grid gap-4 sm:grid-cols-2">
        <Field id="leave-routing-mode" label="Who decides leave first">
          <SelectControl
            id="leave-routing-mode"
            value={mode}
            onValueChange={(v) => {
              form.setValue("mode", v as LeaveRoutingPolicy["mode"], { shouldDirty: true });
              if (v === "hospital") form.setValue("escalation_hours", undefined);
            }}
            options={modeOptions}
          />
        </Field>
        {mode === "reporting_line" && (
          <Field
            id="leave-routing-hours"
            label="Escalate to the next level after (hours)"
            error={errors.escalation_hours?.message}
          >
            <input
              id="leave-routing-hours"
              type="number"
              inputMode="numeric"
              className={inputClass}
              aria-invalid={!!errors.escalation_hours}
              {...form.register("escalation_hours", {
                setValueAs: (v: unknown) => (v === "" || v == null ? undefined : Number(v)),
              })}
            />
          </Field>
        )}
        <div className="sm:col-span-2">
          <Field
            id="leave-routing-source"
            label="Source reference"
            error={errors.source_reference?.message}
          >
            <input
              id="leave-routing-source"
              className={inputClass}
              placeholder="HR circular or order number"
              aria-invalid={!!errors.source_reference}
              {...form.register("source_reference")}
            />
          </Field>
        </div>
        <div className="sm:col-span-2">
          <LeaveReview
            id="leave-routing-review"
            error={errors.reviewed?.message}
            register={form.register("reviewed")}
          >
            I reviewed this policy against the hospital&apos;s approved leave order
          </LeaveReview>
        </div>
      </fieldset>
      {save.isError && <SaveError message={save.error.message} />}
      <div className="flex flex-wrap justify-end gap-3">
        {save.isError && (
          <Button type="button" variant="outline" onClick={reload}>
            Reload policy
          </Button>
        )}
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Saving…" : "Save routing policy"}
        </Button>
      </div>
    </form>
  );
}
