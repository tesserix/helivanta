// TEST ONLY: real Temporal + GCS emulator + bounded synthetic scanner protocol.
// The real Pharmacy API, bus, restricted worker, byte checks and release guards
// execute unchanged. This does not verify ClamAV's production signature coverage.
import { spawn, execFileSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import http from "node:http";
import net from "node:net";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import pg from "pg";

if (process.env.HELIVANTA_ENV !== "dev" || process.env.HELIVANTA_E2E !== "1")
  throw new Error("Evidence fixture requires explicit dev E2E mode");
const port = Number(process.env.HELIVANTA_E2E_EVIDENCE_PORT ?? 18910);
if (!Number.isInteger(port) || port < 1024 || port > 65530) throw new Error("Invalid fixture port");
let database;
try {
  database = new URL(process.env.ADMIN_DATABASE_URL ?? "invalid:");
} catch {
  throw new Error("Evidence fixture requires a valid loopback dev database URL");
}
if (
  !["postgres:", "postgresql:"].includes(database.protocol) ||
  !["127.0.0.1", "localhost"].includes(database.hostname) ||
  database.pathname !== "/helivanta"
)
  throw new Error("Evidence fixture requires the explicitly selected loopback dev database");
database.hostname = "127.0.0.1";
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const temporary = mkdtempSync(path.join(os.tmpdir(), "helivanta-e2e-evidence-"));
const containers = [];
const client = new pg.Client({
  connectionString: database.toString(),
  connectionTimeoutMillis: 5000,
  query_timeout: 5000,
});
const password = randomBytes(36).toString("base64url");
const quarantine = "helivanta-e2e-quarantine";
const released = "helivanta-e2e-released";
const storage = `http://127.0.0.1:${port + 1}`;
let worker;
let login = false;
let connected = false;
let shuttingDown = false;
let ready = false;
let wroteEnvironment = false;
const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

async function cleanup() {
  if (shuttingDown) return;
  shuttingDown = true;
  ready = false;
  worker?.kill("SIGTERM");
  if (worker && worker.exitCode === null) {
    await Promise.race([new Promise((resolve) => worker.once("exit", resolve)), pause(10_000)]);
    if (worker.exitCode === null && worker.signalCode === null) worker.kill("SIGKILL");
  }
  if (login) {
    try {
      await client.query("ALTER ROLE helivanta_pharmacy_evidence_worker NOLOGIN PASSWORD NULL");
    } catch {
      process.stderr.write(
        "Test worker login cleanup unavailable; restore its dormant role before rerunning\n",
      );
    }
  }
  if (connected) await client.end().catch(() => {});
  for (const name of containers.reverse()) {
    try {
      execFileSync("docker", ["stop", "--time", "3", name], { stdio: "ignore" });
    } catch {
      /* already stopped */
    }
  }
  if (scanner.listening) scanner.close();
  if (health.listening) health.close();
  if (wroteEnvironment) {
    const file = path.join(root, ".e2e-web/evidence.env");
    try {
      if (readFileSync(file, "utf8").includes(`HELIVANTA_E2E_EVIDENCE_PID='${process.pid}'`))
        rmSync(file);
    } catch {
      /* already removed */
    }
  }
  rmSync(temporary, { recursive: true, force: true });
}
for (const signal of ["SIGINT", "SIGTERM"])
  process.once(signal, () => {
    void cleanup().finally(() => process.exit(0));
  });

function scannerVersion() {
  const now = new Date();
  const days = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
  const months = [
    "Jan",
    "Feb",
    "Mar",
    "Apr",
    "May",
    "Jun",
    "Jul",
    "Aug",
    "Sep",
    "Oct",
    "Nov",
    "Dec",
  ];
  return `ClamAV 1.5.4/1/${days[now.getUTCDay()]} ${months[now.getUTCMonth()]} ${String(now.getUTCDate()).padStart(2, " ")} ${now.toISOString().slice(11, 19)} ${now.getUTCFullYear()}\0`;
}
const scanner = net.createServer((socket) => {
  socket.setTimeout(15_000, () => socket.destroy());
  socket.on("error", () => {});
  let buffer = Buffer.alloc(0),
    command = false,
    size = 0,
    frames = 0;
  const content = [];
  socket.on("data", (part) => {
    buffer = Buffer.concat([buffer, part]);
    if (!command) {
      const end = buffer.indexOf(0);
      if (end < 0) {
        if (buffer.length > 32) socket.destroy();
        return;
      }
      const name = buffer.subarray(0, end).toString();
      buffer = buffer.subarray(end + 1);
      if (name === "zVERSION") {
        socket.end(scannerVersion());
        return;
      }
      if (name !== "zINSTREAM") {
        socket.destroy();
        return;
      }
      command = true;
    }
    while (buffer.length >= 4) {
      const length = buffer.readUInt32BE();
      if (length > 10 * 1024 * 1024 || size + length > 10 * 1024 * 1024 || frames > 1024) {
        socket.destroy();
        return;
      }
      if (buffer.length < length + 4) return;
      buffer = buffer.subarray(4);
      if (!length) {
        socket.end(
          Buffer.concat(content).includes(Buffer.from("EICAR"))
            ? "stream: TEST-signature FOUND\0"
            : "stream: OK\0",
        );
        return;
      }
      content.push(buffer.subarray(0, length));
      size += length;
      frames++;
      buffer = buffer.subarray(length);
    }
  });
});
const health = http.createServer((request, response) => {
  response.writeHead(ready && request.url === "/healthz" ? 200 : 503, {
    "Content-Type": "application/json",
    "Cache-Control": "no-store",
  });
  response.end(JSON.stringify({ fixture: "TEST ONLY", ready }));
});
async function waitFor(probe) {
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    try {
      if (await probe()) return;
    } catch {
      /* bounded startup */
    }
    await pause(300);
  }
  throw new Error("Evidence fixture dependency did not become ready");
}
async function main() {
  await new Promise((resolve, reject) =>
    health.once("error", reject).listen(port, "127.0.0.1", resolve),
  );
  await new Promise((resolve, reject) =>
    scanner.once("error", reject).listen(port + 3, "127.0.0.1", resolve),
  );
  for (const [kind, image, publish, args] of [
    [
      "gcs",
      "fsouza/fake-gcs-server:1.54.0",
      `${port + 1}:4443`,
      ["-scheme", "http", "-port", "4443", "-external-url", storage],
    ],
    [
      "temporal",
      "temporalio/temporal:1.4.1",
      `${port + 2}:7233`,
      ["server", "start-dev", "--ip", "0.0.0.0"],
    ],
  ]) {
    const name = `helivanta-e2e-evidence-${kind}-${process.pid}`;
    execFileSync(
      "docker",
      [
        "run",
        "--detach",
        "--rm",
        "--name",
        name,
        "--publish",
        `127.0.0.1:${publish}`,
        image,
        ...args,
      ],
      { stdio: "ignore" },
    );
    containers.push(name);
  }
  await waitFor(
    async () => (await fetch(storage + "/storage/v1/b", { signal: AbortSignal.timeout(2000) })).ok,
  );
  for (const name of [quarantine, released]) {
    const response = await fetch(storage + "/storage/v1/b?project=helivanta-e2e", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
    if (!response.ok) throw new Error("Synthetic bucket setup unavailable");
  }
  await client.connect();
  connected = true;
  const role = await client.query(
    "SELECT NOT rolcanlogin AND NOT rolinherit AND NOT rolsuper AND NOT rolbypassrls AND NOT rolcreatedb AND NOT rolcreaterole AND NOT EXISTS(SELECT 1 FROM pg_auth_members WHERE member=r.oid) AS dormant FROM pg_roles r WHERE rolname='helivanta_pharmacy_evidence_worker'",
  );
  if (role.rows.length !== 1 || role.rows[0].dormant !== true)
    throw new Error("The test worker role must be dormant before this fixture owns it");
  await client.query("SELECT set_config('app.e2e_evidence_password',$1,false)", [password]);
  await client.query(
    "DO $$BEGIN EXECUTE format('ALTER ROLE helivanta_pharmacy_evidence_worker LOGIN NOINHERIT NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE PASSWORD %L',current_setting('app.e2e_evidence_password'));END$$",
  );
  login = true;
  await client.query("RESET app.e2e_evidence_password");
  const workerDatabase = new URL(database);
  workerDatabase.username = "helivanta_pharmacy_evidence_worker";
  workerDatabase.password = password;
  const environment = {
    DOCUMENT_QUARANTINE_BUCKET: quarantine,
    DOCUMENT_RELEASED_BUCKET: released,
    STORAGE_EMULATOR_HOST: storage,
    TEMPORAL_ADDRESS: `127.0.0.1:${port + 2}`,
    TEMPORAL_NAMESPACE: "default",
    CLAMAV_ADDRESS: `127.0.0.1:${port + 3}`,
    PHARMACY_EVIDENCE_ENABLED: "true",
    HELIVANTA_E2E_EVIDENCE_PID: String(process.pid),
  };
  const binary = path.join(temporary, "pharmacy-evidence-worker");
  execFileSync("go", ["build", "-o", binary, "./cmd/pharmacy-evidence-worker"], {
    cwd: path.join(root, "backend"),
    stdio: "ignore",
  });
  worker = spawn(binary, [], {
    cwd: root,
    env: {
      PATH: process.env.PATH,
      HELIVANTA_ENV: "dev",
      ...environment,
      PHARMACY_EVIDENCE_WORKER_DATABASE_URL: workerDatabase.toString(),
      PORT: String(port + 4),
    },
    stdio: "ignore",
  });
  worker.once("exit", () => {
    ready = false;
  });
  await waitFor(
    async () =>
      (await fetch(`http://127.0.0.1:${port + 4}/readyz`, { signal: AbortSignal.timeout(2000) }))
        .ok,
  );
  mkdirSync(path.join(root, ".e2e-web"), { recursive: true });
  writeFileSync(
    path.join(root, ".e2e-web/evidence.env"),
    Object.entries(environment)
      .map(([key, value]) => `${key}='${value.replaceAll("'", "'\\''")}'`)
      .join("\n") + "\n",
    { mode: 0o600, flag: "wx" },
  );
  wroteEnvironment = true;
  ready = true;
  process.stdout.write("TEST ONLY evidence dependencies and restricted owner worker ready\n");
}
try {
  await main();
} catch {
  process.stderr.write("Evidence fixture bootstrap failed; dependency payloads withheld\n");
  await cleanup();
  process.exitCode = 1;
}
