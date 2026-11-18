"use client";

import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { Button } from "@tesserix/web";
import { RefreshButton, RequestFailure } from "@helivanta/ui";
import {
  ApiError,
  PasswordRecoveryRequestContract,
  PasswordRecoverySubmitContract,
  requestPasswordRecovery,
  verifyPasswordRecovery,
  getPasswordRecoveryStatus,
  prepareRecoveryPassword,
  cancelRecoveryPassword,
  submitRecoveryPassword,
  newRecoveryCommandId,
  type PasswordRecoveryStatus,
} from "@helivanta/api";
import { MfaCodeForm } from "./mfa-code-form";

const emailSchema = PasswordRecoveryRequestContract.pick({ email: true });
const passwordSchema = z
  .object({
    password: PasswordRecoverySubmitContract.shape.password,
    confirm: z.string(),
  })
  .refine((value) => value.password === value.confirm, {
    path: ["confirm"],
    message: "The passwords must match.",
  });

export function PasswordRecovery({ authRequestId }: { authRequestId?: string }) {
  const client = useQueryClient();
  const queryKey = ["password-recovery", authRequestId ?? "retained"];
  const status = useQuery({
    queryKey,
    queryFn: getPasswordRecoveryStatus,
    retry: false,
    staleTime: 0,
    gcTime: 0,
    refetchOnWindowFocus: false,
    refetchInterval: (query) =>
      ["processing", "applied"].includes(query.state.data?.state ?? "") ? 2000 : false,
  });
  const [delivered, setDelivered] = useState(false);
  const [verified, setVerified] = useState(false);
  const [code, setCode] = useState("");
  const [problem, setProblem] = useState<string>();
  const [uncertain, setUncertain] = useState(false);
  const [freshCommand, setFreshCommand] = useState<string>();
  const command = useRef<string | undefined>(undefined);
  const delivery = useRef<{ request_id: string; email: string } | undefined>(undefined);
  const [deliveryUncertain, setDeliveryUncertain] = useState(false);
  const [hasDelivery, setHasDelivery] = useState(false);
  const password = useRef("");
  const proofCode = useRef("");
  const busy = useRef(false);
  const emailForm = useForm<z.infer<typeof emailSchema>>({
    resolver: zodResolver(emailSchema),
    defaultValues: { email: "" },
  });
  const passwordForm = useForm<z.infer<typeof passwordSchema>>({
    resolver: zodResolver(passwordSchema),
    defaultValues: { password: "", confirm: "" },
  });
  useEffect(
    () => () => {
      password.current = "";
      proofCode.current = "";
    },
    [],
  );

  async function beforeCommand() {
    setProblem(undefined);
    await client.cancelQueries({ queryKey });
  }
  function begin() {
    if (busy.current || status.isFetching) return false;
    busy.current = true;
    return true;
  }
  function receipt(value: PasswordRecoveryStatus) {
    client.setQueryData(queryKey, value);
    setUncertain(false);
  }
  const mail = useMutation({
    mutationFn: async () => {
      if (!authRequestId || !delivery.current) throw new Error("missing recovery intent");
      return requestPasswordRecovery({ auth_request_id: authRequestId, ...delivery.current });
    },
    onMutate: beforeCommand,
    retry: false,
    onSuccess: () => {
      setDelivered(true);
      setDeliveryUncertain(false);
    },
    onError: (error) => {
      setDeliveryUncertain(true);
      setProblem(
        error instanceof ApiError && error.status === 429
          ? "Too many codes requested. Wait before retrying. Up to three per hour and ten per day are allowed."
          : "Delivery could not be confirmed. Retry this delivery request before requesting a different code.",
      );
    },
    onSettled: () => {
      busy.current = false;
    },
  });
  const proof = useMutation({
    mutationFn: () => {
      const transientCode = proofCode.current;
      proofCode.current = "";
      return verifyPasswordRecovery(transientCode);
    },
    onMutate: beforeCommand,
    retry: false,
    onSuccess: async () => {
      setVerified(true);
      setCode("");
      await client.invalidateQueries({ queryKey });
    },
    onError: () => {
      setCode("");
      setProblem(
        "The code could not be verified. Check the eight digits or request a new code if it expired.",
      );
    },
    onSettled: () => {
      busy.current = false;
    },
  });
  const prepare = useMutation({
    mutationFn: () => prepareRecoveryPassword(command.current!),
    onMutate: beforeCommand,
    retry: false,
    onSuccess: (value) => {
      receipt(value);
      setFreshCommand(value.state === "prepared" ? command.current : undefined);
    },
    onError: () => {
      setUncertain(true);
      setProblem("We could not confirm the result. Check recovery status before continuing.");
    },
    onSettled: () => {
      busy.current = false;
    },
  });
  const cancel = useMutation({
    mutationFn: () => cancelRecoveryPassword(command.current),
    onMutate: beforeCommand,
    retry: false,
    onSuccess: (value) => {
      receipt(value);
      command.current = undefined;
      setFreshCommand(undefined);
    },
    onError: () => {
      setUncertain(true);
      setProblem(
        "This attempt could not be cancelled. Check its status; a password update may already be processing.",
      );
    },
    onSettled: () => {
      busy.current = false;
    },
  });
  const submit = useMutation({
    mutationFn: () => {
      const transientPassword = password.current;
      password.current = "";
      if (!command.current || !transientPassword) throw new Error("missing prepared password");
      return submitRecoveryPassword({ command_id: command.current, password: transientPassword });
    },
    onMutate: beforeCommand,
    retry: false,
    onSuccess: receipt,
    onError: () => {
      setUncertain(true);
      setProblem(
        "We could not confirm the result. Check recovery status before continuing. Your password will not be submitted again automatically.",
      );
    },
    onSettled: () => {
      password.current = "";
      busy.current = false;
    },
  });
  const pending =
    mail.isPending || proof.isPending || prepare.isPending || cancel.isPending || submit.isPending;
  const state = status.isError ? undefined : status.data?.state;
  const unknownCommand =
    status.error instanceof ApiError && status.error.code === "password_recovery_command_unknown";
  const readUnavailable =
    status.isError && !(status.error instanceof ApiError && status.error.status === 404);
  const knownTerminal = state === "completed" || state === "refused" || state === "cancelled";
  const unresolved = uncertain && !knownTerminal;
  const login = authRequestId
    ? `/login?authRequest=${encodeURIComponent(authRequestId)}`
    : "/login";

  function send(email: string, resend = false) {
    if (!begin()) return;
    if (!delivery.current || delivery.current.email !== email || (resend && !deliveryUncertain)) {
      delivery.current = { request_id: newRecoveryCommandId(), email };
    }
    setHasDelivery(true);
    mail.mutate();
  }
  const refresh = (
    <RefreshButton
      refresh={() => status.refetch()}
      error={status.error}
      busy={pending || status.isFetching}
    >
      Check recovery status
    </RefreshButton>
  );

  let content;
  if (status.isPending || status.isFetching || pending) {
    content = (
      <p role="status">
        {submit.isPending ? "Submitting your password securely…" : "Checking recovery securely…"}
      </p>
    );
  } else if (readUnavailable && !unknownCommand && !unresolved) {
    content = <p>Current recovery status is unavailable. Check it before continuing.</p>;
  } else if (state === "completed") {
    content = (
      <>
        <p>
          Your password has been updated. Sign in with your new password and your usual verification
          method.
        </p>
        <a className="font-medium text-primary underline" href={login}>
          Sign in with your new password
        </a>
      </>
    );
  } else if (freshCommand && state === "prepared" && !unresolved) {
    content = (
      <form
        className="space-y-4"
        onSubmit={(event) =>
          void passwordForm.handleSubmit((values) => {
            if (!begin()) return;
            password.current = values.password;
            passwordForm.reset();
            setFreshCommand(undefined);
            setUncertain(true);
            submit.mutate();
          })(event)
        }
      >
        <label className="block space-y-2">
          New password
          <input
            className="w-full rounded-md border p-3"
            type="password"
            autoComplete="new-password"
            maxLength={400}
            {...passwordForm.register("password")}
          />
        </label>
        {passwordForm.formState.errors.password && (
          <p role="alert">Enter a new password using up to 200 characters.</p>
        )}
        <label className="block space-y-2">
          Confirm new password
          <input
            className="w-full rounded-md border p-3"
            type="password"
            autoComplete="new-password"
            maxLength={400}
            {...passwordForm.register("confirm")}
          />
        </label>
        {passwordForm.formState.errors.confirm && <p role="alert">The passwords must match.</p>}
        <p className="text-sm text-muted-foreground">
          Your organization’s password policy applies.
        </p>
        <Button type="submit">Set new password</Button>
      </form>
    );
  } else if (
    state === "prepared" ||
    state === "processing" ||
    state === "applied" ||
    state === "unconfirmed" ||
    unresolved ||
    unknownCommand
  ) {
    content = (
      <>
        <p>
          {state === "prepared"
            ? "A password attempt was prepared. Cancel it before entering a new password."
            : "The password update is awaiting confirmation. Check its status or try normal sign-in with the new password. Contact your organization administrator if it remains unresolved."}
        </p>
        {refresh}
        {(state === "prepared" || unknownCommand) && (
          <Button
            variant="outline"
            onClick={() => {
              if (begin()) cancel.mutate();
            }}
          >
            Cancel prepared attempt
          </Button>
        )}
        <a className="text-primary underline" href={login}>
          Return to sign in
        </a>
      </>
    );
  } else if (state === "locked") {
    content = (
      <>
        <p role="alert">
          This account is locked. Contact your organization administrator. Tenant owners should
          contact Helivanta support.
        </p>
        {refresh}
      </>
    );
  } else if (state === "ready" || state === "refused" || state === "cancelled") {
    content = (
      <>
        {state === "refused" && (
          <p role="alert">
            The password did not meet your organization’s password policy. Prepare a new attempt and
            choose another password.
          </p>
        )}
        <p>Your email is verified. Prepare an attempt before choosing a new password.</p>
        <Button
          onClick={() => {
            if (!begin()) return;
            command.current = newRecoveryCommandId();
            setFreshCommand(undefined);
            prepare.mutate();
          }}
        >
          Choose a new password
        </Button>
      </>
    );
  } else if (verified) {
    content = (
      <>
        <p>Your email was verified. Check recovery status to continue.</p>
        {refresh}
      </>
    );
  } else if (delivered) {
    content = (
      <>
        <p>
          Check your email for an eight-digit code. Codes expire after ten minutes and allow five
          verification attempts.
        </p>
        <MfaCodeForm
          length={8}
          value={code}
          onValueChange={setCode}
          onSubmit={(value) => {
            if (!begin()) return;
            proofCode.current = value;
            setCode("");
            proof.mutate();
          }}
          label="Recovery email code"
          submitLabel="Verify recovery code"
        />
        <p className="text-sm text-muted-foreground">
          Up to three codes per hour and ten per day are allowed. Requesting a code does not
          guarantee delivery.
        </p>
        {hasDelivery ? (
          <Button variant="outline" onClick={() => send(delivery.current!.email, true)}>
            {deliveryUncertain ? "Retry delivery request" : "Send a new code"}
          </Button>
        ) : (
          <Button variant="outline" onClick={() => setDelivered(false)}>
            Request a new code
          </Button>
        )}
      </>
    );
  } else if (authRequestId) {
    content = (
      <form
        className="space-y-4"
        onSubmit={(event) => void emailForm.handleSubmit((values) => send(values.email))(event)}
      >
        <p>
          Use the email for the account in your selected organization. We will verify your mailbox
          before checking recovery eligibility.
        </p>
        <label className="block space-y-2">
          Email
          <input
            className="w-full rounded-md border p-3"
            type="email"
            autoComplete="email"
            {...emailForm.register("email")}
          />
        </label>
        {emailForm.formState.errors.email && <p role="alert">Enter a valid email address.</p>}
        <Button type="submit">Send recovery code</Button>
        <Button variant="ghost" type="button" onClick={() => setDelivered(true)}>
          I already have a recovery code
        </Button>
      </form>
    );
  } else {
    content = (
      <p>
        Start sign-in and choose your organization before requesting password recovery.{" "}
        <a className="text-primary underline" href="/login">
          Start sign-in
        </a>
      </p>
    );
  }
  return (
    <div className="space-y-5 text-sm">
      {problem && (
        <p role="alert" className="text-destructive">
          {problem}
        </p>
      )}
      {readUnavailable && (
        <RequestFailure
          error={status.error}
          message="Recovery status could not be checked. No password update has been confirmed."
          retry={() => status.refetch()}
          busy={pending || status.isFetching}
        />
      )}
      {content}
      {!status.data &&
        !status.isPending &&
        (delivered && status.error instanceof ApiError && status.error.status === 404 ? (
          <RequestFailure
            error={status.error}
            refused="Verified recovery status is not available yet. If you already verified your code, check again."
            message="Recovery status could not be checked."
            recheck="Check recovery status"
            retry={() => status.refetch()}
            busy={pending || status.isFetching}
          />
        ) : (
          refresh
        ))}
      <a className="block text-muted-foreground underline" href={login}>
        Back to sign in
      </a>
    </div>
  );
}
