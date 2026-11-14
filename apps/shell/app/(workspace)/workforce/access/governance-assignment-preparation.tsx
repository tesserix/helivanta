"use client";

import { useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  Field,
  FormSelectField,
  RequestFailure,
  SelectControl,
  useLeaveGuard,
  useZodForm,
} from "@helivanta/ui";
import {
  ApiError,
  assignmentDepartmentChoicesKey,
  assignmentTargetChoicesKey,
  governanceAssignmentsKey,
  isGovernanceAssignmentAccessChanged,
  listAssignmentDepartmentChoices,
  listAssignmentTargetChoices,
  newStaffAssignmentId,
  proposeGovernanceAssignment,
  staffAccessTemplates,
  staffAssignmentInputContract,
  useApiMutation,
  type AssignmentTargetChoice,
  type GovernanceAssignmentScope,
  type StaffAssignmentFamily,
  type StaffAssignmentInput,
} from "@helivanta/api";
import { STAFF_ACCESS_CHANGED, useStaffAccessCutoff } from "../staff-access-cutoff";

type Props = {
  scope: GovernanceAssignmentScope;
  family: StaffAssignmentFamily;
  templateKeys: readonly string[];
  onClose: () => void;
  onSaved: (id: string) => void;
  onAccessLost: () => void;
};

export function GovernanceAssignmentPreparation(props: Props) {
  const [target, setTarget] = useState<AssignmentTargetChoice | null>(null);
  const choices = useInfiniteQuery({
    queryKey: assignmentTargetChoicesKey(props.scope),
    queryFn: ({ pageParam, signal }) => listAssignmentTargetChoices(props.scope, pageParam, signal),
    initialPageParam: null as string | null,
    getNextPageParam: (page) => page.page.next_cursor ?? undefined,
    staleTime: 0,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
    enabled: target === null,
  });
  const cutoff = useStaffAccessCutoff(
    [choices.error],
    props.onAccessLost,
    isGovernanceAssignmentAccessChanged,
  );
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  if (target) return <ProposalForm key={target.staff_id} {...props} target={target} />;
  return (
    <section aria-label="Choose staff for assignment" className="space-y-4">
      <h3 className="font-semibold">Choose a colleague</h3>
      <p>Only current digital Staff with an independently reviewed account link can be selected.</p>
      {choices.isPending ? (
        <p role="status">Checking eligible Staff accounts…</p>
      ) : choices.isError ? (
        <RequestFailure
          message="Could not verify eligible Staff accounts."
          error={choices.error}
          retry={choices.refetch}
        />
      ) : (
        <>
          {choices.data.pages.every((page) => page.data.length === 0) && (
            <p>No eligible colleagues on this page.</p>
          )}
          {choices.data.pages
            .flatMap((page) => page.data)
            .map((row) => (
              <section key={row.staff_id} className="space-y-2">
                <p>
                  {row.name} · {row.email}
                </p>
                <Button variant="outline" onClick={() => setTarget(row)}>
                  Choose {row.name}
                </Button>
              </section>
            ))}
          {choices.hasNextPage && (
            <Button
              disabled={choices.isFetchingNextPage}
              onClick={() => void choices.fetchNextPage()}
            >
              More colleagues
            </Button>
          )}
        </>
      )}
      <Button variant="ghost" onClick={props.onClose}>
        Close preparation
      </Button>
    </section>
  );
}

const localDate = (value: Date) =>
  new Date(value.getTime() - value.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
const dateInput = z
  .string()
  .min(1, "Choose a date and time.")
  .refine((value) => Number.isFinite(Date.parse(value)), "Choose a valid date and time.")
  .transform((value) => new Date(value).toISOString());

function ProposalForm({
  scope,
  target,
  family,
  templateKeys,
  onClose,
  onSaved,
  onAccessLost,
}: Props & { target: AssignmentTargetChoice }) {
  const templates = staffAccessTemplates.filter(
    (row) => row.family === family && templateKeys.includes(row.key),
  );
  const [kind, setKind] = useState<"hospital" | "department">(() =>
    templates.some((row) => (row.scope_kinds as readonly string[]).includes("hospital"))
      ? "hospital"
      : "department",
  );
  const [cursor, setCursor] = useState<string | null>(null);
  const [id] = useState(() => newStaffAssignmentId());
  const [submitted, setSubmitted] = useState<StaffAssignmentInput | null>(null);
  const offered = templates.filter((row) => (row.scope_kinds as readonly string[]).includes(kind));
  const departments = useQuery({
    queryKey: [...assignmentDepartmentChoicesKey(scope), cursor],
    queryFn: ({ signal }) => listAssignmentDepartmentChoices(scope, cursor, signal),
    staleTime: 0,
    gcTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
    enabled: kind === "department" && submitted === null,
  });
  const [defaults] = useState(() => ({
    hospital_id: scope.hospitalId,
    subject: target.subject,
    template_key: offered[0]?.key,
    reason: "",
    valid_from: localDate(new Date()),
    expires_at: localDate(new Date(Date.now() + 86400000)),
  }));
  const form = useZodForm(
    staffAssignmentInputContract
      .safeExtend({
        reason: staffAssignmentInputContract.shape.reason.trim().min(1, "Enter a reason."),
        valid_from: dateInput,
        expires_at: dateInput,
      })
      .superRefine((value, context) => {
        if (!offered.some((row) => row.key === value.template_key))
          context.addIssue({
            code: "custom",
            path: ["template_key"],
            message: "Choose a current template for this scope.",
          });
        if (
          kind === "department" &&
          !departments.data?.data.some((row) => row.id === value.department_id)
        )
          context.addIssue({
            code: "custom",
            path: ["department_id"],
            message: "Choose a current department.",
          });
      }),
    defaults,
  );
  const save = useApiMutation(
    (input: StaffAssignmentInput) => proposeGovernanceAssignment(scope, id, input),
    {
      invalidate: [governanceAssignmentsKey(scope)],
      suppressErrorToast: true,
      onSuccess: (receipt) => onSaved(receipt.id),
    },
  );
  const refused = save.error instanceof ApiError && [400, 409].includes(save.error.status);
  const uncertain = submitted !== null && !save.isSuccess && !refused;
  const { confirm } = useLeaveGuard(
    form.formState.isDirty || !!submitted,
    "assignment preparation",
    uncertain,
  );
  const cutoff = useStaffAccessCutoff(
    [departments.error, save.error],
    onAccessLost,
    isGovernanceAssignmentAccessChanged,
  );
  if (cutoff) return <p role="alert">{STAFF_ACCESS_CHANGED}</p>;
  return (
    <form
      aria-label="Prepare hospital assignment"
      noValidate
      className="space-y-4"
      onSubmit={form.handleSubmit((input) => {
        const command = submitted ?? input;
        setSubmitted(command);
        save.mutate(command);
      })}
    >
      <h3 className="font-semibold">Prepare access for {target.name}</h3>
      <p>{target.email}</p>
      <p>
        This proposal requires current source checks and independent review. It grants no operating
        access by itself.
      </p>
      <Field id="native-assignment-scope" label="Access scope">
        <SelectControl
          id="native-assignment-scope"
          value={kind}
          disabled={!!submitted}
          onValueChange={(value) => {
            const next = value as "hospital" | "department";
            setKind(next);
            setCursor(null);
            form.setValue("department_id", undefined);
            const first = templates.find((row) =>
              (row.scope_kinds as readonly string[]).includes(next),
            );
            if (first) form.setValue("template_key", first.key);
          }}
          options={(["hospital", "department"] as const)
            .filter((value) =>
              templates.some((row) => (row.scope_kinds as readonly string[]).includes(value)),
            )
            .map((value) => ({
              value,
              label: value === "hospital" ? "Hospital" : "Department and current sub-units",
            }))}
        />
      </Field>
      <FormSelectField
        control={form.control}
        name="template_key"
        id="native-assignment-template"
        label="Access template"
        disabled={!!submitted}
        options={offered.map((row) => ({ value: row.key, label: row.name }))}
      />
      {kind === "department" &&
        (departments.isPending ? (
          <p role="status">Checking current departments…</p>
        ) : departments.isError ? (
          <RequestFailure
            message="Could not verify departments."
            error={departments.error}
            retry={departments.refetch}
          />
        ) : (
          <>
            <FormSelectField
              control={form.control}
              name="department_id"
              id="native-assignment-department"
              label="Department"
              disabled={!!submitted}
              options={[
                { value: "", label: "Choose a department" },
                ...departments.data.data.map((row) => ({ value: row.id, label: row.name })),
              ]}
            />
            {departments.data.page.has_more && (
              <Button
                type="button"
                variant="outline"
                disabled={!!submitted}
                onClick={() => {
                  form.setValue("department_id", undefined);
                  setCursor(departments.data.page.next_cursor);
                }}
              >
                More departments
              </Button>
            )}
            {cursor && (
              <Button
                type="button"
                variant="outline"
                disabled={!!submitted}
                onClick={() => {
                  form.setValue("department_id", undefined);
                  setCursor(null);
                }}
              >
                First departments
              </Button>
            )}
          </>
        ))}
      <Field
        id="native-assignment-reason"
        label="Assignment reason"
        error={form.formState.errors.reason?.message}
      >
        <Input id="native-assignment-reason" disabled={!!submitted} {...form.register("reason")} />
      </Field>
      <Field
        id="native-assignment-start"
        label="Access starts"
        error={form.formState.errors.valid_from?.message}
      >
        <Input
          id="native-assignment-start"
          type="datetime-local"
          disabled={!!submitted}
          {...form.register("valid_from")}
        />
      </Field>
      <Field
        id="native-assignment-end"
        label="Access expires"
        error={form.formState.errors.expires_at?.message}
      >
        <Input
          id="native-assignment-end"
          type="datetime-local"
          disabled={!!submitted}
          {...form.register("expires_at")}
        />
      </Field>
      {save.isError && (
        <p role="alert">
          {refused
            ? "The proposal or source changed. Close preparation and choose current facts again."
            : "The proposal may have been recorded. Retry the same request before starting another."}
        </p>
      )}
      <Button
        type={submitted ? "button" : "submit"}
        onClick={() => {
          if (submitted) save.mutate(submitted);
        }}
        disabled={
          save.isPending ||
          refused ||
          offered.length === 0 ||
          (kind === "department" && !departments.isSuccess)
        }
      >
        {save.isPending ? "Submitting…" : save.isError ? "Retry same request" : "Submit proposal"}
      </Button>
      <Button
        type="button"
        variant="ghost"
        disabled={uncertain}
        onClick={() => {
          confirm(onClose);
        }}
      >
        Close preparation
      </Button>
    </form>
  );
}
