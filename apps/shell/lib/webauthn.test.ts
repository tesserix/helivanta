import { afterEach, describe, expect, it, vi } from "vitest";

import {
  assertionJSON,
  attestationJSON,
  base64urlToBuffer,
  bufferToBase64url,
  supportsSecurityKeys,
  toCreationOptions,
  toRequestOptions,
} from "./webauthn";

// The option shapes S1 recorded from Zitadel v4.19.5 (spec, "S1 result"):
// `publicKey` at the top, rp.id the boot-validated RP ID, rp.name "ZITADEL",
// and every binary field base64url without padding. The values are synthetic.
const creationOptions = {
  publicKey: {
    challenge: "q83vEjRWeJA",
    rp: { id: "helivanta.localhost", name: "ZITADEL" },
    user: { id: "MzQ1Njc4OTAxMjM0NTY3ODk", name: "a@x.test", displayName: "a Seed" },
    pubKeyCredParams: [
      { type: "public-key", alg: -7 },
      { type: "public-key", alg: -257 },
    ],
    timeout: 300000,
    excludeCredentials: [{ type: "public-key", id: "AAEC_-8" }],
    authenticatorSelection: { userVerification: "discouraged" },
    attestation: "none",
  },
};
const requestOptions = {
  publicKey: {
    challenge: "3q2-7w",
    timeout: 300000,
    rpId: "helivanta.localhost",
    allowCredentials: [{ type: "public-key", id: "AAEC_-8" }],
    userVerification: "discouraged",
  },
};

const bytes = (...values: number[]) => new Uint8Array(values).buffer;

describe("webauthn converters", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("round-trips base64url without padding, including the URL-safe characters", () => {
    for (const value of ["", "AA", "AAE", "AAEC", "AAEC_-8", "q83vEjRWeJA"]) {
      expect(bufferToBase64url(base64urlToBuffer(value))).toBe(value);
    }
    expect(new Uint8Array(base64urlToBuffer("AAEC_-8"))).toEqual(
      new Uint8Array([0, 1, 2, 0xff, 0xef]),
    );
  });

  it.each(["AA+/", "AA==x", "A", "has space", "AAE=", "é"])(
    "refuses %j, which is not base64url",
    (value) => {
      expect(() => base64urlToBuffer(value)).toThrow();
    },
  );

  it("decodes the creation options' binary fields and keeps every other field", () => {
    const options = toCreationOptions(creationOptions);
    const pk = options.publicKey!;
    expect(bufferToBase64url(pk.challenge as ArrayBuffer)).toBe("q83vEjRWeJA");
    expect(bufferToBase64url(pk.user.id as ArrayBuffer)).toBe("MzQ1Njc4OTAxMjM0NTY3ODk");
    expect(bufferToBase64url(pk.excludeCredentials![0]!.id as ArrayBuffer)).toBe("AAEC_-8");
    // Re-encoding every converted field gives back exactly what the server sent.
    expect({
      publicKey: {
        ...pk,
        challenge: bufferToBase64url(pk.challenge as ArrayBuffer),
        user: { ...pk.user, id: bufferToBase64url(pk.user.id as ArrayBuffer) },
        excludeCredentials: pk.excludeCredentials!.map((c) => ({
          ...c,
          id: bufferToBase64url(c.id as ArrayBuffer),
        })),
      },
    }).toEqual(creationOptions);
  });

  it("decodes the request options' binary fields and keeps every other field", () => {
    const pk = toRequestOptions(requestOptions).publicKey!;
    expect({
      publicKey: {
        ...pk,
        challenge: bufferToBase64url(pk.challenge as ArrayBuffer),
        allowCredentials: pk.allowCredentials!.map((c) => ({
          ...c,
          id: bufferToBase64url(c.id as ArrayBuffer),
        })),
      },
    }).toEqual(requestOptions);
  });

  it("refuses options that are not the server's shape rather than guessing", () => {
    expect(() => toCreationOptions({ challenge: "AA" })).toThrow();
    expect(() => toCreationOptions({ publicKey: { challenge: "AA" } })).toThrow();
    expect(() => toRequestOptions({ publicKey: { challenge: "A+" } })).toThrow();
    expect(() => toRequestOptions(null)).toThrow();
  });

  it("encodes a created credential as the JSON Zitadel verifies", () => {
    const credential = {
      id: "AAEC_-8",
      rawId: bytes(0, 1, 2, 0xff, 0xef),
      type: "public-key",
      response: { attestationObject: bytes(1, 2, 3), clientDataJSON: bytes(4, 5) },
    } as unknown as PublicKeyCredential;
    expect(attestationJSON(credential)).toEqual({
      id: "AAEC_-8",
      rawId: "AAEC_-8",
      type: "public-key",
      response: { attestationObject: "AQID", clientDataJSON: "BAU" },
    });
  });

  it("encodes an assertion, with a null user handle when the key gave none", () => {
    const assertion = (userHandle: ArrayBuffer | null) =>
      ({
        id: "AAEC_-8",
        rawId: bytes(0, 1, 2, 0xff, 0xef),
        type: "public-key",
        response: {
          authenticatorData: bytes(1),
          clientDataJSON: bytes(2),
          signature: bytes(3),
          userHandle,
        },
      }) as unknown as PublicKeyCredential;
    expect(assertionJSON(assertion(null))).toEqual({
      id: "AAEC_-8",
      rawId: "AAEC_-8",
      type: "public-key",
      response: {
        authenticatorData: "AQ",
        clientDataJSON: "Ag",
        signature: "Aw",
        userHandle: null,
      },
    });
    expect(assertionJSON(assertion(bytes(9))).response.userHandle).toBe("CQ");
  });

  it("supports security keys only where the browser has WebAuthn", () => {
    vi.stubGlobal("PublicKeyCredential", undefined);
    expect(supportsSecurityKeys()).toBe(false);
    vi.stubGlobal("PublicKeyCredential", function PublicKeyCredential() {});
    vi.stubGlobal("navigator", { credentials: { create: vi.fn(), get: vi.fn() } });
    expect(supportsSecurityKeys()).toBe(true);
    vi.stubGlobal("navigator", {});
    expect(supportsSecurityKeys()).toBe(false);
  });
});
