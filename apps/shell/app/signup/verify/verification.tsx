"use client";

import { useState, useSyncExternalStore } from "react";
import { z } from "zod";
import {
  sendCustomerLink,
  setCustomerPassword,
  verifyCustomerEmail,
  useApiMutation,
} from "@helivanta/api";
import { Button, Field, useZodForm } from "@helivanta/ui";

type Link = { phase: "email" | "password"; userId: string; code: string };
const passwordForm = z
  .object({
    password: z.string().min(1, "Enter your password"),
    confirmation: z.string().min(1, "Confirm your password"),
  })
  .refine((value) => value.password === value.confirmation, {
    path: ["confirmation"],
    message: "Passwords must match",
  });

export function SignupVerification() {
  const [linkStore] = useState(signupLinkStore);
  const link = useSyncExternalStore(linkStore.subscribe, linkStore.snapshot, () => undefined);
  const [completed, setCompleted] = useState<"email" | "password" | null>(null);
  const [problem, setProblem] = useState(false);
  const form = useZodForm(passwordForm, { password: "", confirmation: "" });
  const verify = useApiMutation(verifyCustomerEmail, { gcTime: 0, suppressErrorToast: true });
  const password = useApiMutation(setCustomerPassword, { gcTime: 0, suppressErrorToast: true });
  const delivery = useApiMutation(() => sendCustomerLink("password", { userId: link?.userId }), {
    gcTime: 0,
    suppressErrorToast: true,
  });
  async function prove(secret?: string) {
    if (!link || !link.code) return;
    setProblem(false);
    try {
      if (link.phase === "email")
        await verify.mutateAsync({ userId: link.userId, code: link.code });
      else await password.mutateAsync({ userId: link.userId, code: link.code, password: secret });
      setCompleted(link.phase);
      linkStore.consume();
    } catch {
      setProblem(true);
    } finally {
      form.reset();
      verify.reset();
      password.reset();
    }
  }
  if (link === undefined) return <p role="status">Opening verification link…</p>;
  if (!link)
    return (
      <div className="space-y-5">
        <h1 className="text-2xl font-medium">Open your latest email link</h1>
        <p>This link is incomplete. Open the latest verification or password setup email.</p>
        <a className="underline underline-offset-4" href="/signup">
          Return to workspace setup
        </a>
      </div>
    );
  if (completed === "password")
    return (
      <div className="space-y-5">
        <h1 className="text-2xl font-medium">Your password is set</h1>
        <p>Continue workspace setup, then sign in and register your second factor.</p>
        <a className="underline underline-offset-4" href="/signup">
          Continue workspace setup
        </a>
      </div>
    );
  if (completed === "email")
    return (
      <div className="space-y-5">
        <h1 className="text-2xl font-medium">Your email is verified</h1>
        <p>Request a password setup email to finish creating your login.</p>
        <Button
          type="button"
          disabled={delivery.isPending || delivery.isSuccess}
          onClick={() => delivery.mutate()}
        >
          Send password setup email
        </Button>
        {delivery.isSuccess && (
          <p role="status">Open the password setup email to choose your password.</p>
        )}
        {delivery.error && <p role="alert">We could not request the email. Try again later.</p>}
        <a className="block underline underline-offset-4" href="/signup">
          Return to workspace setup
        </a>
      </div>
    );
  const pending = verify.isPending || password.isPending;
  return (
    <div className="space-y-5">
      <h1 className="text-2xl font-medium">
        {link.phase === "email" ? "Verify your email address" : "Set your password"}
      </h1>
      {problem && (
        <p role="alert">
          This request could not be completed. Check your password or request a fresh email link.
        </p>
      )}
      {link.phase === "email" ? (
        <Button type="button" disabled={pending} onClick={() => void prove()}>
          Verify email address
        </Button>
      ) : (
        <form
          noValidate
          className="space-y-5"
          onSubmit={form.handleSubmit((values) => prove(values.password))}
        >
          <Field
            id="signup-password"
            label="New password"
            error={form.formState.errors.password?.message}
          >
            <input
              id="signup-password"
              className="h-10 rounded-md border bg-background px-3"
              type="password"
              autoComplete="new-password"
              {...form.register("password")}
            />
          </Field>
          <Field
            id="signup-confirmation"
            label="Confirm password"
            error={form.formState.errors.confirmation?.message}
          >
            <input
              id="signup-confirmation"
              className="h-10 rounded-md border bg-background px-3"
              type="password"
              autoComplete="new-password"
              {...form.register("confirmation")}
            />
          </Field>
          <Button type="submit" disabled={pending}>
            Set password
          </Button>
        </form>
      )}
      <Button
        type="button"
        variant="outline"
        disabled={pending || delivery.isPending}
        onClick={() => delivery.mutate()}
      >
        Request a fresh email link
      </Button>
      {delivery.isSuccess && (
        <p role="status">If this request can receive an email, a fresh link has been requested.</p>
      )}
      {delivery.error && <p role="alert">We could not request the email. Try again later.</p>}
    </div>
  );
}

function signupLinkStore() {
  let current: Link | null | undefined;
  let initialized = false;
  const listeners = new Set<() => void>();
  return {
    snapshot: () => current,
    subscribe: (listener: () => void) => {
      listeners.add(listener);
      if (!initialized) {
        initialized = true;
        const fragment = window.location.hash;
        window.history.replaceState(null, "", window.location.pathname + window.location.search);
        const fields = new URLSearchParams(fragment.slice(1));
        const phase = fields.get("phase"),
          userId = fields.get("userId"),
          code = fields.get("code");
        current =
          fragment.length <= 1024 &&
          (phase === "email" || phase === "password") &&
          userId &&
          code &&
          ["phase", "userId", "code"].every((key) => fields.getAll(key).length === 1)
            ? { phase, userId, code }
            : null;
      }
      listener();
      return () => {
        listeners.delete(listener);
      };
    },
    consume: () => {
      if (current) current = { ...current, code: "" };
      for (const listener of listeners) listener();
    },
  };
}
