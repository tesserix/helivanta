import { describe, expect, it, beforeEach, vi } from "vitest";
import { RENEW_AT_KEY, clearRenewAt, loadRenewAt, storeRenewAt } from "./renew-schedule";

// #916 Task 4, F3. This module carries the server's renewal schedule across
// the sign-in navigation and across any full page reload, which is what lets
// apps/shell schedule its FIRST renewal from SESSION_TTL rather than from a
// hardcoded five-minute client constant. Every branch below is a case the
// renewal loop actually depends on behaving a specific way.
describe("renew schedule", () => {
  beforeEach(() => {
    window.sessionStorage.clear();
    vi.restoreAllMocks();
  });

  it("round-trips an ISO timestamp", () => {
    storeRenewAt("2026-08-20T04:12:06.000Z");
    expect(loadRenewAt()?.toISOString()).toBe("2026-08-20T04:12:06.000Z");
  });

  it("round-trips a Date", () => {
    const when = new Date("2026-08-20T04:12:06.000Z");
    storeRenewAt(when);
    expect(loadRenewAt()?.toISOString()).toBe(when.toISOString());
  });

  it("is undefined when nothing was stored", () => {
    expect(loadRenewAt()).toBeUndefined();
  });

  // storeRenewAt is fed straight from a login/renewal response body, which
  // may legitimately lack the field. Writing "undefined" as a literal would
  // then poison every later read.
  it("ignores an absent value rather than storing a placeholder", () => {
    storeRenewAt(undefined);
    expect(window.sessionStorage.getItem(RENEW_AT_KEY)).toBeNull();
    expect(loadRenewAt()).toBeUndefined();
  });

  // A corrupted entry must read as "no schedule" — which
  // nextRenewalDelayMs already handles by falling back to its bounded
  // interval — never as an Invalid Date that would compute NaN and be
  // passed to setTimeout.
  it("treats an unparseable stored value as no schedule", () => {
    window.sessionStorage.setItem(RENEW_AT_KEY, "not-a-date");
    expect(loadRenewAt()).toBeUndefined();
  });

  // Deliberately NOT rejected: a tab restored hours later still holds
  // information, and nextRenewalDelayMs clamps a past instant up to
  // MIN_RENEWAL_DELAY_MS rather than firing a zero-delay retry storm.
  it("returns a past instant rather than discarding it", () => {
    const past = new Date(Date.now() - 60 * 60 * 1000);
    storeRenewAt(past);
    expect(loadRenewAt()?.toISOString()).toBe(past.toISOString());
  });

  it("clearRenewAt removes the entry", () => {
    storeRenewAt("2026-08-20T04:12:06.000Z");
    clearRenewAt();
    expect(window.sessionStorage.getItem(RENEW_AT_KEY)).toBeNull();
    expect(loadRenewAt()).toBeUndefined();
  });

  it("clearRenewAt is safe when nothing was stored", () => {
    expect(() => clearRenewAt()).not.toThrow();
  });

  // Safari's private mode and hardened enterprise profiles make
  // sessionStorage access THROW rather than return null. The renewal loop
  // runs this on mount and on sign-out; throwing there would break the
  // dashboard and the sign-out button respectively, so all three functions
  // fail soft — undefined is a case the caller already handles.
  it("survives a sessionStorage that throws on every access", () => {
    const boom = () => {
      throw new Error("SecurityError: sessionStorage is not available");
    };
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(boom);
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(boom);
    vi.spyOn(Storage.prototype, "removeItem").mockImplementation(boom);

    expect(() => storeRenewAt("2026-08-20T04:12:06.000Z")).not.toThrow();
    expect(loadRenewAt()).toBeUndefined();
    expect(() => clearRenewAt()).not.toThrow();
  });
});
