import { initializeApp, getApps } from "firebase/app";
import { connectAuthEmulator, getAuth, type Auth } from "firebase/auth";
import { env } from "./env";

declare global {
  interface Window {
    // Dev/test-only escape hatch — see the comment below. Never set in a
    // production build.
    __hmsFirebaseAuth?: Auth;
  }
}

export function firebaseAuth() {
  const app =
    getApps()[0] ??
    initializeApp({
      apiKey: env.NEXT_PUBLIC_GIP_API_KEY,
      projectId: env.NEXT_PUBLIC_GIP_PROJECT_ID,
    });
  const auth = getAuth(app);
  // Explicit only — never inferred from NODE_ENV. A production build with
  // a non-production NODE_ENV would otherwise silently point sign-in at a
  // local emulator. `make up` sets this variable for dev.
  const emulator = process.env.NEXT_PUBLIC_AUTH_EMULATOR_HOST;
  // avoids double-connect in fast refresh
  if (emulator && !auth.emulatorConfig) {
    connectAuthEmulator(auth, `http://${emulator}`, { disableWarnings: true });
  }
  // e2e/tests/signout.spec.ts reconstructs the shared-workstation attack
  // (#781) from inside `page.evaluate`, which runs as a plain browser
  // script with no bundler and therefore cannot `import("firebase/auth")`
  // — there is no import map for a bare specifier outside webpack/
  // turbopack. Stashing the already-initialized Auth instance on `window`
  // gives the test the same object `getAuth()` would hand back, without
  // adding a second code path server code could ever reach. Same
  // dev-only convention as the credential prefill in
  // `app/login/page.tsx`; never set in production.
  if (process.env.NODE_ENV !== "production" && typeof window !== "undefined") {
    window.__hmsFirebaseAuth = auth;
  }
  return auth;
}
