"use client";

import {
  CustomerSignupInputContract,
  CustomerSignupReceiptContract,
  useCustomerSignup,
} from "@helivanta/api";
import { Button, Field, FormSelectField, RequestFailure, useZodForm } from "@helivanta/ui";

export function SignupForm() {
  const { progress, create, resend } = useCustomerSignup();
  const form = useZodForm(CustomerSignupInputContract, {
    name: "",
    kind: "hospital",
    countryCode: "IN",
    stateCode: "MH",
    owner: { givenName: "", familyName: "", email: "" },
  });
  const parsed = CustomerSignupReceiptContract.safeParse(progress.data);
  if (progress.isPending) return <p role="status">Checking workspace setup…</p>;
  if (progress.isError || !parsed.success)
    return (
      <RequestFailure
        message="Workspace setup is unavailable. Try again."
        error={progress.error ?? (!parsed.success ? parsed.error : null)}
        retry={progress.refetch}
      />
    );
  const receipt = parsed.data;
  if (receipt.state !== "not_started")
    return (
      <div aria-live="polite" className="space-y-5">
        <h2 className="text-2xl font-medium">
          {receipt.state === "ready"
            ? "Your workspace is ready"
            : receipt.state === "needs_support"
              ? "Setup needs support"
              : receipt.state === "password_action_required"
                ? "Set your password"
                : receipt.state === "email_action_required"
                  ? "Check your email"
                  : "Setting up your workspace"}
        </h2>
        <p className="text-sm text-muted-foreground">
          {receipt.state === "ready"
            ? `${receipt.name} is ready. Sign in to set up your team and security.`
            : receipt.state === "needs_support"
              ? "Contact support to review this setup. Your request has been preserved."
              : receipt.state === "password_action_required"
                ? "Your email is verified. Request a password setup email to finish creating your login."
                : "We are preparing your workspace. Follow any email instructions to complete your login."}
        </p>
        {(receipt.state === "email_action_required" ||
          receipt.state === "password_action_required") && (
          <Button type="button" disabled={resend.isPending} onClick={() => resend.mutate()}>
            {receipt.state === "password_action_required"
              ? "Send password setup email"
              : "Resend verification email"}
          </Button>
        )}
        {resend.isSuccess && (
          <p role="status">If this request can receive an email, a link has been requested.</p>
        )}
        {resend.error && (
          <p role="alert">We could not request another link. Please try again later.</p>
        )}
        <a href="/login?start=1" className="underline underline-offset-4">
          Sign in to an existing workspace
        </a>
      </div>
    );
  const errors = form.formState.errors;
  return (
    <form
      noValidate
      onSubmit={form.handleSubmit((values) => create.mutate(values))}
      className="space-y-5"
    >
      <h2 className="text-2xl font-medium">Create your workspace</h2>
      <Field id="signup-name" label="Organisation name" error={errors.name?.message}>
        <input
          className="h-10 rounded-md border bg-background px-3"
          id="signup-name"
          autoComplete="organization"
          {...form.register("name")}
        />
      </Field>
      <FormSelectField
        control={form.control}
        name="kind"
        id="signup-kind"
        label="Workspace type"
        options={[
          { value: "hospital", label: "Hospital" },
          { value: "pharmacy", label: "Independent pharmacy" },
        ]}
      />
      <FormSelectField
        control={form.control}
        name="stateCode"
        id="signup-state"
        label="State"
        options={[
          { value: "MH", label: "Maharashtra" },
          { value: "OD", label: "Odisha" },
        ]}
      />
      <p className="text-sm text-muted-foreground">Country: India</p>
      <Field id="signup-given" label="Given name" error={errors.owner?.givenName?.message}>
        <input
          className="h-10 rounded-md border bg-background px-3"
          id="signup-given"
          autoComplete="given-name"
          {...form.register("owner.givenName")}
        />
      </Field>
      <Field id="signup-family" label="Family name" error={errors.owner?.familyName?.message}>
        <input
          className="h-10 rounded-md border bg-background px-3"
          id="signup-family"
          autoComplete="family-name"
          {...form.register("owner.familyName")}
        />
      </Field>
      <Field id="signup-email" label="Owner email" error={errors.owner?.email?.message}>
        <input
          className="h-10 rounded-md border bg-background px-3"
          id="signup-email"
          type="email"
          autoComplete="email"
          {...form.register("owner.email")}
        />
      </Field>
      <p className="text-sm text-muted-foreground">
        After email verification, you will own this workspace. Staff, professional credentials and
        operating permissions are set up separately.
      </p>
      {create.error && <p role="alert">We could not submit your workspace. Please try again.</p>}
      <Button type="submit" disabled={create.isPending} className="w-full">
        {create.isPending ? "Submitting…" : "Create workspace"}
      </Button>
      <a href="/login?start=1" className="block text-center text-sm underline underline-offset-4">
        Sign in to an existing workspace
      </a>
    </form>
  );
}
