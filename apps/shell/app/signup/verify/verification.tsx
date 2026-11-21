"use client";

import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { z } from "zod";
import {
  sendCustomerLink,
  setCustomerPassword,
  verifyCustomerEmail,
  useApiMutation,
  useApiQuery,
  CustomerSignupReceiptContract,
} from "@helivanta/api";
import { Loader2 } from "lucide-react";
import { ApiError } from "@helivanta/api";
import { Button, Field, RequestFailure, useZodForm } from "@helivanta/ui";
import { signinForCustomerSignup } from "@/lib/signup-signin";
import { emailRequestRefusal } from "../signup-form";

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
  // "expired": the link's code was refused, so it is spent. "policy": the
  // identity owner refused the password itself; the link is intact (#1763).
  const [problem, setProblem] = useState<"expired" | "policy" | null>(null);
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
  // A completed email link also set the password (one request, #1763); only a
  // link reopened after an earlier, separate verification still owes one.
  const passwordDone =
    completed === "password" || completed === "email" || (link?.phase === "password" && !!ready);
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
  // One request per link (#1763): the email link verifies the address and
  // sets the password together, password first on the identity owner's side,
  // so a refused password leaves the code unspent and the form retries.
  async function prove(secret: string) {
    if (!link || !link.code) return;
    setProblem(null);
    try {
      if (link.phase === "email")
        await verify.mutateAsync({ userId: link.userId, code: link.code, password: secret });
      else await password.mutateAsync({ userId: link.userId, code: link.code, password: secret });
      setCompleted(link.phase);
      linkStore.consume();
      void progress.refetch();
    } catch (error) {
      if (error instanceof ApiError && error.code === "password_policy") {
        setProblem("policy");
        return;
      }
      setProblem("expired");
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
        <p>This link is incomplete. Open the most recent email from Helivanta.</p>
        <a className="underline underline-offset-4" href="/signup">
          Return to workspace setup
        </a>
      </div>
    );
  if (receipt?.state === "needs_support" || (receipt?.state === "provisioning" && !passwordDone))
    return (
      <div className="space-y-5">
        <h1 className="text-2xl font-medium">
          {receipt.state === "needs_support" ? "Setup needs a hand" : "Setting up your workspace"}
        </h1>
        {receipt.state === "needs_support" ? (
          <p>Contact support. We&apos;ve saved everything you entered.</p>
        ) : (
          <SetupWait />
        )}
      </div>
    );
  if (passwordDone)
    return (
      <div className="space-y-5">
        <h1 className="text-2xl font-medium">
          {ready ? "Signing you in" : "Setting up your workspace"}
        </h1>
        {ready ? (
          <SignupSignin tenantId={ready.tenantId} />
        ) : (
          <>
            {statusFailed ? (
              <RequestFailure
                message="Your password is set, but workspace sign-in could not be checked. Try again."
                error={statusError}
                retry={progress.refetch}
              />
            ) : (
              <SetupWait />
            )}
          </>
        )}
      </div>
    );
  if (emailDone && completed !== "email")
    return (
      <div className="space-y-5">
        <h1 className="text-2xl font-medium">Your email is already verified</h1>
        {ready ? (
          <SignupSignin tenantId={ready.tenantId} />
        ) : (
          <>
            <p>Next, set your password. We&apos;ll email you a link.</p>
            <Button type="button" disabled={delivery.isPending} onClick={() => delivery.mutate()}>
              Send password link
            </Button>
            {delivery.isSuccess && (
              <p role="status">If this address can receive email, a new link is on its way.</p>
            )}
            {delivery.error && <p role="alert">{emailRequestRefusal(delivery.error)}</p>}
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
      <h1 className="text-2xl font-medium">Set your password</h1>
      {link.phase === "email" && <p>This also confirms your email address.</p>}
      {problem === "expired" && <p role="alert">This link has expired or was already used.</p>}
      {problem === "policy" && (
        <p role="alert">
          That password doesn&apos;t meet your workspace&apos;s password rules. Try a longer one
          with letters, numbers and a symbol.
        </p>
      )}
      {
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
      }
      {problem === "expired" && (
        <button
          type="button"
          className="text-sm underline underline-offset-4 disabled:opacity-50"
          disabled={pending || delivery.isPending}
          onClick={() => delivery.mutate()}
        >
          Send a new link
        </button>
      )}
      {delivery.isSuccess && (
        <p role="status">If this address can receive email, a new link is on its way.</p>
      )}
      {delivery.error && <p role="alert">{emailRequestRefusal(delivery.error)}</p>}
    </div>
  );
}

// Provisioning takes a minute or two (measured 1m40s-1m45s on 2026-11-20).
// The page polls and signs in by itself, so it says so and shows it is alive.
function SetupWait() {
  return (
    <div role="status" className="flex items-start gap-3 text-sm text-muted-foreground">
      <Loader2 aria-hidden className="mt-0.5 size-4 shrink-0 animate-spin" />
      <p>This can take up to 2 minutes. We&apos;ll sign you in as soon as it&apos;s ready.</p>
    </div>
  );
}

// The password was set moments ago, so sign-in starts at once (#1763 D4):
// the scoped request for this workspace, with prompt=login, exactly what the
// removed "Open workspace" button did. A failed start leaves a link to retry.
function SignupSignin({ tenantId }: { tenantId: string }) {
  const [failed, setFailed] = useState(false);
  const started = useRef(false);
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    // A synchronous throw (state parse, OIDC settings) must land in the same
    // failed state as a rejected redirect, so the person always gets a link.
    Promise.resolve()
      .then(() => signinForCustomerSignup(tenantId))
      .catch(() => setFailed(true));
  }, [tenantId]);
  if (failed)
    return (
      <div className="space-y-4">
        <p role="alert">We couldn&apos;t open sign-in. Try again.</p>
        <a
          className="underline underline-offset-4"
          href={`/login?workspace=${encodeURIComponent(tenantId)}`}
        >
          Sign in
        </a>
      </div>
    );
  return <p role="status">Opening sign-in…</p>;
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
