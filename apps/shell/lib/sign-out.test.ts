import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";

const signOut = vi.hoisted(() => vi.fn());
vi.mock("firebase/auth", () => ({ signOut }));
vi.mock("./firebase", () => ({ firebaseAuth: () => ({ name: "test-auth" }) }));

import { signOutEverywhere } from "./sign-out";

describe("signOutEverywhere", () => {
  beforeEach(() => {
    signOut.mockReset();
    signOut.mockResolvedValue(undefined);
  });
  afterEach(() => vi.unstubAllGlobals());

  it("posts to /logout, then clears the Firebase SDK session, in that order", async () => {
    const calls: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init?: RequestInit) => {
        calls.push(`${init?.method ?? "GET"} ${url}`);
        return { ok: true, status: 200 };
      }),
    );
    signOut.mockImplementation(async () => {
      calls.push("signOut");
    });

    await signOutEverywhere();

    expect(calls).toEqual(["POST /logout", "signOut"]);
  });

  // The whole point of this function: a clinician on a shared terminal
  // must always be able to clear the local Firebase session, even when
  // the network round-trip to revoke server-side cannot complete.
  it("still clears the Firebase SDK session when the /logout request fails", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new Error("network down")));

    await signOutEverywhere();

    expect(signOut).toHaveBeenCalledTimes(1);
  });

  it("still clears the Firebase SDK session when /logout responds with an error status", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: false, status: 502 }));

    await signOutEverywhere();

    expect(signOut).toHaveBeenCalledTimes(1);
  });
});
