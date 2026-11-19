import { lstatSync, readFileSync } from "node:fs";

const nativeIssuer = "http://auth.tesserix.localhost:20080";
const username = "helivanta-local-identity-product";
const validID = (value) => typeof value === "string" && /^[A-Za-z0-9_-]{1,200}$/.test(value);

// Only isolated native setup may create this product machine; never rotate it on retry.
export async function ensureLocalIdentityMachine({
  issuer,
  pat,
  org,
  readSecret,
  writeSecret,
  fetcher = fetch,
}) {
  if (
    issuer !== nativeIssuer ||
    !validID(org) ||
    typeof pat !== "string" ||
    !pat ||
    pat.length > 8192
  )
    throw new Error("identity machine fixture requires its isolated native issuer and owner");
  async function request(path, body) {
    const response = await fetcher(issuer + path, {
      method: "POST",
      redirect: "error",
      signal: AbortSignal.timeout(10000),
      headers: {
        Authorization: `Bearer ${pat}`,
        "Content-Type": "application/json",
        "x-zitadel-orgid": org,
      },
      body: JSON.stringify(body),
    });
    if (!response.ok) throw new Error("native identity machine setup unavailable");
    const text = await response.text();
    if (text.length > 8192) throw new Error("oversized native identity machine proof");
    try {
      return JSON.parse(text);
    } catch {
      throw new Error("invalid native identity machine proof");
    }
  }
  const found = await request("/management/v1/users/_search", {
    query: { limit: "2" },
    queries: [{ userNameQuery: { userName: username, method: "TEXT_QUERY_METHOD_EQUALS" } }],
  });
  if (!Array.isArray(found.result ?? []) || (found.result ?? []).length > 1)
    throw new Error("ambiguous native identity machine");
  if (found.result?.length === 1) {
    const user = found.result[0];
    const secret = readSecret();
    if (
      !validID(user.id) ||
      user.state !== "USER_STATE_ACTIVE" ||
      user.details?.resourceOwner !== org ||
      !user.machine ||
      user.human ||
      typeof secret !== "string" ||
      !secret ||
      secret.length > 8192
    )
      throw new Error(
        "native machine or retained credential unavailable; reset only the isolated fixture",
      );
    return user.id;
  }
  if (readSecret() !== null) throw new Error("retained credential has no native owner");
  const machine = await request("/management/v1/users/machine", {
    userName: username,
    name: "Helivanta isolated identity product",
    accessTokenType: "ACCESS_TOKEN_TYPE_JWT",
  });
  if (!validID(machine.userId)) throw new Error("native identity machine unavailable");
  const credential = await request(`/management/v1/users/${machine.userId}/secret`, {});
  if (
    credential.clientId !== machine.userId ||
    typeof credential.clientSecret !== "string" ||
    !credential.clientSecret ||
    credential.clientSecret.length > 8192 ||
    /[\r\n]/.test(credential.clientSecret)
  )
    throw new Error("native identity machine credential unavailable");
  writeSecret(credential.clientSecret);
  return machine.userId;
}

export function readPrivateFixtureSecret(file) {
  try {
    const info = lstatSync(file);
    if (
      !info.isFile() ||
      info.isSymbolicLink() ||
      (info.mode & 0o777) !== 0o600 ||
      info.size < 1 ||
      info.size > 8192
    )
      throw new Error("isolated product credential unavailable");
    const value = readFileSync(file, "utf8").trim();
    if (!value) throw new Error("isolated product credential unavailable");
    return value;
  } catch (error) {
    if (error.code === "ENOENT") return null;
    throw new Error("isolated product credential unavailable");
  }
}
