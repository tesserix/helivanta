import assert from "node:assert/strict";
import test from "node:test";
import { mkdtempSync, mkdirSync, writeFileSync, chmodSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { nativeMFAIdentityEnvironment } from "./native-mfa-identity-env.mjs";

test("isolated native MFA setup passes only public metadata and a credential file path", () => {
  const root = mkdtempSync(path.join(tmpdir(), "native-mfa-env-"));
  try {
    const secrets = path.join(root, "dev/zitadel/secrets");
    mkdirSync(secrets, { recursive: true });
    writeFileSync(
      path.join(secrets, "identity-fixture.json"),
      JSON.stringify({
        projectId: "native-project",
        adminOrgId: "platform-org",
        machineUserId: "native-machine",
        machineClientId: "native-machine@platform.localhost",
      }),
      { mode: 0o600 },
    );
    writeFileSync(path.join(secrets, "identity-product.secret"), "synthetic-private-secret", {
      mode: 0o600,
    });
    const setupCredential = path.join(secrets, "native-staff-setup.pat");
    writeFileSync(setupCredential, "synthetic-private-setup-token", { mode: 0o600 });
    const env = {
      HELIVANTA_ENV: "dev",
      HELIVANTA_IDENTITY_FIXTURE: "true",
      HELIVANTA_STAFF_MFA_RECOVERY_FIXTURE: "true",
      ZITADEL_ISSUER_URL: "http://auth.tesserix.localhost:20080",
    };
    const values = nativeMFAIdentityEnvironment(root, env);
    assert.equal(values.ORGANIZATION_IDENTITY_URL, "https://onboarding.identity-fixture.invalid");
    assert.equal(values.ORGANIZATION_IDENTITY_CLIENT_ID, "native-machine@platform.localhost");
    assert.equal(values.ORGANIZATION_IDENTITY_TOKEN_PROJECT_ID, "native-project");
    assert.equal(
      values.ORGANIZATION_IDENTITY_CLIENT_SECRET_FILE,
      path.join(secrets, "identity-product.secret"),
    );
    assert.ok(!JSON.stringify(values).includes("synthetic-private-secret"));
    assert.ok(!JSON.stringify(values).includes("synthetic-private-setup-token"));
    chmodSync(setupCredential, 0o644);
    assert.throws(() => nativeMFAIdentityEnvironment(root, env));
    rmSync(setupCredential);
    assert.throws(() => nativeMFAIdentityEnvironment(root, env));
    assert.throws(() =>
      nativeMFAIdentityEnvironment(root, {
        HELIVANTA_ENV: "prod",
        HELIVANTA_IDENTITY_FIXTURE: "true",
        HELIVANTA_STAFF_MFA_RECOVERY_FIXTURE: "true",
        ZITADEL_ISSUER_URL: "http://auth.tesserix.localhost:20080",
      }),
    );
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
test("unrecognized opt-in fails and absent opt-in does not load local state", () => {
  assert.deepEqual(nativeMFAIdentityEnvironment("/missing", {}), {});
  assert.throws(() =>
    nativeMFAIdentityEnvironment("/missing", { HELIVANTA_STAFF_MFA_RECOVERY_FIXTURE: "yes" }),
  );
});
