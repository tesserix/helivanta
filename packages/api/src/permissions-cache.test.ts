import { describe, expect, it, beforeEach, afterEach, vi } from "vitest";
import {
  PERMISSIONS_CACHE_KEY,
  PERMISSIONS_CACHE_TTL_MS,
  clearPermissionsCache,
  readPermissionsCache,
  writePermissionsCache,
} from "./permissions-cache";

const entry = {
  subject: "doc",
  tenantId: "11111111-1111-1111-1111-111111111111",
  permissions: ["medicore.visit.read"],
  storedAt: 1_700_000_000_000,
};

describe("permissions cache", () => {
  // Fake timers pinned to the entry's own storedAt: TTL expiry is the
  // behaviour under test, so the clock has to be the thing the test
  // moves, not the wall clock.
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
    vi.useFakeTimers();
    vi.setSystemTime(entry.storedAt);
  });
  afterEach(() => vi.useRealTimers());

  it("round-trips an entry", () => {
    writePermissionsCache(entry);

    expect(readPermissionsCache()).toEqual(entry);
  });

  it("ignores and removes an entry older than the TTL", () => {
    writePermissionsCache(entry);
    vi.setSystemTime(entry.storedAt + PERMISSIONS_CACHE_TTL_MS + 1);

    expect(readPermissionsCache()).toBeNull();
    expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).toBeNull();
  });

  it("keeps an entry that is within the TTL", () => {
    writePermissionsCache(entry);
    vi.setSystemTime(entry.storedAt + PERMISSIONS_CACHE_TTL_MS - 1);

    expect(readPermissionsCache()).toEqual(entry);
  });

  it("recovers from a corrupt entry by removing it", () => {
    window.localStorage.setItem(PERMISSIONS_CACHE_KEY, "{not json");

    expect(readPermissionsCache()).toBeNull();
    expect(window.localStorage.getItem(PERMISSIONS_CACHE_KEY)).toBeNull();
  });

  it("rejects an entry of the wrong shape", () => {
    window.localStorage.setItem(
      PERMISSIONS_CACHE_KEY,
      JSON.stringify({ subject: "doc", permissions: "not-an-array" }),
    );

    expect(readPermissionsCache()).toBeNull();
  });

  it("does not throw when localStorage is unavailable", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("SecurityError");
    });
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("QuotaExceededError");
    });

    expect(() => writePermissionsCache(entry)).not.toThrow();
    expect(readPermissionsCache()).toBeNull();
  });

  it("clears the entry", () => {
    writePermissionsCache(entry);
    clearPermissionsCache();

    expect(readPermissionsCache()).toBeNull();
  });
});
