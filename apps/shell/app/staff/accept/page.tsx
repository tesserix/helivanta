import type { Metadata } from "next";
import { TenantLoginCard } from "@/components/login-branding";
import { StaffAcceptance } from "./acceptance";

export const metadata: Metadata = {
  title: "Staff account setup | Helivanta",
  referrer: "no-referrer",
};

export default function StaffAcceptancePage() {
  return (
    <TenantLoginCard label="Staff account setup">
      <p className="text-sm font-medium text-muted-foreground">Staff account setup</p>
      <StaffAcceptance />
    </TenantLoginCard>
  );
}
