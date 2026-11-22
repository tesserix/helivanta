import { UserManager } from "oidc-client-ts";
import { describe, expect, it } from "vitest";

import { MISSING_STATE, redirectFailure } from "./callback-error";

describe("redirectFailure", () => {
  // Pins the library's wording: if an upgrade changes it, the expired case
  // would fall back to the generic line, and this test says so.
  it("recognises the real library's missing-state error", async () => {
    const manager = new UserManager({
      authority: "https://auth.example.test",
      client_id: "helivanta-web",
      redirect_uri: "https://app.example.test/api/auth/callback",
    });
    const err = await manager
      .signinRedirectCallback("https://app.example.test/api/auth/callback?code=c&state=gone")
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(Error);
    expect((err as Error).message).toBe(MISSING_STATE);
    expect(redirectFailure(err)).toBe("Your sign-in took too long. Sign in again.");
  });

  it("never shows other library or provider text", () => {
    expect(redirectFailure(new Error("invalid_grant"))).toBe(
      "Sign-in didn't finish. Sign in again.",
    );
    expect(redirectFailure("not an error")).toBe("Sign-in didn't finish. Sign in again.");
  });
});
