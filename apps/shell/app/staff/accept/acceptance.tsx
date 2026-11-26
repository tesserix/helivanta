"use client";

import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import Link from "next/link";
import { z } from "zod";
import {
  newStaffInvitationDeliveryID,
  staffSetup,
  useApiMutation,
  type StaffRecipientInput,
  type StaffSetupAction,
  type StaffSetupReceipt,
} from "@helivanta/api";
import { Button, Field, RequestFailure, useZodForm } from "@helivanta/ui";

type Invitation = { id: string; ticket: string; phase: "email" | "password"; code: string };

// Presence and the match are checked here; StaffRecipientInput (onboarding
// contracts) is a plain type, and the identity provider owns the password
// policy, so no length or strength rule is invented on the client (#1855).
const passwordSchema = z
  .object({
    password: z.string().min(1, "Enter a new password."),
    confirmation: z.string().min(1, "Confirm your new password."),
  })
  .refine((value) => value.password === value.confirmation, {
    path: ["confirmation"],
    message: "Passwords must match.",
  });

export function StaffAcceptance() {
  const [store] = useState(invitationStore);
  const link = useSyncExternalStore(store.subscribe, store.snapshot, () => undefined);
  const [receipt, setReceipt] = useState<StaffSetupReceipt>();
  const [delivery, setDelivery] = useState<StaffSetupReceipt>();
  const [pending, setPending] = useState<StaffSetupAction | null>("status");
  const [problem, setProblem] = useState<{ error: unknown; action: StaffSetupAction }>();
  const busy = useRef(false);
  const deliveryID = useRef<string | undefined>(undefined);
  const secret = useRef<string | undefined>(undefined);
  const form = useZodForm(passwordSchema, { password: "", confirmation: "" });
  // The action is the only variable, so neither the password nor the link's
  // code lands in the mutation cache. RequestFailure is the error surface.
  const step = useApiMutation(
    (action: StaffSetupAction) => {
      const input: StaffRecipientInput = { invitation_id: link!.id, ticket: link!.ticket };
      if (action === "email" || action === "password") input.code = link!.code;
      if (action === "password") input.password = secret.current;
      secret.current = undefined;
      if (action === "resend") {
        deliveryID.current ??= newStaffInvitationDeliveryID();
        input.delivery_id = deliveryID.current;
      }
      return staffSetup(action, input);
    },
    { suppressErrorToast: true },
  );

  useEffect(() => {
    if (!link) return;
    let active = true;
    const controller = new AbortController();
    void staffSetup("status", { invitation_id: link.id, ticket: link.ticket }, controller.signal)
      .then((value) => {
        if (active) setReceipt(value);
      })
      .catch((error: unknown) => {
        if (active) setProblem({ error, action: "status" });
      })
      .finally(() => {
        if (active) setPending(null);
      });
    return () => {
      active = false;
      controller.abort();
    };
  }, [link]);

  function run(action: StaffSetupAction, password?: string) {
    if (!link || busy.current || pending) return;
    busy.current = true;
    setPending(action);
    setProblem(undefined);
    secret.current = password;
    step.mutate(action, {
      onSuccess: (value) => {
        if (action === "resend" && value.state !== "complete") setDelivery(value);
        else setReceipt(value);
      },
      onError: (error) => setProblem({ error, action }),
      onSettled: () => {
        busy.current = false;
        setPending(null);
      },
    });
  }

  function freshDelivery() {
    if (busy.current || pending) return;
    deliveryID.current = undefined;
    run("resend");
  }

  if (link === undefined) return <p role="status">Opening your invitation…</p>;
  if (!link)
    return (
      <div className="space-y-3">
        <h1 className="text-2xl font-medium">Open your invitation email</h1>
        <p className="text-muted-foreground">
          Use the latest account setup link sent by your organisation. If it is missing, ask your
          administrator for another invitation.
        </p>
        <a href="/login" className="underline">
          Sign in
        </a>
      </div>
    );
  if (problem)
    return (
      <RequestFailure
        message={
          problem.action === "resend"
            ? "We could not confirm delivery. Check your inbox or retry this link request."
            : "We could not confirm this step. Check its status before continuing."
        }
        error={problem.error}
        retry={() => run(problem.action === "resend" ? "resend" : "status")}
      />
    );
  if (!receipt) return <p role="status">Checking your invitation…</p>;
  if (receipt.state === "complete")
    return (
      <div className="space-y-4">
        <h1 className="text-2xl font-medium">Your account is ready</h1>
        <p>
          Your email is verified and your password is set. Your organisation administrator will
          confirm your staff access.
        </p>
        <Button asChild>
          <Link href="/login">Sign in</Link>
        </Button>
      </div>
    );

  if (receipt.state === "superseded")
    return (
      <div className="space-y-4">
        <h1 className="text-2xl font-medium">A newer link replaced this invitation</h1>
        <p>
          Open the latest setup email. Older links cannot verify your email or change your password.
        </p>
        <a className="font-medium underline" href="/login">
          Sign in
        </a>
      </div>
    );

  const sent = !!delivery;
  const verified = receipt.state === "already_verified";
  const expired = receipt.state === "expired" || receipt.state === "consumed";
  const uncertain = receipt.state === "delivery_unresolved" || receipt.state === "proof_unresolved";
  const password =
    !sent &&
    !expired &&
    !uncertain &&
    !verified &&
    link.phase === "password" &&
    receipt.state === "password_action_required";
  const email =
    !sent &&
    !expired &&
    !uncertain &&
    !verified &&
    link.phase === "email" &&
    receipt.state === "email_action_required";

  return (
    <div className="space-y-5">
      <h1 className="text-2xl font-medium">
        {sent
          ? "Check your email"
          : verified
            ? "Your email is verified"
            : expired
              ? "This link has expired or been used"
              : uncertain
                ? "Check your previous request"
                : password
                  ? "Choose your password"
                  : "Verify your email"}
      </h1>
      <p className="text-muted-foreground">
        {sent
          ? delivery.state === "delivery_unresolved"
            ? "Delivery is not confirmed. Check your inbox. If nothing arrives, wait a minute before requesting another link."
            : "Open the latest setup email to continue. The new link replaces older links."
          : verified
            ? "Verification is complete. Request a password setup email to finish your account."
            : uncertain
              ? "A previous request has an uncertain result. Check its status before making another change."
              : expired
                ? "Request a fresh link below and continue from your latest email."
                : password
                  ? "Choose a private password that follows your organisation’s policy."
                  : "Confirm your email to continue setting up your staff account."}
      </p>
      {email && (
        <Button type="button" disabled={!!pending} onClick={() => run("email")}>
          Verify email
        </Button>
      )}
      {password && (
        <form
          noValidate
          className="space-y-4"
          onSubmit={(event) =>
            void form.handleSubmit(({ password: value }) => {
              form.reset();
              run("password", value);
            })(event)
          }
        >
          <Field
            id="staff-password"
            label="New password"
            error={form.formState.errors.password?.message}
          >
            <input
              id="staff-password"
              className="h-11 w-full rounded-md border bg-background px-3"
              type="password"
              autoComplete="new-password"
              disabled={!!pending}
              {...form.register("password")}
            />
          </Field>
          <Field
            id="staff-confirmation"
            label="Confirm password"
            error={form.formState.errors.confirmation?.message}
          >
            <input
              id="staff-confirmation"
              className="h-11 w-full rounded-md border bg-background px-3"
              type="password"
              autoComplete="new-password"
              disabled={!!pending}
              {...form.register("confirmation")}
            />
          </Field>
          <Button type="submit" disabled={!!pending}>
            Set password
          </Button>
        </form>
      )}
      {uncertain && (
        <Button type="button" disabled={!!pending} onClick={() => run("status")}>
          Check status
        </Button>
      )}
      {!email && !password && (
        <Button type="button" disabled={!!pending} onClick={freshDelivery}>
          {verified && !sent
            ? "Send password setup email"
            : sent
              ? "Request another link"
              : "Send a fresh link"}
        </Button>
      )}
      <p className="text-xs leading-relaxed text-muted-foreground">
        Links expire after 30 minutes. Wait 60 seconds between requests for the same step. Up to 5
        setup emails are allowed per rolling 24 hours.
      </p>
    </div>
  );
}

function invitationStore() {
  let current: Invitation | null | undefined;
  let initialized = false;
  return {
    snapshot: () => current,
    subscribe: (listener: () => void) => {
      if (!initialized) {
        initialized = true;
        const fragment = window.location.hash;
        window.history.replaceState(null, "", window.location.pathname + window.location.search);
        const fields = new URLSearchParams(fragment.slice(1));
        const id = fields.get("invitation"),
          ticket = fields.get("ticket"),
          phase = fields.get("phase"),
          code = fields.get("code");
        current =
          fragment.length <= 1024 &&
          fields.size === 4 &&
          ["invitation", "ticket", "phase", "code"].every(
            (key) => fields.getAll(key).length === 1,
          ) &&
          id &&
          z.uuidv7().safeParse(id).success &&
          ticket &&
          /^[A-Za-z0-9_-]{43}$/.test(ticket) &&
          (phase === "email" || phase === "password") &&
          code &&
          new TextEncoder().encode(code).length <= 20 &&
          !/\p{Cc}/u.test(code)
            ? { id: id.toLowerCase(), ticket, phase, code }
            : null;
      }
      listener();
      return () => {};
    },
  };
}
