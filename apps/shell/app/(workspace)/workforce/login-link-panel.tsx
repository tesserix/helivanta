"use client";
import { useEffect, useRef, useState } from "react";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Field, LoadMore, Panel, formatDateTime, useZodForm } from "@helivanta/ui";
import {
  ApiError,
  LOGIN_LINK_STATUSES,
  loginLinkInputContract,
  loginLinksKey,
  newLoginLinkId,
  proposeLoginLink,
  decideLoginLink,
  useApiMutation,
  useApiQuery,
  useLoginLinks,
  useLoginLinkHistory,
  type Hospital,
  type Staff,
  type LoginLink,
  type LoginLinkInput,
  type LoginLinkDecisionInput,
  type LoginLinkScope,
} from "@helivanta/api";

type Props = { hospital: Hospital; staff: Staff; onClose: () => void };
type Identity = { tenant_id: string; subject: string; data: string[] };
type View =
  | { kind: "propose" }
  | { kind: "history"; link: LoginLink }
  | { kind: "decide"; link: LoginLink; decision: LoginLinkDecisionInput["decision"] };

export function StaffLoginLinkPanel(props: Props) {
  const identity = useApiQuery<Identity>(["iam", "me", "permissions"], "/iam/me/permissions");
  if (identity.isPending) return <p role="status">Checking account-link access…</p>;
  if (identity.isError || !identity.data?.tenant_id || !identity.data.subject)
    return <p role="alert">Could not verify account-link access. Close and reopen this panel.</p>;
  if (
    props.hospital.access_level !== "manager" ||
    !identity.data.data.some(
      (p) => p === "workforce.login_links.manage" || p === "workforce.login_links.review",
    )
  )
    return <p role="alert">Account-link access is not available.</p>;
  return (
    <LinkPanel
      key={`${identity.data.tenant_id}:${identity.data.subject}:${props.hospital.id}:${props.staff.id}`}
      {...props}
      identity={identity.data}
    />
  );
}

function LinkPanel({ hospital, staff, onClose, identity }: Props & { identity: Identity }) {
  const scope: LoginLinkScope = {
    tenantId: identity.tenant_id,
    hospitalId: hospital.id,
    staffId: staff.id,
  };
  const records = useLoginLinks(scope, identity.subject);
  const [view, setView] = useState<View | null>(null);
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.focus();
  }, []);
  const manage = identity.data.includes("workforce.login_links.manage");
  const review = identity.data.includes("workforce.login_links.review");
  const links = records.data?.pages.flatMap((page) => page.data) ?? [];
  const current = links.some((link) => link.status === "pending" || link.status === "active");
  return (
    <Panel aria-labelledby="account-link-heading" className="mt-6">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h3
            id="account-link-heading"
            ref={heading}
            tabIndex={-1}
            className="text-xl font-semibold"
          >
            Account link
          </h3>
          <p className="text-sm text-muted-foreground">
            {staff.display_name} · {staff.employee_code} · {hospital.name}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            disabled={records.isFetching}
            onClick={() => {
              setView(null);
              void records.refetch();
            }}
          >
            Refresh links
          </Button>
          <Button variant="ghost" onClick={onClose}>
            Close account link
          </Button>
        </div>
      </div>
      <p className="my-4 text-sm">
        Link a verified login to this staff record. A second person must review it. Linking does not
        grant application access or clinical authority.
      </p>
      {records.isPending ? (
        <p role="status">Loading account links…</p>
      ) : records.isError ? (
        <div role="alert">
          Could not load account links.{" "}
          <Button onClick={() => void records.refetch()}>Try again</Button>
        </div>
      ) : (
        <>
          {!links.length && <p>No account has been linked.</p>}
          {links.map((link) => (
            <Panel as="article" key={link.id} className="my-4 space-y-2">
              <p className="break-all font-medium">{link.subject}</p>
              <p>
                {LOGIN_LINK_STATUSES[link.status]} · Version {link.version}
              </p>
              <p className="text-sm">
                Proposed by {link.proposed_by_subject}: {link.proposal_reason}
              </p>
              <div className="flex flex-wrap gap-2">
                <Button variant="outline" onClick={() => setView({ kind: "history", link })}>
                  History
                </Button>
                {link.status === "pending" &&
                  review &&
                  identity.subject !== link.proposed_by_subject &&
                  identity.subject !== link.subject && (
                    <>
                      <Button
                        onClick={() => setView({ kind: "decide", link, decision: "approve" })}
                      >
                        Approve
                      </Button>
                      <Button
                        variant="outline"
                        onClick={() => setView({ kind: "decide", link, decision: "reject" })}
                      >
                        Reject
                      </Button>
                    </>
                  )}
                {link.status === "pending" &&
                  manage &&
                  identity.subject === link.proposed_by_subject && (
                    <Button
                      variant="outline"
                      onClick={() => setView({ kind: "decide", link, decision: "withdraw" })}
                    >
                      Withdraw
                    </Button>
                  )}
                {(link.status === "pending" || link.status === "active") && manage && (
                  <Button
                    variant="outline"
                    onClick={() => setView({ kind: "decide", link, decision: "revoke" })}
                  >
                    Revoke
                  </Button>
                )}
              </div>
            </Panel>
          ))}
          <LoadMore
            hasMore={records.hasNextPage}
            isLoading={records.isFetching}
            onClick={() => void records.fetchNextPage({ cancelRefetch: false })}
          />
          {manage && !current && staff.employment_status === "active" && (
            <Button className="mt-4" onClick={() => setView({ kind: "propose" })}>
              Propose account link
            </Button>
          )}
          {staff.employment_status !== "active" && (
            <p className="mt-4">Inactive staff cannot receive a new account link.</p>
          )}
          {view?.kind === "propose" && <ProposalForm scope={scope} onClose={() => setView(null)} />}
          {view?.kind === "decide" && (
            <DecisionForm
              key={`${view.link.id}:${view.link.version}:${view.decision}`}
              scope={scope}
              link={view.link}
              decision={view.decision}
              onClose={() => setView(null)}
            />
          )}
          {view?.kind === "history" && (
            <History
              key={view.link.id}
              scope={scope}
              link={view.link}
              subject={identity.subject}
              onClose={() => setView(null)}
            />
          )}
        </>
      )}
    </Panel>
  );
}

function failure(error: Error) {
  if (error instanceof ApiError && error.status === 409)
    return "This link changed or another binding exists. Close this form and refresh the links before trying again.";
  if (error instanceof ApiError && [400, 401, 403, 404].includes(error.status))
    return error.message;
  return "The request may have been saved. Retry the same request or check the link history before starting another.";
}

function ProposalForm({ scope, onClose }: { scope: LoginLinkScope; onClose: () => void }) {
  const [id] = useState(() => newLoginLinkId());
  const [submitted, setSubmitted] = useState<LoginLinkInput | null>(null);
  const form = useZodForm(loginLinkInputContract, { subject: "", reason: "" });
  const save = useApiMutation((input: LoginLinkInput) => proposeLoginLink(scope, id, input), {
    invalidate: [loginLinksKey(scope)],
    onSuccess: onClose,
    suppressErrorToast: true,
  });
  return (
    <Panel className="mt-5">
      <form
        aria-label="Propose account link"
        noValidate
        className="space-y-4"
        onSubmit={form.handleSubmit((value) => {
          const command = submitted ?? value;
          setSubmitted(command);
          save.mutate(command);
        })}
      >
        <Field id="link-subject" label="Account ID" error={form.formState.errors.subject?.message}>
          <Input
            id="link-subject"
            autoComplete="off"
            disabled={!!submitted}
            {...form.register("subject")}
          />
        </Field>
        <Field id="link-reason" label="Reason" error={form.formState.errors.reason?.message}>
          <Input id="link-reason" disabled={!!submitted} {...form.register("reason")} />
        </Field>
        {save.isError && <p role="alert">{failure(save.error)}</p>}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={save.isPending}>
            {save.isPending ? "Saving…" : save.isError ? "Retry same request" : "Submit proposal"}
          </Button>
          <Button type="button" variant="ghost" onClick={onClose} disabled={save.isPending}>
            Close form
          </Button>
        </div>
      </form>
    </Panel>
  );
}

const reasonSchema = z.object({ reason: loginLinkInputContract.shape.reason });
function DecisionForm({
  scope,
  link,
  decision,
  onClose,
}: {
  scope: LoginLinkScope;
  link: LoginLink;
  decision: LoginLinkDecisionInput["decision"];
  onClose: () => void;
}) {
  const [submitted, setSubmitted] = useState<LoginLinkDecisionInput | null>(null);
  const form = useZodForm(reasonSchema, { reason: "" });
  const save = useApiMutation(
    (input: LoginLinkDecisionInput) => decideLoginLink(scope, link.id, input),
    { invalidate: [loginLinksKey(scope)], onSuccess: onClose, suppressErrorToast: true },
  );
  return (
    <Panel className="mt-5">
      <form
        aria-label="Account link decision"
        noValidate
        className="space-y-4"
        onSubmit={form.handleSubmit((value) => {
          const command = submitted ?? { ...value, expected_version: link.version, decision };
          setSubmitted(command);
          save.mutate(command);
        })}
      >
        <p>
          {decision} account {link.subject} · Version {link.version}
        </p>
        <Field id="decision-reason" label="Reason" error={form.formState.errors.reason?.message}>
          <Input id="decision-reason" disabled={!!submitted} {...form.register("reason")} />
        </Field>
        {save.isError && <p role="alert">{failure(save.error)}</p>}
        <div className="flex flex-wrap gap-2">
          <Button type="submit" disabled={save.isPending}>
            {save.isPending
              ? "Saving…"
              : save.isError
                ? "Retry same request"
                : `Confirm ${decision}`}
          </Button>
          <Button type="button" variant="ghost" onClick={onClose} disabled={save.isPending}>
            Close form
          </Button>
        </div>
      </form>
    </Panel>
  );
}

function History({
  scope,
  link,
  subject,
  onClose,
}: {
  scope: LoginLinkScope;
  link: LoginLink;
  subject: string;
  onClose: () => void;
}) {
  const history = useLoginLinkHistory(scope, link.id, subject);
  return (
    <Panel aria-label="Account link history" className="mt-5">
      <h4 className="font-semibold">History for {link.subject}</h4>
      {history.isPending ? (
        <p role="status">Loading history…</p>
      ) : history.isError ? (
        <p role="alert">
          Could not load history.{" "}
          <Button onClick={() => void history.refetch()}>Retry history</Button>
        </p>
      ) : (
        <>
          <ol className="divide-y">
            {history.data?.pages
              .flatMap((page) => page.data)
              .map((entry) => (
                <li key={entry.version} className="py-3">
                  <p>
                    {LOGIN_LINK_STATUSES[entry.status]} · Version {entry.version} ·{" "}
                    {formatDateTime(entry.created_at)}
                  </p>
                  <p>
                    {entry.actor_subject}: {entry.reason}
                  </p>
                </li>
              ))}
          </ol>
          <LoadMore
            hasMore={history.hasNextPage}
            isLoading={history.isFetching}
            onClick={() => void history.fetchNextPage({ cancelRefetch: false })}
          />
        </>
      )}
      <Button variant="ghost" onClick={onClose}>
        Close history
      </Button>
    </Panel>
  );
}
