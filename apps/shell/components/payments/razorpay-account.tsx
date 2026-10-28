"use client";

import { z } from "zod";
import { Button } from "@tesserix/web";
import {
  ApiError,
  apiFetch,
  paymentAccountPath,
  useApiMutation,
  useApiQuery,
  type PaymentAccount,
  type PaymentAccountInput,
} from "@helivanta/api";
import { Field, FormSelectField, Panel, useZodForm } from "@helivanta/ui";

const inputClass =
  "w-full rounded-md border bg-background px-3 py-2.5 text-sm focus-visible:outline-2 focus-visible:outline-(--hms-accent)";

const secret = z
  .string()
  .trim()
  .refine((v) => v === "" || (v.length >= 8 && v.length <= 200), "Between 8 and 200 characters");

function accountSchema(saved: PaymentAccount) {
  return z
    .object({
      mode: z.enum(["test", "live"]),
      key_id: z
        .string()
        .trim()
        .regex(/^rzp_(test|live)_[A-Za-z0-9]{1,40}$/, "A Razorpay key id, like rzp_test_…"),
      key_secret: secret,
      webhook_secret: secret,
    })
    .superRefine((v, ctx) => {
      if (!v.key_id.startsWith(`rzp_${v.mode}_`))
        ctx.addIssue({
          code: "custom",
          path: ["key_id"],
          message: `A ${v.mode} account uses an rzp_${v.mode}_ key`,
        });
      // A new key id is a new key pair, so it needs its own secret.
      if (v.key_secret === "" && v.key_id !== saved.key_id)
        ctx.addIssue({
          code: "custom",
          path: ["key_secret"],
          message: "Enter the secret for this key id",
        });
      if (v.webhook_secret === "" && !saved.webhook_secret_set)
        ctx.addIssue({
          code: "custom",
          path: ["webhook_secret"],
          message: "Enter the webhook secret",
        });
    });
}

function saveError(error: Error): string {
  if (error instanceof ApiError) {
    if (error.code === "credentials_rejected")
      return "Razorpay didn't accept this key id and secret.";
    if (error.code === "provider_unavailable")
      return "Razorpay couldn't be reached to check the keys. Try again.";
    if (error.status === 503)
      return "Encrypted storage isn't available in this deployment, so keys can't be saved.";
  }
  return error.message;
}

/** The tenant's Razorpay account: tenant administrators only (#1386 C-4a). */
export function RazorpayAccount() {
  const query = useApiQuery<PaymentAccount>(["payments", "account"], paymentAccountPath);
  if (query.isPending) return <p role="status">Loading the payment account…</p>;
  if (query.isError)
    return (
      <p role="alert">
        Couldn't load the payment account.{" "}
        <Button onClick={() => void query.refetch()}>Try again</Button>
      </p>
    );
  // Keyed on the save time so a saved account resets the form to it.
  return <AccountForm key={query.data.updated_at ?? "new"} saved={query.data} />;
}

function AccountForm({ saved }: { saved: PaymentAccount }) {
  // Secret fields always start empty: no response ever carries a secret.
  const form = useZodForm(accountSchema(saved), {
    mode: saved.mode ?? "test",
    key_id: saved.key_id ?? "",
    key_secret: "",
    webhook_secret: "",
  });
  const save = useApiMutation(
    (value: PaymentAccountInput) =>
      apiFetch<PaymentAccount>(paymentAccountPath, { method: "PUT", body: JSON.stringify(value) }),
    {
      invalidate: [["payments", "account"]],
      successToast: "Razorpay account saved",
      suppressErrorToast: true,
    },
  );
  const errors = form.formState.errors;
  return (
    <div className="grid items-start gap-7 xl:grid-cols-[minmax(300px,0.9fr)_minmax(0,1.1fr)]">
      <Panel>
        <form
          noValidate
          className="space-y-5"
          onSubmit={form.handleSubmit((v) =>
            save.mutate({
              mode: v.mode,
              key_id: v.key_id,
              ...(v.key_secret ? { key_secret: v.key_secret } : {}),
              ...(v.webhook_secret ? { webhook_secret: v.webhook_secret } : {}),
            }),
          )}
        >
          <h3 className="font-semibold">Razorpay keys</h3>
          <FormSelectField
            control={form.control}
            name="mode"
            id="rzp-mode"
            label="Mode"
            options={[
              { value: "test", label: "Test — no real money" },
              { value: "live", label: "Live" },
            ]}
          />
          <Field id="rzp-key" label="Key id" error={errors.key_id?.message}>
            <input
              id="rzp-key"
              className={inputClass}
              autoComplete="off"
              {...form.register("key_id")}
            />
          </Field>
          <Field id="rzp-secret" label="Key secret" error={errors.key_secret?.message}>
            <input
              id="rzp-secret"
              type="password"
              className={inputClass}
              autoComplete="new-password"
              placeholder={saved.key_secret_set ? "Saved — leave blank to keep it" : ""}
              {...form.register("key_secret")}
            />
          </Field>
          <Field
            id="rzp-webhook-secret"
            label="Webhook secret"
            error={errors.webhook_secret?.message}
          >
            <input
              id="rzp-webhook-secret"
              type="password"
              className={inputClass}
              autoComplete="new-password"
              placeholder={saved.webhook_secret_set ? "Saved — leave blank to keep it" : ""}
              {...form.register("webhook_secret")}
            />
          </Field>
          <p className="border-t pt-4 text-xs leading-relaxed text-muted-foreground">
            The keys are checked with Razorpay before they are saved, and stored encrypted. They are
            never shown again.
          </p>
          {save.isError && (
            <p role="alert" className="text-sm text-destructive">
              {saveError(save.error)}
            </p>
          )}
          <Button type="submit" disabled={save.isPending}>
            {save.isPending ? "Saving…" : "Save Razorpay account"}
          </Button>
        </form>
      </Panel>
      <Panel aria-label="Account status">
        <h3 className="mb-3 font-semibold">Status</h3>
        {!saved.configured ? (
          <p className="text-sm text-muted-foreground">
            No Razorpay account yet. Until one is saved, the counter can't take verified UPI.
          </p>
        ) : (
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
            <dt className="text-muted-foreground">Mode</dt>
            <dd>{saved.mode === "live" ? "Live" : "Test"}</dd>
            <dt className="text-muted-foreground">Key id</dt>
            <dd>{saved.key_id}</dd>
            <dt className="text-muted-foreground">Key secret</dt>
            <dd>{saved.key_secret_set ? "Set" : "Not set"}</dd>
            <dt className="text-muted-foreground">Webhook secret</dt>
            <dd>{saved.webhook_secret_set ? "Set" : "Not set"}</dd>
            <dt className="text-muted-foreground">Webhook URL</dt>
            <dd className="break-all">
              {saved.webhook_url ? (
                <code>{saved.webhook_url}</code>
              ) : (
                "Not exposed in this deployment — payments are confirmed by checking Razorpay."
              )}
            </dd>
          </dl>
        )}
        <p className="mt-4 border-t pt-4 text-xs leading-relaxed text-muted-foreground">
          Razorpay must enable QR Codes on your account. Add the webhook URL in the Razorpay
          dashboard for the qr_code.credited and qr_code.closed events, with the same webhook
          secret.
        </p>
      </Panel>
    </div>
  );
}
