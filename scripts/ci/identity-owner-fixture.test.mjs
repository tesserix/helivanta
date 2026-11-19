import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../", import.meta.url));
function invoke(enabled, mfa = "") {
  const temporary = mkdtempSync(path.join(tmpdir(), "helivanta-fixture-contract-"));
  try {
    const bin = path.join(temporary, "bin");
    mkdirSync(bin);
    const ownerModule = path.join(temporary, "owner");
    mkdirSync(ownerModule);
    writeFileSync(path.join(ownerModule, "go.mod"), "module fixture\n");
    const capture = path.join(temporary, "args.json");
    writeFileSync(
      path.join(bin, "go"),
      `#!/usr/bin/env python3\nimport os,sys,json,pathlib\nif 'list' in sys.argv:\n print(os.environ['FIXTURE_MODULE'])\nelse:\n pathlib.Path(os.environ['FIXTURE_CAPTURE']).write_text(json.dumps(sys.argv[1:]))\n`,
      { mode: 0o700 },
    );
    const env = {
      ...process.env,
      PATH: bin + path.delimiter + process.env.PATH,
      HELIVANTA_ENV: "dev",
      HELIVANTA_IDENTITY_FIXTURE: "true",
      ZITADEL_ISSUER_URL: "http://auth.tesserix.localhost:20080",
      HELIVANTA_PASSWORD_RECOVERY_FIXTURE: enabled,
      HELIVANTA_STAFF_MFA_RECOVERY_FIXTURE: mfa,
      FIXTURE_MODULE: ownerModule,
      FIXTURE_CAPTURE: capture,
    };
    execFileSync("bash", ["scripts/ci/identity-owner-fixture.sh", "run"], {
      cwd: root,
      env,
      stdio: "pipe",
    });
    return JSON.parse(readFileSync(capture, "utf8"));
  } finally {
    rmSync(temporary, { recursive: true, force: true });
  }
}

test("ordinary identity owner fixture remains read-only without recovery opt-in", () => {
  const args = invoke("");
  assert.ok(!args.includes("-password-recovery-outbox"));
  assert.ok(!args.includes("-staff-mfa-recovery"));
});
test("explicit recovery fixture confines its mailbox to private native test state", () => {
  const args = invoke("true");
  const index = args.indexOf("-password-recovery-outbox");
  assert.ok(index >= 0, "recovery mode was not passed to the canonical owner fixture");
  assert.equal(args[index + 1], path.join(root, "dev/zitadel/secrets/password-recovery-mailbox"));
});
test("unrecognized recovery fixture values are refused before starting a process", () => {
  assert.throws(() => invoke("false"));
});

test("native MFA recovery requires its own explicit opt-in independently of password mail", () => {
  for (const password of ["", "true"]) {
    const args = invoke(password, "true");
    assert.ok(args.includes("-staff-mfa-recovery"));
    assert.equal(args.includes("-password-recovery-outbox"), password === "true");
  }
  assert.throws(() => invoke("", "false"));
});
