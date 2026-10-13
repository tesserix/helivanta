"use client";
import { Button } from "@tesserix/web";
import {
  apiFetch,
  useApiQuery,
  useApiMutation,
  usePermissions,
  brandingSchema,
  DEFAULT_BRANDING,
  type Branding,
} from "@helivanta/api";
import { HmsShell, Field, useZodForm, LoginSurface, FormSelectField } from "@helivanta/ui";
import { TenantPicker } from "@/components/tenant-picker";
const inputClass =
  "w-full rounded-lg border bg-background px-3 py-2.5 text-sm focus-visible:outline-2 focus-visible:outline-(--hms-accent)";
export default function BrandingPage() {
  const { can, isLoading } = usePermissions();
  const query = useApiQuery<Branding>(["workspace", "branding"], "/workspace/branding");
  return (
    <HmsShell active="/settings/branding" tenantPicker={<TenantPicker />}>
      <div className="mb-8">
        <p className="mb-2 text-xs font-semibold uppercase tracking-[0.18em] text-(--hms-accent)">
          Your organization
        </p>
        <h2 className="text-3xl font-semibold tracking-tight">Make it yours</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          A familiar welcome for your team, from sign-in to their daily workspace.
        </p>
      </div>
      {isLoading || query.isPending ? (
        <p role="status">Loading branding…</p>
      ) : !can("workspace.branding.manage") ? (
        <p role="alert">Only tenant administrators can change branding.</p>
      ) : query.isError ? (
        <p role="alert">
          Could not load branding. <Button onClick={() => void query.refetch()}>Try again</Button>
        </p>
      ) : (
        <BrandingEditor initial={query.data ?? DEFAULT_BRANDING} />
      )}
    </HmsShell>
  );
}
function BrandingEditor({ initial }: { initial: Branding }) {
  const form = useZodForm(brandingSchema, initial);
  const values = form.watch();
  const preview = brandingSchema.safeParse(values);
  const identity = useApiQuery<{ tenant_id: string }>(
    ["iam", "me", "permissions"],
    "/iam/me/permissions",
  );
  const save = useApiMutation(
    (value: Branding) =>
      apiFetch<Branding>("/workspace/branding", { method: "PUT", body: JSON.stringify(value) }),
    {
      invalidate: [["workspace", "branding"]],
      successToast: "Branding published",
      suppressErrorToast: true,
    },
  );
  return (
    <div className="grid items-start gap-7 xl:grid-cols-[minmax(300px,0.85fr)_minmax(0,1.15fr)]">
      <form
        noValidate
        onSubmit={form.handleSubmit((value) => save.mutate(value))}
        className="space-y-5 rounded-2xl border bg-card p-6 shadow-sm"
      >
        <h3 className="font-semibold">Brand identity</h3>
        <Field
          id="brand-name"
          label="Organization name"
          error={form.formState.errors.display_name?.message}
        >
          <input id="brand-name" className={inputClass} {...form.register("display_name")} />
        </Field>
        <Field
          id="brand-logo"
          label="Logo image URL"
          error={form.formState.errors.logo_url?.message}
        >
          <input
            id="brand-logo"
            className={inputClass}
            placeholder="https://…/your-logo.png"
            {...form.register("logo_url")}
          />
          <p className="text-xs font-normal text-muted-foreground">
            Use an existing public HTTPS image. Leave blank for a medical icon. This does not upload
            a file.
          </p>
        </Field>
        <Field
          id="brand-welcome"
          label="Welcome message"
          error={form.formState.errors.welcome_text?.message}
        >
          <textarea
            id="brand-welcome"
            className={inputClass}
            rows={3}
            {...form.register("welcome_text")}
          />
        </Field>
        <div className="grid gap-5 sm:grid-cols-2">
          <FormSelectField
            control={form.control}
            name="theme"
            id="brand-theme"
            label="Medical theme"
            options={[
              { value: "teal", label: "Clinical teal" },
              { value: "blue", label: "Care blue" },
              { value: "indigo", label: "Calm indigo" },
            ]}
          />
          <FormSelectField
            control={form.control}
            name="login_layout"
            id="brand-layout"
            label="Login layout"
            options={[
              { value: "split", label: "Split welcome" },
              { value: "centered", label: "Centered" },
            ]}
          />
        </div>
        <p className="border-t pt-4 text-xs leading-relaxed text-muted-foreground">
          Published branding is visible on your public login page and across your tenant workspace.
          Tesserix attribution and authentication controls always remain in place.
        </p>
        {save.isError && (
          <p role="alert" className="text-sm text-destructive">
            {save.error.message}
          </p>
        )}
        <Button type="submit" disabled={save.isPending}>
          {save.isPending ? "Publishing…" : "Publish branding"}
        </Button>
        {identity.data?.tenant_id && (
          <div className="border-t pt-4">
            <p className="mb-1 text-xs font-medium">Your tenant login page</p>
            <a
              className="break-all text-sm text-(--hms-accent) underline"
              href={`/login?tenant=${identity.data.tenant_id}`}
            >
              Open your branded login page
            </a>
          </div>
        )}
      </form>
      <section
        aria-label="Login preview"
        className="overflow-hidden rounded-2xl border bg-card shadow-sm"
      >
        <div className="flex justify-between border-b px-5 py-3">
          <h3 className="text-sm font-semibold">Live preview</h3>
          <span className="text-xs text-muted-foreground">Draft · publish to apply</span>
        </div>
        <div className="branding-preview pointer-events-none">
          <LoginSurface branding={preview.success ? preview.data : initial}>
            <div className="rounded-xl border bg-card p-6 shadow-sm">
              <h4 className="mb-5 text-xl font-semibold">Welcome back</h4>
              <div className="mb-4 space-y-2">
                <p className="text-xs text-muted-foreground">Email or username</p>
                <div className="rounded-lg border p-3 text-xs text-muted-foreground">
                  name@your-hospital.com
                </div>
              </div>
              <div className="mb-5 space-y-2">
                <p className="text-xs text-muted-foreground">Password</p>
                <div className="rounded-lg border p-3 text-xs text-muted-foreground">
                  ••••••••••••
                </div>
              </div>
              <div className="rounded-lg bg-(--hms-accent) p-3 text-center text-sm font-medium text-white">
                Sign in
              </div>
              <p className="mt-4 text-center text-xs text-muted-foreground">
                Secure access to your care workspace
              </p>
            </div>
          </LoginSurface>
        </div>
      </section>
    </div>
  );
}
