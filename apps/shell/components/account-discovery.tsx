"use client";

import { useEffect, useRef, useState } from "react";
import {
  ApiError,
  AccountDiscoveryCodeContract,
  AccountDiscoveryRequestContract,
  getAccountChoices,
  requestAccountCode,
  useApiMutation,
  verifyAccountCode,
  type AccountDiscoveryChoice,
} from "@helivanta/api";
import { Button, Field, useZodForm } from "@helivanta/ui";
import { MfaCodeForm } from "./mfa-code-form";

type Props = { onSelect: (tenantId: string, email?: string) => Promise<void> };

export function AccountDiscovery({ onSelect }: Props) {
  const [phase, setPhase] = useState<"email" | "code" | "choices">("email");
  const [choices, setChoices] = useState<AccountDiscoveryChoice[]>([]);
  const emailForm = useZodForm(AccountDiscoveryRequestContract, { email: "" });
  const [code, setCode] = useState("");
  const [codeError, setCodeError] = useState<string | undefined>();
  const send = useApiMutation(requestAccountCode, {
    suppressErrorToast: true,
    onSuccess: () => {
      setCode("");
      setCodeError(undefined);
      setPhase("code");
    },
  });
  async function choose(tenantId: string) {
    return onSelect(tenantId, emailForm.getValues("email") || undefined);
  }
  async function loadChoices() {
    const result = await getAccountChoices();
    setChoices(result.accounts);
    setPhase("choices");
    if (result.accounts.length === 1) await choose(result.accounts[0].tenantId);
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
  const select = useApiMutation(choose, { suppressErrorToast: true });
  const pending = send.isPending || verify.isPending || recover.isPending || select.isPending;
  const error = send.error ?? verify.error ?? recover.error ?? select.error;

  // Focus follows the phase (#1854 D2): the pressed button unmounts with its
  // phase. The email and code phases have no heading of their own, so their
  // field takes focus; MfaCodeForm focuses its first digit when it mounts.
  const choicesHeading = useRef<HTMLHeadingElement>(null);
  const focusedFor = useRef(phase);
  useEffect(() => {
    if (focusedFor.current === phase) return;
    focusedFor.current = phase;
    if (phase === "email") emailForm.setFocus("email");
    if (phase === "choices") choicesHeading.current?.focus();
  }, [phase, emailForm]);

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
              type="text"
              inputMode="email"
              autoComplete="email"
              disabled={pending}
              className="w-full rounded-md border bg-background px-3 py-2"
              {...emailForm.register("email")}
            />
          </Field>
          <p className="text-sm text-muted-foreground">
            We’ll email a code to help you securely find your hospital or pharmacy accounts.
          </p>
          <Button type="submit" size="lg" className="w-full" disabled={pending}>
            {pending ? "Sending code…" : "Send code"}
          </Button>
        </form>
      ) : phase === "code" ? (
        <>
          <p className="text-sm text-muted-foreground">
            Check your email for an eight-digit code. It expires in ten minutes.
          </p>
          {/* Submits only a complete eight-digit code; the contract check is
              kept so nothing else can reach the request. */}
          <MfaCodeForm
            length={8}
            value={code}
            onValueChange={(next) => {
              setCode(next);
              setCodeError(undefined);
            }}
            onSubmit={(submitted) => {
              if (!AccountDiscoveryCodeContract.safeParse({ code: submitted }).success) {
                setCodeError("Enter the eight-digit code from your email.");
                return;
              }
              verify.mutate(submitted);
            }}
            loading={pending}
            error={codeError}
            label="Email verification code"
            submitLabel={verify.isPending ? "Verifying…" : "Verify email"}
          />
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
              Show my accounts again
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
          <h2 ref={choicesHeading} tabIndex={-1} className="text-lg font-semibold">
            Choose an account
          </h2>
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
          {/* Only the API's own wording; a fetch or redirect failure is not ours. */}
          {error instanceof ApiError ? error.message : "Check your connection and try again."}
        </p>
      ) : null}
    </div>
  );
}
