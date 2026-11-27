import type { Metadata } from "next";
import { TenantLoginCard } from "@/components/login-branding";
import { SignupVerification } from "./verification";

export const metadata: Metadata = {
  title: "Verify your workspace login | Helivanta",
  referrer: "no-referrer",
};
export default function SignupVerificationPage() {
  return (
    <TenantLoginCard label="Workspace login verification">
      <SignupVerification />
    </TenantLoginCard>
  );
}
