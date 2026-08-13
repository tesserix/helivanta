"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { signInWithEmailAndPassword } from "firebase/auth";
import { z } from "zod";
import { Button, Input } from "@tesserix/web";
import { Field, useZodForm } from "@hms/ui";
import { clearPermissionsCache } from "@hms/api";
import { firebaseAuth } from "@/lib/firebase";

const loginSchema = z.object({
  email: z.string().email("Enter a valid email"),
  password: z.string().min(1, "Password is required"),
});

export default function LoginPage() {
  const router = useRouter();
  const [signInError, setSignInError] = useState<string | null>(null);
  const form = useZodForm(loginSchema, {
    email: process.env.NODE_ENV !== "production" ? "test@hms.dev" : "",
    password: process.env.NODE_ENV !== "production" ? "password123" : "",
  });

  // Initializes the Firebase client on arrival, not only on submit. Sign-out
  // (packages/ui/src/hms-shell.tsx) always ends in a hard navigation here,
  // so this is also the first point after sign-out where the SDK's session
  // state — cleared or not — becomes observable again; a login page that
  // waited for a submit to even construct the Auth instance would leave
  // that state effectively invisible until the next sign-in.
  useEffect(() => {
    firebaseAuth();
  }, []);

  async function onSubmit(values: z.infer<typeof loginSchema>) {
    setSignInError(null);
    try {
      const cred = await signInWithEmailAndPassword(firebaseAuth(), values.email, values.password);
      const idToken = await cred.user.getIdToken();
      const res = await fetch("/api/session", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ idToken }),
      });
      if (!res.ok) throw new Error("session");
      // Whoever signs in now owns this browser's permission cache. A
      // session that expired without an explicit sign-out leaves the
      // previous user's entry behind, so clearing here — not only on the
      // logout link — is what keeps the next user from seeing their nav.
      clearPermissionsCache();
      router.replace("/");
    } catch {
      setSignInError("Sign-in failed. Check your email and password.");
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-muted/30 p-4">
      <form
        noValidate
        onSubmit={form.handleSubmit(onSubmit)}
        className="w-full max-w-sm space-y-4 rounded-lg border bg-card p-6 shadow-sm"
      >
        <div>
          <h1 className="text-xl font-semibold text-foreground">Sign in to HMS</h1>
          <p className="mt-1 text-sm text-muted-foreground">Hospital Management System</p>
        </div>
        <Field id="email" label="Email" error={form.formState.errors.email?.message}>
          <Input
            id="email"
            type="email"
            placeholder="user@example.com"
            {...form.register("email")}
            className="mt-1.5"
          />
        </Field>
        <Field id="password" label="Password" error={form.formState.errors.password?.message}>
          <Input
            id="password"
            type="password"
            placeholder="Enter your password"
            {...form.register("password")}
            className="mt-1.5"
          />
        </Field>
        {signInError && (
          <p role="alert" className="text-sm text-destructive">
            {signInError}
          </p>
        )}
        <Button type="submit" disabled={form.formState.isSubmitting} className="w-full">
          {form.formState.isSubmitting ? "Signing in…" : "Sign in"}
        </Button>
      </form>
    </main>
  );
}
