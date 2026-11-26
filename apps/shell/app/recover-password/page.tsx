import type { Metadata } from "next";
import { PasswordRecoveryRequestContract } from "@helivanta/api";
import { PasswordRecovery } from "@/components/password-recovery";
import { TenantLoginCard } from "@/components/login-branding";

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
    <TenantLoginCard label="Password recovery">
      <h1 className="text-2xl font-semibold">Recover your password</h1>
      <PasswordRecovery authRequestId={parsed.success ? parsed.data : undefined} />
    </TenantLoginCard>
  );
}
