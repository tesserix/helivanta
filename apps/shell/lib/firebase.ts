import { initializeApp, getApps } from "firebase/app";
import { connectAuthEmulator, getAuth } from "firebase/auth";
import { env } from "./env";

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
  return auth;
}
