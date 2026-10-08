import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";

// oidc-client-ts's real UserManager talks to a real Zitadel origin; every
// test here replaces it with a spy-backed stand-in so signoutRedirect()/
// removeUser() are observable without a network.
const signoutRedirect = vi.hoisted(() => vi.fn());
const removeUser = vi.hoisted(() => vi.fn());
// A `function` expression, not an arrow: production calls `new UserManager()`,
// and since vitest 4 a mock invoked with `new` runs its implementation as a
// constructor. Arrow functions cannot be constructed, so an arrow here throws
// "is not a constructor" instead of returning the stub.
const UserManagerMock = vi.hoisted(() =>
  vi.fn().mockImplementation(function () {
    return { signoutRedirect, removeUser };
  }),
);
vi.mock("oidc-client-ts", () => ({
  UserManager: UserManagerMock,
  WebStorageStateStore: vi.fn(),
}));

// CRITICAL review finding (#848 task 7 follow-up): endZitadelSession used
// to set SIGNED_OUT_MARK unconditionally on every call, so the idle
// teardown path (which separately set IDLE_ENDED_MARK itself first) left
// BOTH marks in sessionStorage — and the stale SIGNED_OUT_MARK survived
// to lie on the NEXT arrival at /login. This file is the direct test of
// the fix: endZitadelSession now takes the mark to set as a required
// argument (or none), and this is the ONLY place in the codebase that
// exercises its actual sessionStorage.setItem call — packages/ui/src/
// hms-shell.test.tsx mocks this whole module away, so without this file
// the fix's core guarantee ("exactly the mark asked for, never a
// different one, never both") was completely untested.
describe("endZitadelSession", () => {
  const originalIssuer = process.env.NEXT_PUBLIC_ZITADEL_ISSUER_URL;
  const originalClientId = process.env.NEXT_PUBLIC_ZITADEL_CLIENT_ID;

  beforeEach(() => {
    vi.resetModules();
    signoutRedirect.mockReset().mockResolvedValue(undefined);
    removeUser.mockReset().mockResolvedValue(undefined);
    UserManagerMock.mockClear();
    window.sessionStorage.clear();
    process.env.NEXT_PUBLIC_ZITADEL_ISSUER_URL = "https://issuer.example";
    process.env.NEXT_PUBLIC_ZITADEL_CLIENT_ID = "helivanta-web";
    vi.stubGlobal("location", { ...window.location, href: "" });
  });

  afterEach(() => {
    if (originalIssuer === undefined) delete process.env.NEXT_PUBLIC_ZITADEL_ISSUER_URL;
    else process.env.NEXT_PUBLIC_ZITADEL_ISSUER_URL = originalIssuer;
    if (originalClientId === undefined) delete process.env.NEXT_PUBLIC_ZITADEL_CLIENT_ID;
    else process.env.NEXT_PUBLIC_ZITADEL_CLIENT_ID = originalClientId;
    vi.unstubAllGlobals();
  });

  it("sets exactly IDLE_ENDED_MARK, and never SIGNED_OUT_MARK, when asked for the idle mark", async () => {
    const { endZitadelSession, IDLE_ENDED_MARK, SIGNED_OUT_MARK } =
      await import("./zitadel-session");
    await endZitadelSession(IDLE_ENDED_MARK);
    expect(window.sessionStorage.getItem(IDLE_ENDED_MARK)).toBe("1");
    expect(window.sessionStorage.getItem(SIGNED_OUT_MARK)).toBeNull();
  });

  it("sets exactly SIGNED_OUT_MARK, and never IDLE_ENDED_MARK, when asked for the sign-out mark", async () => {
    const { endZitadelSession, IDLE_ENDED_MARK, SIGNED_OUT_MARK } =
      await import("./zitadel-session");
    await endZitadelSession(SIGNED_OUT_MARK);
    expect(window.sessionStorage.getItem(SIGNED_OUT_MARK)).toBe("1");
    expect(window.sessionStorage.getItem(IDLE_ENDED_MARK)).toBeNull();
  });

  // The other half of the fix: a caller with neither wording to offer
  // (hms-shell.tsx's D6 teardown on a non-`session_idle` 401) must be
  // able to end the session without stamping either mark.
  it("sets no mark at all when the caller passes none", async () => {
    const { endZitadelSession, IDLE_ENDED_MARK, SIGNED_OUT_MARK } =
      await import("./zitadel-session");
    await endZitadelSession(undefined);
    expect(window.sessionStorage.getItem(IDLE_ENDED_MARK)).toBeNull();
    expect(window.sessionStorage.getItem(SIGNED_OUT_MARK)).toBeNull();
  });

  // Ordering is load-bearing (this module's own doc comment): signoutRedirect()
  // is a real navigation, so the mark must already be written by the time
  // it is called, not merely by the time this function returns.
  it("sets the mark BEFORE calling signoutRedirect, since that call navigates away", async () => {
    const { endZitadelSession, IDLE_ENDED_MARK } = await import("./zitadel-session");
    let markAtSignoutRedirectCall: string | null = "unset";
    signoutRedirect.mockImplementation(async () => {
      markAtSignoutRedirectCall = window.sessionStorage.getItem(IDLE_ENDED_MARK);
    });

    await endZitadelSession(IDLE_ENDED_MARK);

    expect(markAtSignoutRedirectCall).toBe("1");
  });

  it("falls back to a same-origin /login redirect, setting no mark, when Zitadel config is unreachable", async () => {
    delete process.env.NEXT_PUBLIC_ZITADEL_ISSUER_URL;
    const { endZitadelSession, IDLE_ENDED_MARK, SIGNED_OUT_MARK } =
      await import("./zitadel-session");

    const result = await endZitadelSession(IDLE_ENDED_MARK);

    expect(result).toBe(false);
    expect(window.location.href).toBe("/login");
    expect(window.sessionStorage.getItem(IDLE_ENDED_MARK)).toBeNull();
    expect(window.sessionStorage.getItem(SIGNED_OUT_MARK)).toBeNull();
  });
});
