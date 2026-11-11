import { describe, expect, it } from "vitest";
import { workspaceSigninScope } from "./workspace-signin";

describe("workspace sign-in scope", () => {
  it("pins the exact tenant organisation", () => {
    expect(workspaceSigninScope("11111111-1111-4111-8111-111111111111")).toBe(
      "openid profile email urn:zitadel:iam:org:id:helivanta-11111111-1111-4111-8111-111111111111",
    );
  });
  it.each([
    "",
    "other-org",
    "https://foreign.example",
    "11111111-1111-4111-8111-111111111111 openid",
    "00000000-0000-0000-0000-000000000000",
  ])("rejects an invalid workspace: %s", (value) => {
    expect(() => workspaceSigninScope(value)).toThrow("Enter a valid workspace ID");
  });
});
