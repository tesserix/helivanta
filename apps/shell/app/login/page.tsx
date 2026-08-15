"use client";

import { useEffect, useState } from "react";
import { getUserManager } from "@/lib/oidc";

// Login is a redirect, not a form (design spec D5a). HMS renders no
// password field: Zitadel's hosted login at NEXT_PUBLIC_ZITADEL_ISSUER_URL
// owns the credential surface, MFA, password reset and lockout, so a
// compromised HMS frontend never has a password to harvest.
// apps/shell/app/api/auth/callback/page.tsx is the other half — the
// caller Zitadel redirects back to once the user has authenticated there.
//
// This used to be apps/shell/app/login/page.tsx: a Firebase
// email/password form calling signInWithEmailAndPassword directly. That
// form is deleted, not ported — per D5a it is precisely what must not
// carry over.
export default function LoginPage() {
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    getUserManager()
      .signinRedirect()
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : "Could not reach the sign-in page.");
      });
  }, []);

  if (error) {
    return (
      <main className="flex min-h-screen items-center justify-center p-4">
        <div className="max-w-sm text-center">
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        </div>
      </main>
    );
  }

  return (
    <main className="flex min-h-screen items-center justify-center p-4">
      <p className="text-sm text-muted-foreground">Redirecting to sign in…</p>
    </main>
  );
}
