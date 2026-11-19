import { appendFileSync, lstatSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

export function nativeMFAIdentityEnvironment(root, env) {
  const enabled = env.HELIVANTA_STAFF_MFA_RECOVERY_FIXTURE;
  if (!enabled) return {};
  if (
    enabled !== "true" ||
    env.HELIVANTA_ENV !== "dev" ||
    env.HELIVANTA_IDENTITY_FIXTURE !== "true" ||
    env.ZITADEL_ISSUER_URL !== "http://auth.tesserix.localhost:20080"
  )
    throw new Error("native MFA identity environment requires isolated opt-in");
  const directory = path.resolve(root, "dev/zitadel/secrets");
  const manifest = path.join(directory, "identity-fixture.json");
  const credential = path.join(directory, "identity-product.secret");
  for (const file of [manifest, credential]) {
    const info = lstatSync(file);
    if (
      !info.isFile() ||
      info.isSymbolicLink() ||
      (info.mode & 0o777) !== 0o600 ||
      info.size < 1 ||
      info.size > 8192
    )
      throw new Error("private native identity fixture state unavailable");
  }
  const data = JSON.parse(readFileSync(manifest, "utf8"));
  for (const key of ["projectId", "adminOrgId", "machineUserId"]) {
    if (typeof data[key] !== "string" || !/^[A-Za-z0-9_-]{1,200}$/.test(data[key]))
      throw new Error("native identity fixture metadata unavailable");
  }
  return {
    ORGANIZATION_IDENTITY_URL: "https://onboarding.identity-fixture.invalid",
    ORGANIZATION_IDENTITY_PRODUCT_KEY: "helivanta",
    ORGANIZATION_IDENTITY_TOKEN_PROJECT_ID: data.projectId,
    ORGANIZATION_IDENTITY_CLIENT_ID: data.machineUserId,
    ORGANIZATION_IDENTITY_CLIENT_SECRET_FILE: credential,
  };
}
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const values = nativeMFAIdentityEnvironment(process.cwd(), process.env);
  if (Object.keys(values).length) {
    if (!process.env.GITHUB_ENV)
      throw new Error("native CI identity environment destination is required");
    appendFileSync(
      process.env.GITHUB_ENV,
      Object.entries(values)
        .map(([key, value]) => `${key}=${value}`)
        .join("\n") + "\n",
    );
  }
}
