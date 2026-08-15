"use client";

import { useEffect } from "react";
import { getUserManager } from "@/lib/oidc";

// Loaded in a hidden iframe by oidc-client-ts's signinSilent()
// (lib/renew.ts), never navigated to directly. Registered as hms-web's
// silent_redirect_uri (scripts/zitadel-bootstrap.mjs; the path here MUST
// match lib/oidc.ts's SILENT_RENEW_PATH).
//
// signinSilentCallback() reads the authorization response off this
// frame's own URL, completes the same PKCE + state-validated exchange
// app/api/auth/callback/page.tsx does for a full redirect, and posts the
// result back to the parent window via postMessage — there is no
// separate, weaker validation path for the silent case.
export default function SilentRenewPage() {
  useEffect(() => {
    getUserManager().signinSilentCallback();
  }, []);

  return null;
}
