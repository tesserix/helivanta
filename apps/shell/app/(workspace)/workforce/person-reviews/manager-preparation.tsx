"use client";

import { useEffect, useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import { Controller, Field, Panel, SelectControl, useLeaveGuard, useZodForm } from "@helivanta/ui";
import {
  ApiError,
  PersonReviewTargetSearchInputContract,
  PersonReviewSubmissionInputContract,
  usePersonReviewTargets,
  usePersonReviewPreparationSource,
  usePersonReviewEvidenceOptions,
  useApiMutation,
  submitPersonReview,
  newPersonReviewId,
  type PersonReviewHospitalWorkspace,
  type PersonReviewScope,
  type PersonReviewTarget,
  type PersonReviewSubmissionInput,
  type IdentitySource,
} from "@helivanta/api";
import { useStaffAccessCutoff } from "../staff-access-cutoff";
import { personReviewContextChanged } from "./person-review-context";

type Props = {
  workspace: PersonReviewHospitalWorkspace;
  busy: boolean;
  onBusy: (value: boolean) => void;
  onAccessLost: () => void;
  onSubmitted: () => void;
};
type PreparationProps = Pick<Props, "onBusy" | "onAccessLost" | "onSubmitted"> & {
  scope: PersonReviewScope;
  onClose: () => void;
};
const submissionFields = PersonReviewSubmissionInputContract.omit({
  command_id: true,
  staff_version: true,
  employment_epoch: true,
  identity_hash: true,
});

export function ManagerPersonReviewPreparation({
  workspace,
  busy,
  onBusy,
  onAccessLost,
  onSubmitted,
}: Props) {
  const [search, setSearch] = useState("");
  const [input, setInput] = useState("");
  const [searchError, setSearchError] = useState("");
  const [cursors, setCursors] = useState<(string | null)[]>([null]);
  const [selected, setSelected] = useState<PersonReviewTarget | null>(null);
  const targets = usePersonReviewTargets(workspace, search, cursors.at(-1) ?? null);
  const cutoff = useStaffAccessCutoff([targets.error], onAccessLost, personReviewContextChanged);
  return (
    <Panel className="space-y-4 p-5">
      <h2 className="font-semibold">Prepare a person finding</h2>
      <p className="text-sm text-muted-foreground">
        Choose the applicant’s Staff record, including inactive joiners. Submission requires
        independent review.
      </p>
      {cutoff ? (
        <p role="alert">Staff selection access changed. Refresh access to continue.</p>
      ) : (
        <>
          <form
            noValidate
            className="flex flex-wrap items-end gap-3"
            onSubmit={(event) => {
              event.preventDefault();
              const parsed = PersonReviewTargetSearchInputContract.safeParse({
                employee_code: input,
              });
              if (!parsed.success) {
                setSearchError("Enter a valid Staff ID, or leave it empty to browse.");
                return;
              }
              setSearchError("");
              setSearch(parsed.data.employee_code);
              setCursors([null]);
              setSelected(null);
            }}
          >
            <Field id="person-target-code" label="Staff ID" error={searchError}>
              <Input
                id="person-target-code"
                maxLength={40}
                value={input}
                disabled={busy}
                onChange={(event) => setInput(event.target.value)}
              />
            </Field>
            <Button type="submit" variant="outline" disabled={busy || targets.isFetching}>
              Search staff
            </Button>
          </form>
          {targets.isPending || targets.isFetching ? (
            <p role="status">Loading Staff records…</p>
          ) : targets.isError ? (
            <div role="alert">
              <p>Could not verify the Staff list.</p>
              <Button variant="outline" disabled={busy} onClick={() => void targets.refetch()}>
                Retry Staff search
              </Button>
            </div>
          ) : (
            <>
              {targets.data.data.length === 0 ? (
                <p>No Staff records match this search on this page.</p>
              ) : (
                <ul className="divide-y">
                  {targets.data.data.map((target) => (
                    <li
                      key={target.id}
                      className="flex flex-wrap items-center justify-between gap-3 py-3"
                    >
                      <div>
                        <p className="font-medium">{target.display_name}</p>
                        <p className="text-sm text-muted-foreground">
                          {target.employee_code} · {target.employment_status}
                        </p>
                      </div>
                      <Button variant="outline" disabled={busy} onClick={() => setSelected(target)}>
                        Prepare review for {target.display_name}
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
                    Previous Staff records
                  </Button>
                )}
                {targets.data.page.next_cursor && (
                  <Button
                    variant="outline"
                    disabled={busy}
                    onClick={() => {
                      setSelected(null);
                      setCursors((value) => [...value, targets.data.page.next_cursor]);
                    }}
                  >
                    Next Staff records
                  </Button>
                )}
              </div>
            </>
          )}
          {selected && !targets.isError && !targets.isFetching && (
            <PreparationSource
              key={selected.id}
              scope={{ ...workspace, staffId: selected.id }}
              onBusy={onBusy}
              onAccessLost={onAccessLost}
              onSubmitted={onSubmitted}
              onClose={() => setSelected(null)}
            />
          )}
        </>
      )}
    </Panel>
  );
}

function PreparationSource(props: PreparationProps) {
  const source = usePersonReviewPreparationSource(props.scope);
  const cutoff = useStaffAccessCutoff(
    [source.error],
    props.onAccessLost,
    personReviewContextChanged,
  );
  if (cutoff)
    return <p role="alert">The identity basis changed. Refresh access to prepare a new finding.</p>;
  if (source.isPending || source.isFetching)
    return <p role="status">Verifying current Staff identity details…</p>;
  if (source.isError)
    return (
      <div role="alert">
        <p>Could not verify the current identity preparation basis.</p>
        <Button variant="outline" onClick={() => void source.refetch()}>
          Retry preparation basis
        </Button>
        <Button variant="ghost" onClick={props.onClose}>
          Close preparation
        </Button>
      </div>
    );
  return (
    <PreparationForm
      key={`${source.data.staff_version}:${source.data.employment_epoch}:${source.data.identity_hash}`}
      {...props}
      source={source.data}
    />
  );
}

function PreparationForm({
  scope,
  source,
  onBusy,
  onAccessLost,
  onSubmitted,
  onClose,
}: PreparationProps & { source: IdentitySource }) {
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.focus();
  }, []);
  const [cursors, setCursors] = useState<(string | null)[]>([null]);
  const basis = {
    staff_version: source.staff_version,
    employment_epoch: source.employment_epoch,
    identity_hash: source.identity_hash,
  };
  const files = usePersonReviewEvidenceOptions(scope, basis, cursors.at(-1) ?? null);
  const form = useZodForm(submissionFields, {
    policy_reference: "",
    finding_reference: "",
    observed_on: "",
    valid_until: "",
    access_intent: "non_digital",
    declared_subject: "",
    identity_examined: false,
    known_associations_disclosed: false,
    document_ids: [],
  });
  const [submitted, setSubmitted] = useState<{
    id: string;
    input: PersonReviewSubmissionInput;
  } | null>(null);
  const original = useRef<typeof submitted>(null);
  useEffect(() => {
    onBusy(form.formState.isDirty || submitted !== null);
  }, [form.formState.isDirty, submitted, onBusy]);
  const { confirm } = useLeaveGuard(
    form.formState.isDirty || submitted !== null,
    "person finding submission",
    submitted !== null,
  );
  const mutation = useApiMutation(
    (command: NonNullable<typeof submitted>) =>
      submitPersonReview(scope, command.id, command.input),
    {
      suppressErrorToast: true,
      onError: (error) => {
        if (error instanceof ApiError && error.status === 400) {
          original.current = null;
          setSubmitted(null);
        }
      },
      onSuccess: () => {
        onBusy(false);
        onSubmitted();
      },
    },
  );
  const cutoff = useStaffAccessCutoff(
    [files.error, mutation.error],
    onAccessLost,
    personReviewContextChanged,
  );
  const locked = submitted !== null;
  const selected = form.watch("document_ids");
  if (cutoff)
    return (
      <p role="alert">
        The Staff identity or evidence basis changed. Refresh access before continuing.
      </p>
    );
  return (
    <section aria-label="Prepare person review" className="space-y-4 border-t pt-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h3 tabIndex={-1} ref={heading} className="font-semibold">
          {source.display_name} · {source.employee_code}
        </h3>
        <Button
          variant="ghost"
          disabled={locked}
          onClick={() =>
            confirm(() => {
              onBusy(false);
              onClose();
            })
          }
        >
          Close preparation
        </Button>
      </div>
      <dl className="grid gap-3 text-sm sm:grid-cols-3">
        <div>
          <dt className="text-muted-foreground">Recorded name</dt>
          <dd>
            {[source.given_name, source.family_name].filter(Boolean).join(" ") ||
              source.display_name}
          </dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Date of birth</dt>
          <dd>{source.date_of_birth || "Not recorded"}</dd>
        </div>
        <div>
          <dt className="text-muted-foreground">Employment record</dt>
          <dd>{source.employment_status}</dd>
        </div>
      </dl>
      <p className="text-sm text-muted-foreground">
        Check the person against this current record and the hospital policy. This finding does not
        activate employment or link a login account.
      </p>
      <form
        aria-label="Person finding submission"
        noValidate
        className="space-y-4"
        onSubmit={(event) => {
          void form.handleSubmit((values) => {
            if (original.current) return;
            if (!values.identity_examined) {
              form.setError("identity_examined", {
                message: "Confirm examination under the recorded policy.",
              });
              return;
            }
            if (!values.known_associations_disclosed) {
              form.setError("known_associations_disclosed", {
                message: "Disclose known associations before submitting.",
              });
              return;
            }
            if (values.access_intent === "digital" && !values.declared_subject.trim()) {
              form.setError("declared_subject", {
                message: "Enter the existing account ID supplied by access administration.",
              });
              return;
            }
            if (values.observed_on > new Date().toISOString().slice(0, 10)) {
              form.setError("observed_on", {
                message: "The observation date cannot be in the future.",
              });
              return;
            }
            if (Date.parse(values.valid_until) <= Date.now()) {
              form.setError("valid_until", {
                message: "Choose a future validity end under the hospital policy.",
              });
              return;
            }
            const input = PersonReviewSubmissionInputContract.parse({
              ...values,
              ...basis,
              command_id: newPersonReviewId(),
              declared_subject:
                values.access_intent === "non_digital" ? "" : values.declared_subject,
            });
            const command = { id: newPersonReviewId(), input };
            original.current = command;
            setSubmitted(command);
            onBusy(true);
            mutation.mutate(command);
          })(event);
        }}
      >
        <fieldset disabled={locked} className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field
              id="person-policy-reference"
              label="Hospital policy reference"
              error={form.formState.errors.policy_reference && "Enter a hospital policy reference."}
            >
              <Input
                id="person-policy-reference"
                required
                maxLength={300}
                {...form.register("policy_reference")}
              />
            </Field>
            <Field
              id="person-finding-reference"
              label="Observation and finding reference"
              error={
                form.formState.errors.finding_reference &&
                "Enter an observation and finding reference."
              }
            >
              <Input
                id="person-finding-reference"
                required
                maxLength={500}
                {...form.register("finding_reference")}
              />
            </Field>
            <Field
              id="person-observed-on"
              label="Observation date (UTC)"
              error={
                form.formState.errors.observed_on?.type
                  ? "Enter the observation date in UTC."
                  : form.formState.errors.observed_on?.message
              }
            >
              <Input
                id="person-observed-on"
                type="date"
                required
                {...form.register("observed_on")}
              />
            </Field>
            <Field
              id="person-valid-until"
              label="Finding valid until (local time)"
              error={
                form.formState.errors.valid_until?.type
                  ? "Enter a valid finding expiry date and time."
                  : form.formState.errors.valid_until?.message
              }
            >
              <Input
                id="person-valid-until"
                type="datetime-local"
                required
                {...form.register("valid_until", {
                  setValueAs: (value) => {
                    const parsed = new Date(value);
                    return value && Number.isFinite(parsed.getTime())
                      ? parsed.toISOString()
                      : value;
                  },
                })}
              />
            </Field>
          </div>
          <Controller
            control={form.control}
            name="access_intent"
            render={({ field }) => (
              <Field id="person-access-intent" label="Account intent">
                <SelectControl
                  id="person-access-intent"
                  value={field.value}
                  disabled={locked}
                  onValueChange={(value) => {
                    field.onChange(value);
                    form.setValue("declared_subject", "", { shouldDirty: true });
                  }}
                  options={[
                    { value: "non_digital", label: "No digital access requested" },
                    { value: "digital", label: "Existing digital account declared" },
                  ]}
                />
              </Field>
            )}
          />
          {form.watch("access_intent") === "digital" && (
            <Field
              id="person-declared-account"
              label="Existing account ID"
              error={form.formState.errors.declared_subject?.message}
            >
              <Input
                id="person-declared-account"
                required
                maxLength={200}
                {...form.register("declared_subject")}
              />
              <p className="text-xs text-muted-foreground">
                Use the account ID supplied by access administration. This declaration does not
                verify or link it.
              </p>
            </Field>
          )}
          <fieldset className="space-y-2">
            <legend className="font-medium">Released identity evidence</legend>
            <p className="text-sm text-muted-foreground">
              Choose 1–10 files. Staff details, evidence and contributor history are checked again
              on submission.
            </p>
            {files.isPending || files.isFetching ? (
              <p role="status">Verifying released identity files…</p>
            ) : files.isError ? (
              <div role="alert">
                <p>Could not verify the released evidence list.</p>
                <Button type="button" variant="outline" onClick={() => void files.refetch()}>
                  Retry evidence selection
                </Button>
              </div>
            ) : (
              <>
                {files.data.data.length === 0 ? (
                  <p>
                    No released identity evidence is available on this page for the current Staff
                    record. Ask authorized Staff intake staff to attach it.
                  </p>
                ) : (
                  files.data.data.map((file) => (
                    <label key={file.id} className="flex items-center gap-3 rounded-md border p-3">
                      <input type="checkbox" value={file.id} {...form.register("document_ids")} />
                      <span className="min-w-0 break-words">
                        {file.filename}
                        <span className="ml-2 text-xs text-muted-foreground">
                          {Math.ceil(file.size / 1024)} KB · {file.content_type}
                        </span>
                      </span>
                    </label>
                  ))
                )}
                <div className="flex gap-2">
                  {cursors.length > 1 && (
                    <Button
                      type="button"
                      variant="outline"
                      onClick={() => setCursors((value) => value.slice(0, -1))}
                    >
                      Previous evidence
                    </Button>
                  )}
                  {files.data.page.next_cursor && (
                    <Button
                      type="button"
                      variant="outline"
                      onClick={() => setCursors((value) => [...value, files.data.page.next_cursor])}
                    >
                      Next evidence
                    </Button>
                  )}
                </div>
              </>
            )}
            <p className="text-xs text-muted-foreground">
              {selected.length} files selected, including other pages.
            </p>
            {form.formState.errors.document_ids && (
              <p role="alert">Select between one and ten identity evidence files.</p>
            )}
          </fieldset>
          <div>
            <label className="flex items-start gap-2">
              <input type="checkbox" {...form.register("identity_examined")} />
              <span>I examined this person’s identity evidence under the recorded policy.</span>
            </label>
            {form.formState.errors.identity_examined && (
              <p role="alert">{form.formState.errors.identity_examined.message}</p>
            )}
          </div>
          <div>
            <label className="flex items-start gap-2">
              <input type="checkbox" {...form.register("known_associations_disclosed")} />
              <span>I disclosed all known staff and account associations for this person.</span>
            </label>
            {form.formState.errors.known_associations_disclosed && (
              <p role="alert">{form.formState.errors.known_associations_disclosed.message}</p>
            )}
          </div>
        </fieldset>
        {mutation.error && (
          <p role="alert">
            {mutation.error instanceof ApiError && mutation.error.status === 400
              ? "The submission was refused. Review the fields before submitting again."
              : "The submission result is unconfirmed. Retry the same submission before starting another action."}
          </p>
        )}
        {submitted ? (
          <Button
            type="button"
            disabled={mutation.isPending}
            onClick={() => mutation.mutate(submitted)}
          >
            {mutation.isPending ? "Recording submission…" : "Retry same submission"}
          </Button>
        ) : (
          <div className="flex justify-end">
            <Button type="submit" disabled={files.isFetching || files.isError}>
              Submit for independent review
            </Button>
          </div>
        )}
      </form>
    </section>
  );
}
