"use client";

import { useState } from "react";
import {
  AccountDiscoveryCodeContract,
  AccountDiscoveryRequestContract,
  getAccountChoices,
  requestAccountCode,
  useApiMutation,
  verifyAccountCode,
  type AccountDiscoveryChoice,
} from "@helivanta/api";
import { Button, Field, useZodForm } from "@helivanta/ui";

type Props = { onSelect: (tenantId: string) => Promise<void> };

export function AccountDiscovery({ onSelect }: Props) {
  const [phase, setPhase] = useState<"email" | "code" | "choices">("email");
  const [choices, setChoices] = useState<AccountDiscoveryChoice[]>([]);
  const emailForm = useZodForm(AccountDiscoveryRequestContract, { email: "" });
  const codeForm = useZodForm(AccountDiscoveryCodeContract, { code: "" });
  const send = useApiMutation(requestAccountCode, {
    suppressErrorToast: true,
    onSuccess: () => {
      codeForm.reset({ code: "" });
      setPhase("code");
    },
  });
  async function loadChoices() {
    const result = await getAccountChoices();
    setChoices(result.accounts);
    setPhase("choices");
    if (result.accounts.length === 1) await onSelect(result.accounts[0].tenantId);
    return result;
  }
  const verify = useApiMutation(
    async (code: string) => {
      await verifyAccountCode(code);
      return loadChoices();
    },
    { suppressErrorToast: true },
  );
  const recover = useApiMutation(loadChoices, { suppressErrorToast: true });
  const select = useApiMutation((tenantId: string) => onSelect(tenantId), {
    suppressErrorToast: true,
  });
  const pending = send.isPending || verify.isPending || recover.isPending || select.isPending;
  const error = send.error ?? verify.error ?? recover.error ?? select.error;

  return (
    <div className="space-y-4">
      {phase === "email" ? (
        <form
          noValidate
          onSubmit={emailForm.handleSubmit(({ email }) => send.mutate(email))}
          className="space-y-4"
        >
          <Field
            id="account-email"
            label="Email"
            error={emailForm.formState.errors.email ? "Enter a valid email address." : undefined}
          >
            <input
              id="account-email"
              type="email"
              autoComplete="email"
              maxLength={200}
              disabled={pending}
              className="w-full rounded-md border bg-background px-3 py-2"
              {...emailForm.register("email")}
            />
          </Field>
          <p className="text-sm text-muted-foreground">
            We’ll email a code to help you securely find your hospital or pharmacy accounts.
          </p>
          <Button type="submit" size="lg" className="w-full" disabled={pending}>
            {pending ? "Sending code…" : "Sign in"}
          </Button>
        </form>
      ) : phase === "code" ? (
        <>
          <p className="text-sm text-muted-foreground">
            Check your email for an eight-digit code. It expires in ten minutes.
          </p>
          <form
            noValidate
            onSubmit={codeForm.handleSubmit(({ code }) => verify.mutate(code))}
            className="space-y-4"
          >
            <Field
              id="account-code"
              label="Email verification code"
              error={
                codeForm.formState.errors.code
                  ? "Enter the eight-digit code from your email."
                  : undefined
              }
            >
              <input
                id="account-code"
                inputMode="numeric"
                autoComplete="one-time-code"
                maxLength={8}
                disabled={pending}
                className="w-full rounded-md border bg-background px-3 py-2"
                {...codeForm.register("code")}
              />
            </Field>
            <Button type="submit" className="w-full" disabled={pending}>
              {verify.isPending ? "Verifying…" : "Verify email"}
            </Button>
          </form>
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              disabled={pending}
              onClick={() => {
                verify.reset();
                recover.reset();
                send.mutate(emailForm.getValues("email"));
              }}
            >
              Send a new code
            </Button>
            <Button
              variant="ghost"
              disabled={pending}
              onClick={() => {
                verify.reset();
                recover.mutate();
              }}
            >
              Retry account selection
            </Button>
            <Button
              variant="ghost"
              disabled={pending}
              onClick={() => {
                send.reset();
                verify.reset();
                recover.reset();
                setPhase("email");
              }}
            >
              Use another email
            </Button>
          </div>
        </>
      ) : (
        <>
          <h2 className="text-lg font-semibold">Choose an account</h2>
          {choices.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No eligible Helivanta accounts are available. Use another email, contact your
              organisation’s administrator, or{" "}
              <a href="/signup" className="underline">
                register your organisation
              </a>
              .
            </p>
          ) : (
            <div className="space-y-2">
              {choices.map((choice) => (
                <Button
                  key={choice.tenantId}
                  variant="outline"
                  className="h-auto w-full justify-start whitespace-normal py-3 text-left"
                  disabled={pending}
                  onClick={() => select.mutate(choice.tenantId)}
                >
                  <span>
                    <span className="block font-medium">{choice.name}</span>
                    <span className="block text-sm text-muted-foreground">{choice.account}</span>
                  </span>
                </Button>
              ))}
            </div>
          )}
          <p className="text-sm text-muted-foreground">
            Each account uses its own password and two-step verification.
          </p>
          <Button
            variant="ghost"
            disabled={pending}
            onClick={() => {
              select.reset();
              send.reset();
              verify.reset();
              recover.reset();
              setChoices([]);
              setPhase("email");
            }}
          >
            Use another email
          </Button>
        </>
      )}
      {error ? (
        <p role="alert" className="text-sm text-destructive">
          {error.message}
        </p>
      ) : null}
    </div>
  );
}
