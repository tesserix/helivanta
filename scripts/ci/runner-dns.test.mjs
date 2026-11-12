import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync, readFileSync, existsSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

const script = fileURLToPath(new URL("./runner-dns.sh", import.meta.url));
function fixture(t, broken = false) {
  const root = mkdtempSync(path.join(tmpdir(), "helivanta-dns-test-"));
  t.after(() => rmSync(root, { recursive: true }));
  const log = path.join(root, "calls"),
    recovered = path.join(root, "recovered");
  for (const [name, body] of Object.entries({
    timeout: 'shift; exec "$@"',
    getent: broken ? `test -f '${recovered}'` : "exit 0",
    ip: "echo 'default via 192.0.2.1 dev eth0 proto dhcp'",
    sudo: `printf '%s\\n' "$*" >> '${log}'; touch '${recovered}'`,
  }))
    writeFileSync(path.join(root, name), "#!/bin/sh\n" + body + "\n", { mode: 0o700 });
  return { root, log };
}
function run(root, extra = {}) {
  return spawnSync("/bin/bash", [script], {
    env: {
      ...process.env,
      PATH: root + ":" + process.env.PATH,
      GITHUB_ACTIONS: "true",
      RUNNER_OS: "Linux",
      ...extra,
    },
    encoding: "utf8",
  });
}
test("working runner DNS is preserved", (t) => {
  const f = fixture(t);
  const result = run(f.root);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(existsSync(f.log), false);
});
test("failed public resolution gets a bounded CI interface recovery", (t) => {
  const f = fixture(t, true);
  const result = run(f.root);
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(readFileSync(f.log, "utf8").trim().split("\n"), [
    "resolvectl dns eth0 1.1.1.1 8.8.8.8",
    "resolvectl flush-caches",
  ]);
});
test("local machines cannot enter runner DNS recovery", (t) => {
  const f = fixture(t, true);
  const result = run(f.root, { GITHUB_ACTIONS: "false" });
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /GitHub Linux runner/);
  assert.equal(existsSync(f.log), false);
});

test("unresolved dependencies still fail after attempted recovery", (t) => {
  const f = fixture(t, true);
  writeFileSync(path.join(f.root, "getent"), "#!/bin/sh\nexit 1\n", { mode: 0o700 });
  const result = run(f.root);
  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /remains unavailable/);
});
test("an unknown CI interface cannot change resolver configuration", (t) => {
  const f = fixture(t, true);
  writeFileSync(path.join(f.root, "ip"), "#!/bin/sh\nexit 0\n", { mode: 0o700 });
  const result = run(f.root);
  assert.notEqual(result.status, 0);
  assert.equal(existsSync(f.log), false);
});
