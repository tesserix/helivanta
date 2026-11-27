// The browser side of #1898's security keys. The API relays Zitadel's
// WebAuthn options verbatim as JSON (a top-level `publicKey`, every binary
// field base64url), and relays the browser's credential back to Zitadel
// verbatim (spec S1). These helpers are the only translation between that
// JSON and the ArrayBuffers navigator.credentials works in. Nothing here
// verifies anything: the authenticator and Zitadel do.
//
// The browser's own PublicKeyCredential.parseCreationOptionsFromJSON/toJSON
// would do the same, but not every supported browser has them yet.

const BASE64URL = /^[A-Za-z0-9_-]*$/;

// Throws on anything that is not unpadded base64url (S1: Zitadel sends none):
// a malformed field is a server contract violation, never something to guess.
export function base64urlToBuffer(value: string): ArrayBuffer {
  if (!BASE64URL.test(value) || value.length % 4 === 1) {
    throw new Error("Sign-in could not be completed: the server sent an unexpected response.");
  }
  const binary = atob(value.replace(/-/g, "+").replace(/_/g, "/"));
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes.buffer;
}

export function bufferToBase64url(buffer: ArrayBuffer | ArrayBufferView): string {
  const bytes = ArrayBuffer.isView(buffer)
    ? new Uint8Array(buffer.buffer, buffer.byteOffset, buffer.byteLength)
    : new Uint8Array(buffer);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function supportsSecurityKeys(): boolean {
  return (
    typeof globalThis.PublicKeyCredential === "function" &&
    typeof navigator !== "undefined" &&
    typeof navigator.credentials?.create === "function" &&
    typeof navigator.credentials?.get === "function"
  );
}

type Json = Record<string, unknown>;
type Descriptor = { id: string } & Json;

function publicKeyOf(options: unknown): Json & { challenge: string } {
  const pk = (options as { publicKey?: unknown } | null)?.publicKey as Json | undefined;
  if (!pk || typeof pk !== "object" || typeof pk.challenge !== "string") {
    throw new Error("Sign-in could not be completed: the server sent an unexpected response.");
  }
  return pk as Json & { challenge: string };
}

function descriptors(list: unknown) {
  if (list === undefined || list === null) return undefined;
  if (!Array.isArray(list) || list.some((d: Descriptor) => typeof d?.id !== "string")) {
    throw new Error("Sign-in could not be completed: the server sent an unexpected response.");
  }
  return (list as Descriptor[]).map((d) => ({ ...d, id: base64urlToBuffer(d.id) }));
}

// Every field the server sent is kept; only the binary ones are decoded.
export function toCreationOptions(options: unknown): CredentialCreationOptions {
  const pk = publicKeyOf(options);
  const user = pk.user as { id?: unknown } | undefined;
  if (!user || typeof user.id !== "string") {
    throw new Error("Sign-in could not be completed: the server sent an unexpected response.");
  }
  const excludeCredentials = descriptors(pk.excludeCredentials);
  return {
    publicKey: {
      ...pk,
      challenge: base64urlToBuffer(pk.challenge),
      user: { ...user, id: base64urlToBuffer(user.id) },
      ...(excludeCredentials ? { excludeCredentials } : {}),
    } as unknown as PublicKeyCredentialCreationOptions,
  };
}

export function toRequestOptions(options: unknown): CredentialRequestOptions {
  const pk = publicKeyOf(options);
  const allowCredentials = descriptors(pk.allowCredentials);
  return {
    publicKey: {
      ...pk,
      challenge: base64urlToBuffer(pk.challenge),
      ...(allowCredentials ? { allowCredentials } : {}),
    } as unknown as PublicKeyCredentialRequestOptions,
  };
}

// The registration JSON Zitadel's POST /v2/users/{id}/u2f/{u2fId} accepts.
export function attestationJSON(credential: PublicKeyCredential) {
  const response = credential.response as AuthenticatorAttestationResponse;
  return {
    id: credential.id,
    rawId: bufferToBase64url(credential.rawId),
    type: credential.type,
    response: {
      attestationObject: bufferToBase64url(response.attestationObject),
      clientDataJSON: bufferToBase64url(response.clientDataJSON),
    },
  };
}

// The assertion JSON Zitadel's checks.webAuthN.credentialAssertionData accepts.
export function assertionJSON(credential: PublicKeyCredential) {
  const response = credential.response as AuthenticatorAssertionResponse;
  return {
    id: credential.id,
    rawId: bufferToBase64url(credential.rawId),
    type: credential.type,
    response: {
      authenticatorData: bufferToBase64url(response.authenticatorData),
      clientDataJSON: bufferToBase64url(response.clientDataJSON),
      signature: bufferToBase64url(response.signature),
      userHandle: response.userHandle ? bufferToBase64url(response.userHandle) : null,
    },
  };
}
