"use client";

import { useEffect, useRef, useState } from "react";
import { Button, Input } from "@tesserix/web";
import {
  ApiError,
  newSupplyId,
  onTenantTransition,
  tenantTransitionPending,
  useTenantTransition,
  usePermissions,
  useApiMutation,
  useSupplyReview,
  useSupplyReviewHistory,
  useSupplyEvidence,
  supplyReviewFormSchema,
  supplyReviewKey,
  recordSupplyReview,
  type SupplyEvidenceScope,
  type SupplyReviewInput,
  type SupplyReviewState,
} from "@helivanta/api";
import { Field, FormSelectField, LoadMore, formatDateTime, useZodForm } from "@helivanta/ui";
import { RequestFailure } from "../request-failure";

type Action = SupplyReviewInput["action"];
const statusLabels: Record<SupplyReviewState["effective_status"], string> = {
  not_submitted: "Not submitted",
  submitted: "Awaiting independent review",
  verified: "Verified",
  rejected: "Rejected",
  suspended: "Suspended",
  review_required: "Source changed · review required",
  evidence_unavailable: "Evidence unavailable · review required",
  premise_inactive: "Premise inactive",
  not_yet_valid: "Not yet valid",
  expired: "Expired",
};
const actionLabels: Record<Action, string> = {
  submit: "Submit for review",
  verify: "Verify licence",
  reject: "Reject licence",
  suspend: "Suspend verification",
};
const successLabels: Record<Action, string> = {
  submit: "Licence submitted for review",
  verify: "Licence verified",
  reject: "Licence rejected",
  suspend: "Verification suspended",
};
type Props = {
  scope: SupplyEvidenceScope;
  licence: { id: string; version: number; number: string };
  roles: string[];
  onClose: () => void;
};

export function LicenceReview({ scope, licence, roles, onClose }: Props) {
  const { can } = usePermissions();
  const switching = useTenantTransition();
  const read =
    can("supply.license.read") &&
    roles.some((r) =>
      ["administrator", "purchasing", "finance", "compliance", "auditor"].includes(r),
    );
  const state = useSupplyReview(scope, read && !switching);
  const history = useSupplyReviewHistory(scope, read && !switching);
  const heading = useRef<HTMLHeadingElement>(null);
  const [action, setAction] = useState<Action | null>(null);
  useEffect(() => {
    heading.current?.scrollIntoView({ block: "start", behavior: "instant" });
    heading.current?.focus({ preventScroll: true });
  }, []);
  const actions = (state.data?.allowed_actions ?? []).filter((a) =>
    a === "submit"
      ? can("supply.license.submit") &&
        roles.some((r) => ["administrator", "purchasing", "compliance"].includes(r))
      : can("supply.license.review") && roles.includes("compliance"),
  );
  return (
    <section
      aria-label="Licence review"
      className="scroll-mt-32 rounded-2xl border bg-card p-4 sm:p-6"
    >
      <header className="flex items-start justify-between gap-4">
        <div>
          <h5 ref={heading} tabIndex={-1} className="text-lg font-semibold outline-none">
            Licence review
          </h5>
          <p className="mt-1 text-sm text-muted-foreground">
            {licence.number} · Current revision {licence.version}
          </p>
        </div>
        <Button variant="ghost" onClick={onClose}>
          Close review
        </Button>
      </header>
      <p className="mt-3 text-sm leading-6 text-muted-foreground">
        An independent reviewer checks the issuing authority and the exact supporting evidence.
        Verification does not approve trading or replace country and product eligibility controls.
      </p>
      {switching ? (
        <p role="status" className="mt-4">
          Switching workspace. Review access has been cleared.
        </p>
      ) : !read ? (
        <p role="alert">You do not have access to this organisation’s licence reviews.</p>
      ) : state.isError ? (
        state.error instanceof ApiError && state.error.code === "session_changed" ? (
          <div role="alert" className="my-4 space-y-3 rounded-xl border p-4">
            <p>{state.error.message}</p>
            <Button onClick={() => window.location.reload()}>Refresh workspace</Button>
          </div>
        ) : (
          <RequestFailure
            message="Could not load the current licence review."
            retry={() => void state.refetch()}
          />
        )
      ) : state.isPending ? (
        <p role="status" className="mt-4">
          Loading licence review…
        </p>
      ) : (
        state.data && (
          <>
            <div className="my-5 rounded-xl border bg-muted/20 p-4">
              <p className="font-semibold">{statusLabels[state.data.effective_status]}</p>
              <p className="mt-1 text-xs text-muted-foreground">
                Current organisation revision {state.data.party_version} · Premise revision{" "}
                {state.data.premise_version} · Licence revision {state.data.license_version}
              </p>
            </div>
            {state.data.latest?.status === "submitted" &&
              !actions.includes("verify") &&
              !actions.includes("reject") && (
                <p className="my-4 text-sm text-muted-foreground">
                  Another authorised compliance reviewer must make the decision. Makers and evidence
                  uploaders cannot review their own submission.
                </p>
              )}
            {actions.length > 0 && (
              <div className="my-4 flex flex-wrap gap-2" aria-label="Review actions">
                {actions.map((a) => (
                  <Button
                    key={a}
                    variant={action === a ? "default" : "outline"}
                    onClick={() => setAction(a)}
                    aria-pressed={action === a}
                  >
                    {a === "submit"
                      ? "Prepare submission"
                      : a === "verify"
                        ? "Record verification"
                        : a === "reject"
                          ? "Record rejection"
                          : "Prepare suspension"}
                  </Button>
                ))}
              </div>
            )}
            {action && actions.includes(action) && (
              <ReviewForm
                key={`${action}:${state.data.latest?.id ?? "new"}:${state.data.party_version}:${state.data.premise_version}:${state.data.license_version}`}
                scope={scope}
                state={state.data}
                action={action}
                onSaved={() => setAction(null)}
              />
            )}
            <h6 className="mt-6 font-semibold">Decision history</h6>
            {history.isError ? (
              <RequestFailure
                message="Could not load private review history."
                retry={() => void history.refetch()}
              />
            ) : history.isPending ? (
              <p role="status">Loading review history…</p>
            ) : (
              <>
                {!history.data?.pages.some((p) => p.data.length) && (
                  <p className="mt-3 text-sm text-muted-foreground">
                    No review decisions recorded.
                  </p>
                )}
                <ol className="mt-3 divide-y">
                  {history.data?.pages
                    .flatMap((p) => p.data)
                    .map((row) => (
                      <li key={row.id} className="space-y-1 py-4">
                        <p className="font-medium">
                          {statusLabels[row.status]} · Decision {row.version}
                        </p>
                        <p className="text-xs text-muted-foreground">
                          {formatDateTime(row.created_at)} · {row.actor_subject} · Licence revision{" "}
                          {row.license_version}
                        </p>
                        <p className="break-words text-sm">{row.reason}</p>
                        {row.reference && (
                          <p className="break-words text-sm text-muted-foreground">
                            {row.method.replaceAll("_", " ")} · {row.reference}
                          </p>
                        )}
                      </li>
                    ))}
                </ol>
                <LoadMore
                  hasMore={history.hasNextPage}
                  isLoading={history.isFetchingNextPage}
                  onClick={() => {
                    if (!history.isFetchingNextPage)
                      void history.fetchNextPage({ cancelRefetch: false });
                  }}
                />
              </>
            )}
          </>
        )
      )}
    </section>
  );
}

function ReviewForm({
  scope,
  state,
  action,
  onSaved,
}: {
  scope: SupplyEvidenceScope;
  state: SupplyReviewState;
  action: Action;
  onSaved: () => void;
}) {
  const { can } = usePermissions();
  const files = useSupplyEvidence(scope, can("supply.evidence.read") && !tenantTransitionPending());
  const source = action === "submit" ? state : (state.latest ?? state);
  const form = useZodForm(supplyReviewFormSchema, {
    expected_version: state.latest?.version ?? 0,
    expected_party_version: source.party_version,
    expected_premise_version: source.premise_version,
    expected_license_version: source.license_version,
    evidence_id: action === "submit" ? "" : (state.latest?.evidence_id ?? ""),
    action,
    reason: "",
    method: "not_applicable",
    reference: "",
  });
  const command = useRef<{ id: string; input: SupplyReviewInput } | null>(null);
  const active = useRef<AbortController | null>(null);
  const busy = useRef(false);
  useEffect(() => {
    const unsubscribe = onTenantTransition(() => {
      if (tenantTransitionPending()) active.current?.abort();
    });
    return () => {
      unsubscribe();
      active.current?.abort();
    };
  }, []);
  const mutation = useApiMutation(
    async (input: SupplyReviewInput) => {
      if (tenantTransitionPending())
        throw new Error("Wait for your workspace to finish switching.");
      if (!command.current || JSON.stringify(command.current.input) !== JSON.stringify(input))
        command.current = { id: newSupplyId(), input };
      const controller = new AbortController();
      active.current = controller;
      const saved = await recordSupplyReview(
        scope,
        command.current.id,
        command.current.input,
        controller.signal,
      );
      controller.signal.throwIfAborted();
      return saved;
    },
    {
      invalidate: [supplyReviewKey(scope)],
      suppressErrorToast: true,
      successToast: successLabels[action],
      onSuccess: onSaved,
    },
  );
  const decision = action === "verify" || action === "reject";
  return (
    <form
      noValidate
      aria-label={actionLabels[action]}
      className="space-y-4 rounded-xl border bg-background p-4 sm:p-5"
      onSubmit={(event) =>
        void form.handleSubmit((input) => {
          if (busy.current) return;
          busy.current = true;
          mutation.mutate(input, {
            onSettled: () => {
              busy.current = false;
            },
          });
        })(event)
      }
    >
      <h6 className="font-semibold">{actionLabels[action]}</h6>
      <fieldset disabled={mutation.isPending} className="space-y-4">
        {action === "submit" ? (
          <>
            {files.isError ? (
              <RequestFailure
                message="Could not load released evidence images."
                retry={() => void files.refetch()}
              />
            ) : (
              <FormSelectField
                control={form.control}
                name="evidence_id"
                id="review-evidence"
                label="Supporting evidence"
                disabled={files.isPending}
                options={
                  files.data?.pages
                    .flatMap((p) => p.data)
                    .filter(
                      (f) =>
                        f.status === "available" && f.license_version === state.license_version,
                    )
                    .map((f) => ({ value: f.id, label: f.filename })) ?? []
                }
              />
            )}
            <p className="text-xs text-muted-foreground">
              Choose a released image for the current licence revision. Open Evidence to inspect the
              document before submitting.
            </p>
            <LoadMore
              hasMore={files.hasNextPage}
              isLoading={files.isFetchingNextPage}
              onClick={() => {
                if (!files.isFetchingNextPage) void files.fetchNextPage({ cancelRefetch: false });
              }}
            />
          </>
        ) : (
          <p className="break-all text-xs text-muted-foreground">
            Pinned evidence: {state.latest?.evidence_id} · Organisation {source.party_version} ·
            Premise {source.premise_version} · Licence {source.license_version}
          </p>
        )}
        {decision && (
          <>
            <FormSelectField
              control={form.control}
              name="method"
              id="review-method"
              label="Independent verification method"
              options={[
                { value: "authority_portal", label: "Issuing authority portal" },
                { value: "authority_contact", label: "Direct authority contact" },
                { value: "physical_inspection", label: "Original document inspection" },
              ]}
            />
            <Field
              id="review-reference"
              label="Authority-check reference"
              error={form.formState.errors.reference?.message}
            >
              <Input id="review-reference" {...form.register("reference")} />
            </Field>
            <p className="text-xs text-muted-foreground">
              Record the check you actually performed. A clean antivirus result is not licence
              verification.
            </p>
          </>
        )}
        <Field
          id="review-reason"
          label="Review reason"
          error={form.formState.errors.reason?.message}
        >
          <textarea
            id="review-reason"
            {...form.register("reason")}
            rows={3}
            maxLength={2000}
            className="w-full rounded-md border bg-background p-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        </Field>
        <p className="text-xs text-muted-foreground">
          Use business review details only. Exclude patient information, credentials and payment
          account details.
        </p>
      </fieldset>
      {mutation.isError && (
        <div role="alert" className="space-y-3 text-sm text-destructive">
          <p>{mutation.error.message}</p>
          {mutation.error instanceof ApiError && mutation.error.code === "session_changed" && (
            <Button type="button" onClick={() => window.location.reload()}>
              Refresh workspace
            </Button>
          )}
        </div>
      )}
      <Button type="submit" disabled={mutation.isPending}>
        {mutation.isPending ? "Recording…" : actionLabels[action]}
      </Button>
    </form>
  );
}
