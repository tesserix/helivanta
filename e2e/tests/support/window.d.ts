// Ambient type for the test-only window hook apps/shell/lib/oidc.ts stashes
// the oidc-client-ts UserManager on (dev/test builds only — see that file's
// comment). e2e cannot import UserManager's real type without pulling
// apps/shell's dependency graph into this package's program, so this
// declares only the shape the specs actually call: getUser().id_token.
// ratelimit.spec.ts and signout.spec.ts are the two callers.
export {};

declare global {
  interface Window {
    __hmsUserManager?: {
      getUser(): Promise<{ id_token?: string } | null>;
    };
  }
}
