"use client";
import { useEffect, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import {
  Panel,
  Field,
  LoadMore,
  RequestFailure,
  useConfirmLeave,
  useLeaveGuard,
  useZodForm,
  formatDateTime,
} from "@helivanta/ui";
import {
  ApiError,
  PartnerMemberProposalInputContract,
  newPartnerGroupId,
  preparePartnerMember,
  recoverPreparedPartnerMember,
  partnerMembersKey,
  useApiMutation,
  usePartnerMemberCandidates,
  type PartnerGroupScope,
  type PartnerGroup,
  type PartnerMemberCandidate,
  type PartnerMemberProposalInput,
} from "@helivanta/api";
import { accessChanged } from "./errors";
type Props = {
  scope: PartnerGroupScope;
  group: PartnerGroup;
  busy: boolean;
  onChanged: () => void;
  onBusyChanged: (busy: boolean) => void;
  onFinished: (recovered: boolean) => void;
  onCancel: () => void;
};
export function MemberNomination(props: Props) {
  const { onChanged } = props;
  const candidates = usePartnerMemberCandidates(props.scope, props.group.hospital_id);
  const [selected, setSelected] = useState<PartnerMemberCandidate | null>(null);
  const confirm = useConfirmLeave();
  useEffect(() => {
    if (accessChanged(candidates.error)) onChanged();
  }, [candidates.error, onChanged]);
  if (selected) return <NominationForm key={selected.id} {...props} candidate={selected} />;
  if (candidates.isPending) return <p role="status">Loading candidate Staff postings…</p>;
  if (candidates.isError)
    return (
      <RequestFailure
        message="Candidate Staff postings are unavailable."
        error={candidates.error}
        retry={candidates.refetch}
      />
    );
  const rows = candidates.data.pages.flatMap((page) => page.data);
  return (
    <section aria-label="Staff membership candidates" className="space-y-3">
      <p>
        Select a named hospital Staff account and its posting. This candidate list does not
        establish current eligibility; the server rechecks it when the nomination is recorded.
      </p>
      {!rows.length && <p>No candidate Staff postings on this page.</p>}
      {rows.map((candidate) => (
        <Panel as="article" key={candidate.id} className="space-y-2">
          <p>{candidate.display_name}</p>
          <p>
            Posting starts {formatDateTime(candidate.starts_at)}; nominal expiry{" "}
            {formatDateTime(candidate.nominal_until)}
          </p>
          <Button variant="outline" onClick={() => setSelected(candidate)}>
            Nominate {candidate.display_name}
          </Button>
        </Panel>
      ))}
      <LoadMore
        hasMore={candidates.hasNextPage}
        isLoading={candidates.isFetchingNextPage}
        onClick={() => candidates.fetchNextPage()}
        label="Load more candidate Staff"
      />
      <Button variant="outline" onClick={() => confirm(props.onCancel)}>
        Close membership nomination
      </Button>
    </section>
  );
}
const dateInput = z
  .string()
  .min(1, "Choose a date and time.")
  .refine((value) => Number.isFinite(Date.parse(value)), "Choose a valid date and time.");
function localTime(date: Date) {
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 19);
}
function NominationForm(props: Props & { candidate: PartnerMemberCandidate }) {
  const { onChanged, onBusyChanged } = props;
  const [openedAt] = useState(() => Date.now());
  const [id] = useState(() => newPartnerGroupId());
  const [retained, setRetained] = useState<PartnerMemberProposalInput | null>(null);
  const [refused, setRefused] = useState(false);
  const confirm = useConfirmLeave();
  const makeInput = (values: {
    reason: string;
    valid_from: string;
    expires_at: string;
    sources_examined: boolean;
  }): PartnerMemberProposalInput => ({
    kind: "subject",
    subject: props.candidate.subject,
    staff_id: props.candidate.staff_id,
    posting_id: props.candidate.id,
    posting_revision_id: props.candidate.posting_revision_id,
    posting_version: props.candidate.posting_version,
    department_id: props.candidate.department_id,
    department_revision_id: props.candidate.department_revision_id,
    expected_group_version: props.group.version,
    ...values,
    valid_from: new Date(values.valid_from).toISOString(),
    expires_at: new Date(values.expires_at).toISOString(),
  });
  const schema = z
    .object({
      reason: PartnerMemberProposalInputContract.shape.reason.trim(),
      valid_from: dateInput,
      expires_at: dateInput,
      sources_examined: z.boolean(),
    })
    .superRefine((values, ctx) => {
      if (
        !Number.isFinite(Date.parse(values.valid_from)) ||
        !Number.isFinite(Date.parse(values.expires_at))
      )
        return;
      const parsed = PartnerMemberProposalInputContract.safeParse(makeInput(values));
      if (!parsed.success)
        for (const issue of parsed.error.issues)
          ctx.addIssue({ code: "custom", path: issue.path, message: issue.message });
      if (Date.parse(values.valid_from) < Date.parse(props.group.valid_from))
        ctx.addIssue({
          code: "custom",
          path: ["valid_from"],
          message: "Start within the published team's validity.",
        });
      if (
        Date.parse(values.expires_at) >
        Math.min(Date.parse(props.group.expires_at), Date.parse(props.candidate.nominal_until))
      )
        ctx.addIssue({
          code: "custom",
          path: ["expires_at"],
          message: "End within both the team and selected posting's validity.",
        });
    });
  const form = useZodForm(schema, {
    reason: "",
    valid_from: localTime(
      new Date(Math.ceil(Math.max(openedAt, Date.parse(props.group.valid_from)) / 1000) * 1000),
    ),
    expires_at: localTime(
      new Date(
        Math.floor(
          Math.min(Date.parse(props.group.expires_at), Date.parse(props.candidate.nominal_until)) /
            1000,
        ) * 1000,
      ),
    ),
    sources_examined: false,
  });
  const save = useApiMutation(
    (input: PartnerMemberProposalInput) =>
      preparePartnerMember(props.scope, props.group.hospital_id, props.group.id, id, input),
    {
      invalidate: [partnerMembersKey(props.scope, props.group.hospital_id, props.group.id)],
      onSuccess: () => props.onFinished(false),
      onError: (error) => {
        if (accessChanged(error)) onChanged();
        setRefused(error instanceof ApiError && [400, 404, 409].includes(error.status));
      },
      suppressErrorToast: true,
    },
  );
  const recover = useApiMutation(
    () => {
      if (!retained) throw new Error("No membership nomination awaits recovery.");
      return recoverPreparedPartnerMember(
        props.scope,
        props.group.hospital_id,
        props.group.id,
        id,
        retained,
      );
    },
    {
      invalidate: [partnerMembersKey(props.scope, props.group.hospital_id, props.group.id)],
      onSuccess: () => props.onFinished(true),
      onError: (error) => {
        if (accessChanged(error)) onChanged();
      },
      suppressErrorToast: true,
    },
  );
  const pending = save.isPending || recover.isPending;
  const busy = pending || (!!retained && !refused);
  useLeaveGuard(form.formState.isDirty || !!retained, "membership nomination", busy);
  useEffect(() => onBusyChanged(busy), [busy, onBusyChanged]);
  return (
    <Panel>
      <form
        aria-label="Prepare hospital membership nomination"
        noValidate
        className="space-y-4"
        onSubmit={form.handleSubmit((values) => {
          const input = retained ?? makeInput(values);
          setRetained(input);
          setRefused(false);
          save.mutate(input);
        })}
      >
        <h4 className="font-medium">Nominate {props.candidate.display_name}</h4>
        <p>Selected account: {props.candidate.subject}</p>
        <p>
          Team purpose: {props.group.purpose}. Posting starts{" "}
          {formatDateTime(props.candidate.starts_at)}; nominal expiry{" "}
          {formatDateTime(props.candidate.nominal_until)}.
        </p>
        <p>
          Another current Staff reviewer must independently approve this nomination. It grants no
          supplier access.
        </p>
        <Field
          id="membership-nomination-reason"
          label="Nomination reason"
          error={form.formState.errors.reason?.message}
        >
          <Input
            id="membership-nomination-reason"
            {...form.register("reason")}
            disabled={!!retained}
          />
        </Field>
        <Field
          id="membership-valid-from"
          label="Membership starts"
          error={form.formState.errors.valid_from?.message}
        >
          <Input
            id="membership-valid-from"
            type="datetime-local"
            step="1"
            {...form.register("valid_from")}
            disabled={!!retained}
          />
        </Field>
        <Field
          id="membership-expires-at"
          label="Membership expires"
          error={form.formState.errors.expires_at?.message}
        >
          <Input
            id="membership-expires-at"
            type="datetime-local"
            step="1"
            {...form.register("expires_at")}
            disabled={!!retained}
          />
        </Field>
        <label className="flex items-center gap-2">
          <input type="checkbox" {...form.register("sources_examined")} disabled={!!retained} />I
          examined the selected Staff account and posting
        </label>
        {form.formState.errors.sources_examined && (
          <p role="alert">{form.formState.errors.sources_examined.message}</p>
        )}
        {save.isError && (
          <p role="alert">
            {refused
              ? save.error.message
              : "Nomination outcome unconfirmed. Recover its retained record or retry the same nomination."}
          </p>
        )}
        {recover.isError && (
          <p role="alert">
            The retained nomination could not be verified. Keep this request and retry recovery or
            the same nomination.
          </p>
        )}
        <div className="flex flex-wrap gap-3">
          <Button
            type={retained ? "button" : "submit"}
            disabled={pending}
            onClick={
              retained
                ? () => {
                    setRefused(false);
                    save.mutate(retained);
                  }
                : undefined
            }
          >
            {retained ? "Retry same membership nomination" : "Prepare membership nomination"}
          </Button>
          {retained && (
            <Button
              type="button"
              variant="outline"
              disabled={pending}
              onClick={() => recover.mutate(undefined)}
            >
              Recover membership nomination
            </Button>
          )}
          {(!retained || refused) && (
            <Button
              type="button"
              variant="outline"
              disabled={pending}
              onClick={() => confirm(props.onCancel)}
            >
              Close membership nomination
            </Button>
          )}
        </div>
      </form>
    </Panel>
  );
}
