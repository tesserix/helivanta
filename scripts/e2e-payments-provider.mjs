// Synthetic external-provider boundary for #1478. All Helivanta owners and
// OpenBao remain real; this process never contacts Razorpay or a bank.
import { execFileSync } from "node:child_process";
import { generateKeyPairSync, randomBytes, sign } from "node:crypto";
import { readFileSync, writeFileSync, mkdtempSync, mkdirSync, rmSync } from "node:fs";
import http from "node:http";
import https from "node:https";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

if (process.env.HELIVANTA_ENV !== "dev")
  throw new Error("The synthetic payment provider requires HELIVANTA_ENV=dev.");
const root = fileURLToPath(new URL("../", import.meta.url));
const account = JSON.parse(
  readFileSync(new URL("../e2e/fixtures/payments-account.json", import.meta.url), "utf8"),
);
const port = Number(process.env.HELIVANTA_E2E_PAYMENT_PORT ?? 18892);
if (!Number.isInteger(port) || port < 1024 || port > 65530)
  throw new Error("Invalid local fixture port");
const origin = `http://127.0.0.1:${port}`;
const images = `https://127.0.0.1:${port + 1}`;
const bao = `http://127.0.0.1:${port + 2}`;
const directory = mkdtempSync(path.join(tmpdir(), "helivanta-e2e-payments-"));
const container = `helivanta-e2e-payments-${process.pid}`;
const rootToken = randomBytes(24).toString("hex");
const driverToken = randomBytes(24).toString("hex");
const tokenFile = path.join(directory, "api.jwt");
const environment = path.join(root, ".e2e-web/payments.env");
const qrs = new Map();
const byIntent = new Map();
let sequence = 0;
let started = false;

function cleanup() {
  if (started) {
    try {
      execFileSync("docker", ["stop", "--time", "3", container], { stdio: "ignore" });
    } catch {
      /* already stopped */
    }
  }
  rmSync(directory, { recursive: true, force: true });
}
process.once("exit", cleanup);
for (const signal of ["SIGTERM", "SIGINT"]) process.once(signal, () => process.exit(0));

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function vault(method, route, value) {
  const response = await fetch(bao + route, {
    method,
    headers: { "X-Vault-Token": rootToken, "Content-Type": "application/json" },
    body: value === undefined ? undefined : JSON.stringify(value),
    signal: AbortSignal.timeout(5000),
  });
  if (!response.ok)
    throw new Error(`Synthetic OpenBao setup failed: ${route} (${response.status})`);
}

execFileSync(
  "docker",
  [
    "run",
    "--detach",
    "--rm",
    "--name",
    container,
    "--publish",
    `127.0.0.1:${port + 2}:8200`,
    "--env",
    "BAO_DEV_ROOT_TOKEN_ID",
    "openbao/openbao:2.6.2",
    "server",
    "-dev",
    "-dev-listen-address=0.0.0.0:8200",
  ],
  { stdio: "ignore", env: { ...process.env, BAO_DEV_ROOT_TOKEN_ID: rootToken } },
);
started = true;
let ready = false;
for (let n = 0; n < 60; n++) {
  try {
    const response = await fetch(bao + "/v1/sys/health", { signal: AbortSignal.timeout(1000) });
    ready = response.ok;
  } catch {
    /* starting */
  }
  if (ready) break;
  await sleep(500);
}
if (!ready) throw new Error("Synthetic OpenBao did not become ready");
await vault("POST", "/v1/sys/mounts/transit", { type: "transit" });
await vault("POST", "/v1/sys/auth/kubernetes", { type: "jwt" });
const keys = generateKeyPairSync("rsa", { modulusLength: 2048 });
const publicKey = keys.publicKey.export({ type: "spki", format: "pem" });
await vault("POST", "/v1/auth/kubernetes/config", {
  jwt_validation_pubkeys: [publicKey],
  bound_issuer: "helivanta-e2e",
  jwt_supported_algs: ["RS256"],
});
// Mirror the application's bounded transit key families, without key admin/list.
const families = ["helivanta-payments-credentials", "helivanta-staff-*", "helivanta-emergency-*"];
const policy = families
  .flatMap((key) => [
    `path "transit/encrypt/${key}" { capabilities = ["create", "update"] }`,
    `path "transit/decrypt/${key}" { capabilities = ["update"] }`,
    `path "transit/hmac/${key}" { capabilities = ["update"] }`,
  ])
  .join("\n");
await vault("PUT", "/v1/sys/policies/acl/helivanta-e2e-api", { policy });
await vault("POST", "/v1/auth/kubernetes/role/helivanta-e2e-api", {
  role_type: "jwt",
  bound_audiences: ["helivanta-e2e"],
  bound_subject: "helivanta-e2e-api",
  user_claim: "sub",
  token_policies: ["helivanta-e2e-api"],
  token_ttl: 300,
  token_max_ttl: 300,
});
const encode = (value) => Buffer.from(JSON.stringify(value)).toString("base64url");
function writeServiceToken() {
  const now = Math.floor(Date.now() / 1000);
  const unsigned = `${encode({ alg: "RS256", typ: "JWT" })}.${encode({ iss: "helivanta-e2e", sub: "helivanta-e2e-api", aud: "helivanta-e2e", iat: now - 5, exp: now + 7200 })}`;
  writeFileSync(
    tokenFile,
    `${unsigned}.${sign("RSA-SHA256", Buffer.from(unsigned), keys.privateKey).toString("base64url")}`,
    { mode: 0o600 },
  );
}
writeServiceToken();
// Exercise the client's normal projected-token reread on renewal, including
// long local debugging runs. Stopping this fixture removes the token and vault.
setInterval(writeServiceToken, 300_000).unref();

execFileSync(
  "openssl",
  [
    "req",
    "-x509",
    "-newkey",
    "rsa:2048",
    "-nodes",
    "-days",
    "1",
    "-subj",
    "/CN=localhost",
    "-addext",
    "subjectAltName=DNS:localhost,IP:127.0.0.1",
    "-keyout",
    path.join(directory, "key.pem"),
    "-out",
    path.join(directory, "cert.pem"),
  ],
  { stdio: "ignore" },
);

function send(response, status, value) {
  response.writeHead(status, { "Content-Type": "application/json", "Cache-Control": "no-store" });
  response.end(JSON.stringify(value));
}
async function body(request) {
  let text = "";
  for await (const chunk of request) {
    text += chunk;
    if (text.length > 8192) throw new Error("body too large");
  }
  return text ? JSON.parse(text) : {};
}
const server = http.createServer(async (request, response) => {
  try {
    const url = new URL(request.url, origin);
    if (request.method === "GET" && url.pathname === "/healthz")
      return send(response, 200, { fixture: "TEST ONLY", ready: true });
    if (url.pathname.startsWith("/_test/")) {
      if (request.headers["x-helivanta-e2e-driver"] !== driverToken || request.method !== "POST")
        return send(response, 403, {});
      const input = await body(request);
      const qr = qrs.get(input.qr_id);
      if (!qr) return send(response, 404, {});
      if (url.pathname === "/_test/pay" && !qr.closed && !qr.payment) {
        qr.closed = true;
        qr.payment = {
          id: `pay_E2E${++sequence}`,
          status: "captured",
          amount: qr.amount,
          currency: "INR",
          created_at: Math.floor(Date.now() / 1000),
          acquirer_data: { rrn: `TEST-RRN-${sequence}` },
        };
        return send(response, 200, qr.payment);
      }
      if (
        url.pathname === "/_test/refund" &&
        qr.payment &&
        Number.isSafeInteger(input.amount_minor) &&
        input.amount_minor > 0 &&
        input.amount_minor <= qr.amount - (qr.refunded ?? 0)
      ) {
        qr.refunded = (qr.refunded ?? 0) + input.amount_minor;
        return send(response, 200, {
          id: `rfnd_E2E${++sequence}`,
          amount: input.amount_minor,
          payment_id: qr.payment.id,
        });
      }
      return send(response, 409, {});
    }
    const auth = Buffer.from(
      (request.headers.authorization ?? "").replace(/^Basic /, ""),
      "base64",
    ).toString();
    if (auth !== `${account.key_id}:${account.key_secret}`) return send(response, 401, {});
    if (url.pathname === "/v1/payments/qr_codes") {
      if (request.method === "GET") return send(response, 200, { items: [] });
      const input = await body(request);
      if (
        request.method !== "POST" ||
        input.type !== "upi_qr" ||
        input.usage !== "single_use" ||
        input.fixed_amount !== true ||
        !Number.isSafeInteger(input.payment_amount) ||
        input.payment_amount < 1 ||
        input.payment_amount > 1e12 ||
        typeof input.notes?.intent_id !== "string"
      )
        return send(response, 400, {});
      let id = byIntent.get(input.notes.intent_id);
      if (!id) {
        id = `qr_E2E${++sequence}`;
        byIntent.set(input.notes.intent_id, id);
        qrs.set(id, { amount: input.payment_amount, closed: false });
      }
      return send(response, 200, { id, image_url: `${images}/qr/${id}.svg`, status: "active" });
    }
    const match = /^\/v1\/payments\/qr_codes\/(qr_E2E\d+)\/(payments|close)$/.exec(url.pathname);
    const qr = match && qrs.get(match[1]);
    if (!qr) return send(response, 404, {});
    if (match[2] === "payments" && request.method === "GET")
      return send(response, 200, { items: qr.payment ? [qr.payment] : [] });
    if (match[2] === "close" && request.method === "POST") {
      qr.closed = true;
      return send(response, 200, {});
    }
    return send(response, 404, {});
  } catch {
    return send(response, 400, {});
  }
});
const imageServer = https.createServer(
  {
    key: readFileSync(path.join(directory, "key.pem")),
    cert: readFileSync(path.join(directory, "cert.pem")),
  },
  (request, response) => {
    if (!/^\/qr\/qr_E2E\d+\.svg$/.test(request.url)) {
      response.writeHead(404);
      response.end();
      return;
    }
    response.writeHead(200, { "Content-Type": "image/svg+xml", "Cache-Control": "no-store" });
    response.end(
      '<svg xmlns="http://www.w3.org/2000/svg" width="224" height="224"><rect width="224" height="224" fill="white"/><text x="12" y="100">TEST ONLY</text><text x="12" y="126">Synthetic UPI provider</text></svg>',
    );
  },
);
await Promise.all([
  new Promise((resolve, reject) => server.once("error", reject).listen(port, "127.0.0.1", resolve)),
  new Promise((resolve, reject) =>
    imageServer.once("error", reject).listen(port + 1, "127.0.0.1", resolve),
  ),
]);
mkdirSync(path.dirname(environment), { recursive: true });
const values = {
  HELIVANTA_E2E_PAYMENT_PID: String(process.pid),
  RAZORPAY_API_URL: origin,
  OPENBAO_ADDR: bao,
  OPENBAO_TRANSIT_MOUNT: "transit",
  OPENBAO_AUTH_ROLE: "helivanta-e2e-api",
  OPENBAO_TOKEN_PATH: tokenFile,
  HELIVANTA_E2E_PAYMENT_PROVIDER_URL: origin,
  HELIVANTA_E2E_PAYMENT_DRIVER_TOKEN: driverToken,
};
writeFileSync(
  environment,
  Object.entries(values)
    .map(([key, value]) => `${key}='${value.replaceAll("'", "'\\''")}'`)
    .join("\n") + "\n",
  { mode: 0o600 },
);
console.log(
  "Synthetic payment provider and real scoped OpenBao ready on loopback; configuration written to .e2e-web/payments.env",
);
