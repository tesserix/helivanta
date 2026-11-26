import type { Metadata } from "next";

import { TenantLoginCard } from "@/components/login-branding";
import { SignupForm } from "./signup-form";

export const metadata: Metadata = { title: "Start your workspace | Helivanta" };

export default function SignupPage() {
  return (
    <TenantLoginCard entry label="Workspace registration">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">
          Bring your organisation into Helivanta.
        </h1>
        <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
          Start with your hospital or independent pharmacy. You can add your team after you sign in.
        </p>
      </div>
      <SignupForm />
    </TenantLoginCard>
  );
}
