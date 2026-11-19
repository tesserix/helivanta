import assert from "node:assert/strict";
import test from "node:test";
import { execFileSync } from "node:child_process";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { recoveryDiagnostics } from "./recovery-owner-diagnostics.mjs";

test("recovery diagnostics retain bounded stages and status without private fields", () => {
  const privateFields = {
    actor: "private-account",
    reason: "private-reason",
    token: "private-token",
    body: "private-provider-payload",
  };
  const log = [
    { msg: "Staff MFA target proof unavailable", stage: "member_proof", ...privateFields },
    { msg: "Staff MFA recovery preparation unavailable", stage: "target", ...privateFields },
    {
      msg: "request",
      method: "PUT",
      path: "/v1/orgs/:id/staff-mfa-recoveries/:commandId",
      status: 503,
      ...privateFields,
    },
  ]
    .map((record) => JSON.stringify(record))
    .join("\n");
  const result = recoveryDiagnostics(log);
  assert.deepEqual(result, [
    { component: "native_target", stage: "member_proof" },
    { component: "owner_prepare", stage: "target" },
    { component: "owner_http", method: "PUT", status: 503 },
  ]);
  for (const value of Object.values(privateFields))
    assert.equal(JSON.stringify(result).includes(value), false);
});

test("unknown, malformed and private-path records never reach diagnostics", () => {
  const records = [
    null,
    [],
    "private-payload",
    { msg: "Staff MFA target proof unavailable", stage: "private-stage" },
    { msg: "Staff MFA recovery preparation unavailable", stage: "private-stage" },
    { msg: "unknown", stage: "target" },
    {
      msg: "request",
      method: "PUT",
      path: "/v1/orgs/private-org/staff-mfa-recoveries/private-command",
      status: 503,
    },
    ...[99, 600, "503", 503.5].map((status) => ({
      msg: "request",
      method: "PUT",
      path: "/v1/orgs/:id/staff-mfa-recoveries/:commandId",
      status,
    })),
    {
      msg: "request",
      method: "GET",
      path: "/v1/orgs/:id/staff-mfa-recoveries/:commandId",
      status: 503,
    },
  ];
  assert.deepEqual(recoveryDiagnostics(`not json\n${records.map(JSON.stringify).join("\n")}`), []);
});

test("diagnostics bound input bytes, record bytes and output count", () => {
  const valid = JSON.stringify({ msg: "Staff MFA target proof unavailable", stage: "token" });
  assert.deepEqual(recoveryDiagnostics("x".repeat((8 << 20) + 1)), []);
  assert.deepEqual(
    recoveryDiagnostics(
      JSON.stringify({
        msg: "Staff MFA target proof unavailable",
        stage: "token",
        body: "x".repeat(16 << 10),
      }),
    ),
    [],
  );
  assert.equal(recoveryDiagnostics(Array(101).fill(valid).join("\n")).length, 100);
  assert.deepEqual(recoveryDiagnostics(null), []);
});

test("CLI emits only allowlisted diagnostics and fixed unreadable-input output", (t) => {
  const directory = mkdtempSync(join(tmpdir(), "recovery-diagnostics-"));
  t.after(() => rmSync(directory, { recursive: true, force: true }));
  const path = join(directory, "owner.log");
  const script = fileURLToPath(new URL("./recovery-owner-diagnostics.mjs", import.meta.url));
  const run = (input) => execFileSync(process.execPath, [script, input], { encoding: "utf8" });
  writeFileSync(
    path,
    JSON.stringify({
      msg: "Staff MFA target proof unavailable",
      stage: "token",
      secret: "private-value",
    }),
  );
  assert.equal(run(path), '{"component":"native_target","stage":"token"}\n');
  assert.equal(run(join(directory, "private-missing")), "Recovery owner diagnostics unavailable\n");
  assert.equal(run(directory), "Recovery owner diagnostics unavailable\n");
  writeFileSync(path, "private".repeat(1_200_000));
  assert.equal(run(path), "Recovery owner diagnostics unavailable\n");
});

test("recovery admission diagnostics expose only known native proof stages", () => {
  const records = [
    {
      msg: "Staff MFA recovery admission unavailable",
      stage: "session",
      subject: "private-user",
      nativeSessionId: "private-session",
    },
    {
      msg: "Staff MFA enrollment proof unavailable",
      stage: "decode",
      body: "private-provider-body",
    },
    { msg: "Staff MFA session proof unavailable", stage: "read", token: "private-token" },
    { msg: "Staff MFA recovery admission unavailable", stage: "private-stage" },
    { msg: "Staff MFA enrollment proof unavailable", stage: "private-stage" },
    { msg: "Staff MFA session proof unavailable", stage: "private-stage" },
  ];
  assert.deepEqual(recoveryDiagnostics(records.map(JSON.stringify).join("\n")), [
    { component: "owner_admission", stage: "session" },
    { component: "native_enrollment", stage: "decode" },
    { component: "native_session", stage: "read" },
  ]);
});
