"use client";

import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { useRouter } from "next/navigation";
import { z } from "zod";
import {
  newStaffInvitationDeliveryID,
  staffSetup,
  type StaffRecipientInput,
  type StaffSetupAction,
  type StaffSetupReceipt,
} from "@helivanta/api";
import { Button, Field, RequestFailure } from "@helivanta/ui";

type Link = { id: string; ticket: string; phase: "email" | "password"; code: string };

export function StaffAcceptance() {
  const router = useRouter();
  const [store] = useState(invitationStore);
  const link = useSyncExternalStore(store.subscribe, store.snapshot, () => undefined);
  const [receipt, setReceipt] = useState<StaffSetupReceipt>();
  const [delivery, setDelivery] = useState<StaffSetupReceipt>();
  const [pending, setPending] = useState<StaffSetupAction | null>("status");
  const [problem, setProblem] = useState<{ error: unknown; action: StaffSetupAction }>();
  const [fieldError, setFieldError] = useState<string>();
  const busy = useRef(false);
  const deliveryID = useRef<string | undefined>(undefined);

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

  useEffect(() => {
    if (receipt?.state !== "complete") return;
    const timer = window.setTimeout(() => router.replace("/login"), 7000);
    return () => window.clearTimeout(timer);
  }, [receipt?.state, router]);

  async function run(action: StaffSetupAction, password?: string) {
    if (!link || busy.current || pending) return;
    busy.current = true;
    setPending(action);
    setProblem(undefined);
    const input: StaffRecipientInput = { invitation_id: link.id, ticket: link.ticket };
    if (action === "email" || action === "password") input.code = link.code;
    if (action === "password") input.password = password;
    if (action === "resend") {
      deliveryID.current ??= newStaffInvitationDeliveryID();
      input.delivery_id = deliveryID.current;
    }
    try {
      const value = await staffSetup(action, input);
      if (action === "resend" && value.state !== "complete") setDelivery(value);
      else setReceipt(value);
    } catch (error: unknown) {
      setProblem({ error, action });
    } finally {
      busy.current = false;
      setPending(null);
    }
  }

  function freshDelivery() {
    if (busy.current || pending) return;
    deliveryID.current = undefined;
    void run("resend");
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
          Go to login
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
        retry={() => {
          void run(problem.action === "resend" ? "resend" : "status");
        }}
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
        <p className="text-sm text-muted-foreground">Redirecting to login in 7 seconds.</p>
        <a className="font-medium underline" href="/login">
          Go to login
        </a>
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
          Go to login
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
        <Button
          type="button"
          disabled={!!pending}
          onClick={() => {
            void run("email");
          }}
        >
          Verify email
        </Button>
      )}
      {password && (
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            const form = event.currentTarget;
            const values = new FormData(form);
            const value = values.get("password");
            if (typeof value !== "string" || value !== values.get("confirmation")) {
              setFieldError("Passwords must match.");
              return;
            }
            setFieldError(undefined);
            form.reset();
            void run("password", value);
          }}
        >
          <Field id="staff-password" label="New password">
            <input
              id="staff-password"
              name="password"
              className="h-11 w-full rounded-md border bg-background px-3"
              type="password"
              autoComplete="new-password"
              required
              disabled={!!pending}
            />
          </Field>
          <Field id="staff-confirmation" label="Confirm password" error={fieldError}>
            <input
              id="staff-confirmation"
              name="confirmation"
              className="h-11 w-full rounded-md border bg-background px-3"
              type="password"
              autoComplete="new-password"
              required
              disabled={!!pending}
            />
          </Field>
          <Button type="submit" disabled={!!pending}>
            Set password
          </Button>
        </form>
      )}
      {uncertain && (
        <Button
          type="button"
          disabled={!!pending}
          onClick={() => {
            void run("status");
          }}
        >
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
  let current: Link | null | undefined;
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
