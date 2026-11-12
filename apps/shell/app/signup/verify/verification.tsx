"use client";

import { useEffect, useState, useSyncExternalStore } from "react";
import { z } from "zod";
import {
  sendCustomerLink,
  setCustomerPassword,
  verifyCustomerEmail,
  useApiMutation,
  useApiQuery,
  CustomerSignupReceiptContract,
} from "@helivanta/api";
import { Button, Field, RequestFailure, useZodForm } from "@helivanta/ui";

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
  const progress = useApiQuery<unknown>(
    ["signup-link-status", link?.userId],
    `/auth/signup?userId=${encodeURIComponent(link?.userId ?? "")}`,
    {
      enabled: !!link,
      gcTime: 0,
      staleTime: 0,
      request: { cache: "no-store" },
      poll: (data) => {
        const parsed = CustomerSignupReceiptContract.safeParse(data);
        return (
          parsed.success && ["provisioning", "email_action_required"].includes(parsed.data.state)
        );
      },
    },
  );
  const parsed = CustomerSignupReceiptContract.safeParse(progress.data);
  const statusFailed = progress.isError || (!progress.isPending && !parsed.success);
  const statusError = progress.error ?? (!parsed.success ? parsed.error : null);
  const receipt = parsed.success && !progress.isError ? parsed.data : null;
  const ready =
    receipt?.state === "ready" && typeof receipt.tenantId === "string"
      ? { tenantId: receipt.tenantId }
      : null;
  const readyTenantId = ready?.tenantId;
  const alreadyVerified = receipt?.state === "password_action_required" || !!ready;
  const passwordDone = completed === "password" || (link?.phase === "password" && !!ready);
  const emailDone = completed === "email" || (link?.phase === "email" && alreadyVerified);
  useEffect(() => {
    if (
      (alreadyVerified && link?.phase === "email") ||
      (readyTenantId && link?.phase === "password")
    )
      linkStore.consume();
  }, [alreadyVerified, readyTenantId, link?.phase, linkStore]);
  const form = useZodForm(passwordForm, { password: "", confirmation: "" });
  const verify = useApiMutation(verifyCustomerEmail, { gcTime: 0, suppressErrorToast: true });
  const password = useApiMutation(setCustomerPassword, { gcTime: 0, suppressErrorToast: true });
  const delivery = useApiMutation(
    () =>
      sendCustomerLink(emailDone ? "password" : (link?.phase ?? "email"), {
        userId: link?.userId,
      }),
    {
      gcTime: 0,
      suppressErrorToast: true,
    },
  );
  async function prove(secret?: string) {
    if (!link || !link.code) return;
    setProblem(false);
    try {
      if (link.phase === "email")
        await verify.mutateAsync({ userId: link.userId, code: link.code });
      else await password.mutateAsync({ userId: link.userId, code: link.code, password: secret });
      setCompleted(link.phase);
      linkStore.consume();
      void progress.refetch();
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
  if (receipt?.state === "needs_support" || (receipt?.state === "provisioning" && !passwordDone))
    return (
      <div className="space-y-5">
        <h1 className="text-2xl font-medium">
          {receipt.state === "needs_support" ? "Setup needs support" : "Preparing your workspace"}
        </h1>
        <p>
          {passwordDone ? "Your password is set. " : ""}
          {receipt.state === "needs_support"
            ? "Contact support to review this setup. Your request has been preserved."
            : "Workspace setup is still in progress. This page will check for the next step; do not reuse your password setup link."}
        </p>
        <a className="underline underline-offset-4" href="/signup">
          Continue workspace setup
        </a>
      </div>
    );
  if (passwordDone)
    return (
      <div className="space-y-5">
        <h1 className="text-2xl font-medium">Your password is set</h1>
        {ready ? (
          <SignupLoginCountdown tenantId={ready.tenantId} />
        ) : (
          <>
            {statusFailed ? (
              <RequestFailure
                message="Your password is set, but workspace sign-in could not be checked. Try again."
                error={statusError}
                retry={progress.refetch}
              />
            ) : (
              <p role="status">
                Preparing your workspace sign-in. Your password is set; do not use this link again.
              </p>
            )}
            <a className="underline underline-offset-4" href="/signup">
              Continue workspace setup
            </a>
          </>
        )}
      </div>
    );
  if (emailDone)
    return (
      <div className="space-y-5">
        <h1 className="text-2xl font-medium">
          {completed === "email" ? "Your email is verified" : "Your email is already verified"}
        </h1>
        {ready ? (
          <SignupLoginCountdown tenantId={ready.tenantId} />
        ) : (
          <>
            <p>Request a password setup email to finish creating your login.</p>
            <EmailDeliveryPolicy />
            <Button type="button" disabled={delivery.isPending} onClick={() => delivery.mutate()}>
              Send password setup email
            </Button>
            {delivery.isSuccess && (
              <p role="status">
                If this request can receive an email, a password setup link has been requested.
              </p>
            )}
            {delivery.error && <p role="alert">We could not request the email. Try again later.</p>}
            <a className="block underline underline-offset-4" href="/signup">
              Return to workspace setup
            </a>
          </>
        )}
      </div>
    );
  if (progress.isPending)
    return <p role="status">Checking this workspace's verification status…</p>;
  if (progress.isError || !receipt)
    return (
      <RequestFailure
        message="We could not check this link's status. No verification or password change was made."
        error={statusError}
        retry={progress.refetch}
      />
    );
  const pending = verify.isPending || password.isPending;
  return (
    <div className="space-y-5">
      <h1 className="text-2xl font-medium">
        {link.phase === "email" ? "Verify your email address" : "Set your password"}
      </h1>
      <EmailDeliveryPolicy />
      {problem && (
        <p role="alert">
          This link may have expired or already been used. Check your password or request a fresh
          email link.
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

function SignupLoginCountdown({ tenantId }: { tenantId: string }) {
  const [seconds, setSeconds] = useState(7);
  const href = `/login?workspace=${encodeURIComponent(tenantId)}`;
  useEffect(() => {
    const countdown = window.setInterval(() => setSeconds((value) => Math.max(0, value - 1)), 1000);
    const redirect = window.setTimeout(() => window.location.assign(href), 7000);
    return () => {
      window.clearInterval(countdown);
      window.clearTimeout(redirect);
    };
  }, [href]);
  return (
    <div className="space-y-4">
      <p role="status">
        Taking you to your workspace sign-in in {seconds} seconds. Sign in and register your own
        second factor.
      </p>
      <a className="underline underline-offset-4" href={href}>
        Sign in now
      </a>
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
          /^[A-Za-z0-9_-]{1,200}$/.test(userId) &&
          code &&
          code.length <= 20 &&
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
      if (!current?.code) return;
      current = { ...current, code: "" };
      for (const listener of listeners) listener();
    },
  };
}

function EmailDeliveryPolicy() {
  return (
    <>
      <p className="text-sm text-muted-foreground">
        Links expire after 1 hour. If yours has expired, request a fresh email link below.
      </p>
      <p className="text-sm text-muted-foreground">
        Up to 3 emails per hour and 10 per 24 hours, shared across verification and password setup.
        Verified email addresses do not receive another verification email.
      </p>
    </>
  );
}
