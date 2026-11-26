"use client";

import { useEffect, useRef, useState } from "react";
import { Loader2 } from "lucide-react";
import { z } from "zod";
import type { FormEvent } from "react";

import {
  ApiError,
  CustomerSignupInputContract,
  CustomerOrganizationProfileContract,
  CustomerSignupReceiptContract,
  usePublicReleaseCapabilities,
  useCustomerSignup,
  type CustomerSignupReceipt,
} from "@helivanta/api";
import { Button, Field, FormSelectField, RequestFailure, useZodForm } from "@helivanta/ui";
import { signinForCustomerSignup } from "@/lib/signup-signin";

// The wire contract leaves the owner email a bare string; the form checks its
// shape, as the native type="email" input did before #1855.
const signupFormSchema = CustomerSignupInputContract.extend({
  profile: CustomerOrganizationProfileContract,
  owner: CustomerSignupInputContract.shape.owner.extend({
    email: z.email("Enter a valid email address."),
  }),
});
const emptyAddress = {
  line1: "",
  line2: "",
  city: "",
  district: "",
  postalCode: "",
  stateCode: "MH" as const,
  countryCode: "IN" as const,
};
type SignupFormMethods = ReturnType<typeof useZodForm<typeof signupFormSchema>>;

export function SignupForm() {
  const [sameAddress, setSameAddress] = useState(true);
  const { progress, create, resend, begin } = useCustomerSignup();
  const formHeading = useRef<HTMLHeadingElement>(null);
  const form = useZodForm(signupFormSchema, {
    name: "",
    kind: "hospital",
    countryCode: "IN",
    stateCode: "MH",
    owner: { givenName: "", familyName: "", email: "" },
    profile: {
      contact: { phone: "", email: "" },
      headOffice: { ...emptyAddress },
      mainBranch: { name: "Main branch", address: { ...emptyAddress } },
    },
  });
  const parsed = CustomerSignupReceiptContract.safeParse(progress.data);
  const release = usePublicReleaseCapabilities(
    parsed.success && parsed.data.state === "not_started",
  );
  useEffect(() => {
    if (
      begin.isSuccess &&
      release.available &&
      !release.isPending &&
      !release.isError &&
      parsed.success &&
      parsed.data.state === "not_started"
    ) {
      formHeading.current?.focus({ preventScroll: true });
      formHeading.current?.scrollIntoView({ block: "start", behavior: "instant" });
    }
  }, [begin.isSuccess, progress.data, release.available, release.isPending, release.isError]);
  function beginAnother() {
    begin.mutate(undefined, {
      onSuccess: () => {
        form.reset();
        setSameAddress(true);
        create.reset();
        resend.reset();
      },
    });
  }
  // One quiet pending state until the first answer, so the card never paints
  // half its controls and then grows (#1767).
  if (progress.isPending) return <PendingCard />;
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
      <SignupStatus receipt={receipt} resend={resend} begin={begin} beginAnother={beginAnother} />
    );
  const errors = form.formState.errors;
  if (release.isPending) return <PendingCard />;
  if (release.isError)
    return (
      <RequestFailure
        message="Registration availability could not be verified."
        error={release.error}
        retry={release.refetch}
      />
    );
  if (!release.available) {
    return (
      <p role="status">
        New workspace registration is not available. Existing accounts can still sign in.
      </p>
    );
  }
  function submit(event: FormEvent<HTMLFormElement>) {
    form.setValue("stateCode", form.getValues("profile.headOffice.stateCode"));
    if (sameAddress)
      form.setValue("profile.mainBranch.address", { ...form.getValues("profile.headOffice") });
    void form.handleSubmit((values) => create.mutate(values))(event);
  }
  return (
    <form noValidate onSubmit={submit} className="space-y-5">
      <h2 ref={formHeading} tabIndex={-1} className="text-2xl font-medium outline-none">
        Create your workspace
      </h2>
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
      <fieldset className="space-y-4">
        <legend className="text-base font-semibold">Organisation contact</legend>
        <Field
          id="signup-phone"
          label="Organisation phone"
          error={errors.profile?.contact?.phone?.message}
        >
          <input
            className="h-10 rounded-md border bg-background px-3"
            id="signup-phone"
            type="tel"
            autoComplete="section-business tel"
            placeholder="+919876543210"
            {...form.register("profile.contact.phone")}
          />
        </Field>
        <p className="text-xs text-muted-foreground">
          Include the country code, for example +91 followed by the phone number.
        </p>
        <Field
          id="signup-contact-email"
          label="Organisation email"
          error={errors.profile?.contact?.email?.message}
        >
          <input
            className="h-10 rounded-md border bg-background px-3"
            id="signup-contact-email"
            type="text"
            inputMode="email"
            autoComplete="section-business email"
            {...form.register("profile.contact.email")}
          />
        </Field>
      </fieldset>
      <AddressFields form={form} prefix="profile.headOffice" label="Head office" />
      <fieldset className="space-y-4">
        <legend className="text-base font-semibold">Main branch</legend>
        <Field
          id="signup-branch-name"
          label="Main branch name"
          error={errors.profile?.mainBranch?.name?.message}
        >
          <input
            className="h-10 rounded-md border bg-background px-3"
            id="signup-branch-name"
            {...form.register("profile.mainBranch.name")}
          />
        </Field>
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={sameAddress}
            onChange={(event) => setSameAddress(event.target.checked)}
          />
          Main branch uses the head office address
        </label>
        {!sameAddress && (
          <AddressFields form={form} prefix="profile.mainBranch.address" label="Main branch" />
        )}
      </fieldset>
      <h3 className="text-base font-semibold">Owner login</h3>
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
          type="text"
          inputMode="email"
          autoComplete="email"
          {...form.register("owner.email")}
        />
      </Field>
      <p className="text-sm text-muted-foreground">
        After email verification, you will own this workspace. Staff, professional credentials and
        operating permissions are set up separately.
      </p>
      {create.error && <p role="alert">We could not submit your workspace. Please try again.</p>}
      {create.isError && (
        <Button type="button" variant="outline" disabled={begin.isPending} onClick={beginAnother}>
          Start a fresh signup
        </Button>
      )}
      <Button type="submit" disabled={create.isPending} className="w-full">
        {create.isPending ? "Submitting…" : "Create workspace"}
      </Button>
      <a href="/login?start=1" className="block text-center text-sm underline underline-offset-4">
        Sign in to an existing workspace
      </a>
    </form>
  );
}

function SignupStatus({
  receipt,
  resend,
  begin,
  beginAnother,
}: {
  begin: ReturnType<typeof useCustomerSignup>["begin"];
  beginAnother: () => void;
  receipt: CustomerSignupReceipt;
  resend: ReturnType<typeof useCustomerSignup>["resend"];
}) {
  const [signingIn, setSigningIn] = useState(false);
  const [signinFailed, setSigninFailed] = useState(false);
  async function signin() {
    setSigningIn(true);
    setSigninFailed(false);
    try {
      await signinForCustomerSignup(receipt.state === "ready" ? receipt.tenantId : undefined);
    } catch {
      setSigningIn(false);
      setSigninFailed(true);
    }
  }
  const heading = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    heading.current?.focus({ preventScroll: true });
    heading.current?.scrollIntoView({ block: "center", behavior: "instant" });
  }, [receipt.state]);

  return (
    <div aria-live="polite" className="space-y-6">
      <div className="space-y-3">
        <h2
          ref={heading}
          tabIndex={-1}
          className="text-2xl font-medium tracking-tight outline-none"
        >
          {receipt.state === "ready"
            ? `${receipt.name} is ready`
            : receipt.state === "needs_support"
              ? "Setup needs a hand"
              : receipt.state === "password_action_required"
                ? "Your email is verified"
                : receipt.state === "email_action_required"
                  ? "Check your email"
                  : "Setting up your workspace"}
        </h2>
        <p className="text-sm leading-relaxed text-muted-foreground">
          {receipt.state === "ready"
            ? "Sign in to finish setting up security and your team."
            : receipt.state === "needs_support"
              ? "Contact support. We've saved everything you entered."
              : receipt.state === "password_action_required"
                ? "Next, set your password. We'll email you a link."
                : receipt.state === "email_action_required"
                  ? "We sent a verification link to your email. Open it to continue."
                  : null}
        </p>
        {receipt.state === "provisioning" && (
          <div className="flex items-start gap-3 text-sm text-muted-foreground">
            <Loader2 aria-hidden className="mt-0.5 size-4 shrink-0 animate-spin" />
            {/* No sign-in promise here: since one-email signup (#1763) the tab
                that opens the email link signs in, not this one (#1767). */}
            <p>This can take up to 2 minutes.</p>
          </div>
        )}
      </div>
      {receipt.state === "ready" && (
        <Button type="button" className="w-full" disabled={signingIn} onClick={() => void signin()}>
          {signingIn ? "Opening sign-in…" : "Open workspace"}
        </Button>
      )}
      {receipt.state === "ready" && (
        <a
          className="block text-sm underline underline-offset-4"
          href={`/login?workspace=${receipt.tenantId}`}
        >
          Save your workspace sign-in link
        </a>
      )}
      {(receipt.state === "email_action_required" ||
        receipt.state === "password_action_required") && (
        <div className="space-y-3">
          {/* Opening the inbox is the next step, so resending is secondary;
              a legacy receipt's password link is still the only way on. */}
          <Button
            type="button"
            variant={receipt.state === "password_action_required" ? undefined : "outline"}
            className="w-full"
            disabled={resend.isPending}
            onClick={() => resend.mutate()}
          >
            {resend.isPending
              ? "Sending…"
              : receipt.state === "password_action_required"
                ? "Send password link"
                : "Resend email"}
          </Button>
          <p className="text-sm text-muted-foreground">Check spam if it hasn&apos;t arrived.</p>
          {resend.isSuccess && (
            <p role="status" className="text-sm text-muted-foreground">
              If this address can receive email, a new link is on its way.
            </p>
          )}
          {resend.error && (
            <p role="alert" className="text-sm text-destructive">
              {emailRequestRefusal(resend.error)}
            </p>
          )}
        </div>
      )}
      {signinFailed && (
        <p role="alert" className="text-sm text-destructive">
          We could not start sign-in. Please try again.
        </p>
      )}
      <div className="space-y-3 border-t border-border pt-5">
        <Button
          type="button"
          variant="outline"
          className="w-full"
          disabled={begin.isPending || signingIn}
          onClick={beginAnother}
        >
          {begin.isPending ? "Opening blank signup…" : "Create another workspace"}
        </Button>
        {begin.isError && (
          <p role="alert">Could not confirm a new signup. Refresh to check your current setup.</p>
        )}

        <a href="/login?start=1" className="block text-center text-sm underline underline-offset-4">
          Sign in to an existing workspace
        </a>
      </div>
    </div>
  );
}

function AddressFields({
  form,
  prefix,
  label,
}: {
  form: SignupFormMethods;
  prefix: "profile.headOffice" | "profile.mainBranch.address";
  label: "Head office" | "Main branch";
}) {
  const fields = [
    ["line1", "address line 1", "address-line1"],
    ["line2", "address line 2 (optional)", "address-line2"],
    ["city", "city", "address-level2"],
    ["district", "district", "address-level3"],
    ["postalCode", "PIN code", "postal-code"],
  ] as const;
  return (
    <fieldset className="space-y-4">
      <legend className="text-base font-semibold">{label} address</legend>
      {fields.map(([field, text, autocomplete]) => {
        const name = `${prefix}.${field}` as const;
        const id = `signup-${prefix.replaceAll(".", "-")}-${field}`;
        return (
          <Field
            key={field}
            id={id}
            label={`${label} ${text}`}
            error={form.getFieldState(name, form.formState).error?.message}
          >
            <input
              className="h-10 rounded-md border bg-background px-3"
              id={id}
              autoComplete={autocomplete}
              inputMode={field === "postalCode" ? "numeric" : "text"}
              {...form.register(name)}
            />
          </Field>
        );
      })}
      <FormSelectField
        control={form.control}
        name={`${prefix}.stateCode`}
        id={`signup-${prefix}-state`}
        label={label === "Head office" ? "State" : "Main branch state"}
        options={[
          { value: "MH", label: "Maharashtra" },
          { value: "OD", label: "Odisha" },
        ]}
      />
      <p className="text-sm text-muted-foreground">Country: India</p>
    </fieldset>
  );
}

// The email limits are told only when a request is refused (#1762): up front
// they are noise on every screen. Any other failure is a plain retry.
export function emailRequestRefusal(error: unknown): string {
  return error instanceof ApiError && error.status === 429
    ? "You've requested several emails. Try again in about an hour."
    : "We couldn't send the email. Try again.";
}

function PendingCard() {
  return (
    <div role="status" className="flex justify-center py-10">
      <Loader2 aria-hidden className="size-5 animate-spin text-muted-foreground" />
      <span className="sr-only">Loading</span>
    </div>
  );
}
