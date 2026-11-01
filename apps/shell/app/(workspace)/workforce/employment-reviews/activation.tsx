"use client";
import { useEffect, useId, useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import { Field, Panel, useLeaveGuard, useZodForm } from "@helivanta/ui";
import {
  ApiError,
  EmploymentActivationInputContract,
  getEmploymentActivationSource,
  activateEmployment,
  newEmploymentActivationId,
  useApiMutation,
  type EmploymentActivationScope,
  type EmploymentActivationSource,
  type EmploymentActivationInput,
  type EmploymentGenerationView,
  type EmploymentReview,
} from "@helivanta/api";

type Props = {
  scope: EmploymentActivationScope;
  review: Pick<EmploymentReview, "id" | "revision_id" | "version">;
  onBusy: (busy: boolean) => void;
  onAccessLost: () => void;
};
type Frozen = { id: string; input: EmploymentActivationInput };
const schema = EmploymentActivationInputContract.pick({ reason: true, activation_examined: true });
const labels: Record<EmploymentActivationSource["current_state"], string> = {
  eligible: "The independently approved appointment is eligible for activation.",
  not_approved: "An independently approved appointment is required.",
  source_changed: "Staff details or approved terms changed. Obtain a new review.",
  person_unavailable: "A current independently approved person finding is required.",
  evidence_unavailable: "Supporting evidence is unavailable. Resolve it before activation.",
  not_started: "The appointment has not started.",
  expired: "The appointment or supporting verification has expired.",
  already_active: "This Staff record is already active.",
  associated_actor: "Another authorized person must activate this appointment.",
  consumed: "An activation is already recorded for these approved terms.",
  identity_changed:
    "Identity details changed. Obtain a fresh person finding and appointment review.",
  identity_unavailable:
    "The identity basis needs a fresh independent finding and appointment review.",
};
const date = (value: string) =>
  new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
    timeZone: "UTC",
  }).format(new Date(value)) + " UTC";

export function EmploymentActivation({ scope, review, onBusy, onAccessLost }: Props) {
  const id = useId();
  const controller = useRef<AbortController | null>(null);
  const [source, setSource] = useState<EmploymentActivationSource | null>(null);
  const [frozen, setFrozen] = useState<Frozen | null>(null);
  const [receipt, setReceipt] = useState<EmploymentGenerationView | null>(null);
  const form = useZodForm(schema, { reason: "", activation_examined: false });
  const failure = (error: Error) => {
    if (controller.current?.signal.aborted) return;
    if (error instanceof ApiError && [401, 403, 404, 409].includes(error.status)) {
      setSource(null);
      setFrozen(null);
      form.reset();
      onAccessLost();
    } else if (error instanceof ApiError && error.status === 400) {
      setFrozen(null);
    }
  };
  const eligibility = useApiMutation(
    async () => {
      controller.current?.abort();
      controller.current = new AbortController();
      const next = await getEmploymentActivationSource(scope, review.id, controller.current.signal);
      if (next.review_version !== review.version || next.review_revision_id !== review.revision_id)
        throw new Error("The approved review changed. Reopen the Staff workspace.");
      return next;
    },
    { suppressErrorToast: true, onError: failure, onSuccess: setSource },
  );
  const mutation = useApiMutation(
    async (command: Frozen) => {
      controller.current?.abort();
      controller.current = new AbortController();
      return activateEmployment(scope, command.id, command.input, controller.current.signal);
    },
    {
      suppressErrorToast: true,
      onError: failure,
      onSuccess: (result) => {
        setReceipt(result);
        setFrozen(null);
        form.reset();
      },
    },
  );
  const pending = eligibility.isPending || mutation.isPending;
  useLeaveGuard(
    form.formState.isDirty || frozen !== null,
    "employment activation",
    mutation.isPending,
  );
  useEffect(() => {
    onBusy(form.formState.isDirty || frozen !== null || pending);
  }, [form.formState.isDirty, frozen, pending, onBusy]);
  useEffect(
    () => () => {
      controller.current?.abort();
      onBusy(false);
    },
    [onBusy],
  );
  const submit = form.handleSubmit((values) => {
    if (!source?.can_activate) return;
    const command = frozen ?? {
      id: newEmploymentActivationId(),
      input: {
        command_id: newEmploymentActivationId(),
        review_id: source.review_id,
        review_revision_id: source.review_revision_id,
        review_version: source.review_version,
        staff_version: source.staff_version,
        employment_epoch: source.employment_epoch,
        reason: values.reason,
        activation_examined: values.activation_examined,
      },
    };
    setFrozen(command);
    mutation.mutate(command);
  });
  const error = mutation.error ?? eligibility.error;
  return (
    <Panel aria-label="Employment activation" aria-busy={pending} className="space-y-4">
      <div>
        <h3 className="font-semibold">Employment activation</h3>
        <p className="mt-1 text-sm text-muted-foreground">
          Activate an independently approved appointment. Login and scoped role assignments require
          separate approval.
        </p>
      </div>
      {receipt ? (
        <div role="status" className="space-y-2">
          <p className="font-medium">Employment activation recorded.</p>
          <p className="text-sm">Login and role approvals are still separate steps.</p>
          <p className="text-sm text-muted-foreground">
            Recorded {date(receipt.generation.activated_at)}
          </p>
        </div>
      ) : (
        <>
          {!source && (
            <Button disabled={pending} onClick={() => eligibility.mutate()}>
              {eligibility.isPending ? "Checking eligibility…" : "Check activation eligibility"}
            </Button>
          )}
          {source && (
            <>
              <p role="status" className="text-sm">
                {labels[source.current_state]}
              </p>
              <dl className="grid gap-4 text-sm sm:grid-cols-2">
                <div>
                  <dt className="text-muted-foreground">Effective from</dt>
                  <dd className="mt-1 font-medium">{date(source.starts_at)}</dd>
                </div>
                <div>
                  <dt className="text-muted-foreground">Appointment end</dt>
                  <dd className="mt-1 font-medium">
                    {source.ends_at ? date(source.ends_at) : "Ongoing"}
                  </dd>
                </div>
              </dl>
            </>
          )}
          {source && !source.can_activate && (
            <Button disabled={pending} onClick={() => eligibility.mutate()}>
              Refresh eligibility
            </Button>
          )}
          {source?.can_activate && (
            <form onSubmit={submit} className="space-y-4" noValidate>
              <Field
                label="Activation reason"
                id={id + "-reason"}
                error={
                  form.formState.errors.reason?.type === "too_small"
                    ? "Enter a reason for this activation."
                    : form.formState.errors.reason?.message
                }
              >
                <Input
                  id={id + "-reason"}
                  required
                  maxLength={500}
                  disabled={pending || frozen !== null}
                  aria-invalid={Boolean(form.formState.errors.reason)}
                  aria-describedby={form.formState.errors.reason ? id + "-reason-error" : undefined}
                  {...form.register("reason")}
                />
              </Field>
              <label className="flex items-start gap-3 text-sm">
                <input
                  type="checkbox"
                  disabled={pending || frozen !== null}
                  aria-invalid={Boolean(form.formState.errors.activation_examined)}
                  aria-describedby={
                    form.formState.errors.activation_examined
                      ? id + "-confirmation-error"
                      : undefined
                  }
                  className="mt-0.5"
                  {...form.register("activation_examined")}
                />
                <span>I examined this approved appointment and its effective dates.</span>
              </label>
              {form.formState.errors.activation_examined && (
                <p
                  id={id + "-confirmation-error"}
                  role="alert"
                  className="text-sm text-destructive"
                >
                  {form.formState.errors.activation_examined.message}
                </p>
              )}
              <Button type="submit" disabled={pending}>
                {mutation.isPending
                  ? "Recording activation…"
                  : frozen
                    ? "Retry same activation"
                    : "Activate employment"}
              </Button>
            </form>
          )}
        </>
      )}
      {error && error.name !== "AbortError" && (
        <p role="alert" className="text-sm text-destructive">
          {error.message}
        </p>
      )}
    </Panel>
  );
}
