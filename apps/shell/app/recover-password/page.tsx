import type { Metadata } from "next";
import { TesserixFooter } from "@helivanta/ui";
import { PasswordRecoveryRequestContract } from "@helivanta/api";
import { PasswordRecovery } from "@/components/password-recovery";

export const metadata: Metadata = {
  title: "Recover your password | Helivanta",
  referrer: "no-referrer",
};

export default async function PasswordRecoveryPage({
  searchParams,
}: {
  searchParams: Promise<{ authRequest?: string | string[] }>;
}) {
  const params = await searchParams;
  const parsed = PasswordRecoveryRequestContract.shape.auth_request_id.safeParse(
    params.authRequest,
  );
  return (
    <div className="public-site flex min-h-screen flex-col bg-background text-foreground">
      <header className="mx-auto w-full max-w-6xl px-6 py-8">
        <a href="/" className="text-xl font-semibold tracking-tight">
          helivanta.
        </a>
      </header>
      <main className="mx-auto flex w-full max-w-xl flex-1 items-center px-6 py-12">
        <section
          aria-label="Password recovery"
          className="w-full rounded-xl border border-border bg-card p-8 shadow-sm sm:p-10"
        >
          <h1 className="mb-5 text-2xl font-semibold">Recover your password</h1>
          <PasswordRecovery authRequestId={parsed.success ? parsed.data : undefined} />
        </section>
      </main>
      <TesserixFooter />
    </div>
  );
}
