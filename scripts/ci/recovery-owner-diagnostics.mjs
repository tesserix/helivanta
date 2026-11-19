import { open } from "node:fs/promises";
import { pathToFileURL } from "node:url";

const maxBytes = 8 << 20;
const maxLineBytes = 16 << 10;
const accountPhases = new Set([
  "scope_organization_record",
  "scope_product_record",
  "scope_enrolments",
  "scope_current_identity",
]);
const stages = new Map([
  [
    "Staff MFA recovery admission unavailable",
    {
      component: "owner_admission",
      allowed: new Set([
        "record",
        "account",
        "removal",
        "enrollment",
        "session",
        "enrollment_recheck",
        "account_recheck",
        "credentials",
        "record_recheck",
        "store",
        "commit_recheck",
      ]),
    },
  ],
  [
    "Staff MFA enrollment proof unavailable",
    {
      component: "native_enrollment",
      allowed: new Set(["configuration", "token", "read", "decode", "proof"]),
    },
  ],
  [
    "Staff MFA session proof unavailable",
    {
      component: "native_session",
      allowed: new Set(["configuration", "token", "read", "decode", "proof"]),
    },
  ],
  [
    "Staff MFA target proof unavailable",
    {
      component: "native_target",
      allowed: new Set([
        "configuration",
        "token",
        "user_read",
        "user_projection",
        "password_evidence",
        "member_read",
        "member_proof",
      ]),
    },
  ],
  [
    "Staff MFA recovery preparation unavailable",
    {
      component: "owner_prepare",
      allowed: new Set(["scope", "target", "scope_recheck", "store", "scope_commit_recheck"]),
    },
  ],
]);

export function recoveryDiagnostics(log) {
  if (typeof log !== "string" || Buffer.byteLength(log) > maxBytes) return [];
  const results = [];
  for (const line of log.split("\n")) {
    if (Buffer.byteLength(line) > maxLineBytes) continue;
    let record;
    try {
      record = JSON.parse(line);
    } catch {
      continue;
    }
    if (!record || typeof record !== "object" || Array.isArray(record)) continue;
    const rule = stages.get(record.msg);
    if (rule?.allowed.has(record.stage)) {
      results.push({ component: rule.component, stage: record.stage });
    } else if (
      record.msg === "account lookup unavailable" &&
      accountPhases.has(record.phase) &&
      Number.isInteger(record.elapsed_ms) &&
      record.elapsed_ms >= 0 &&
      record.elapsed_ms <= 60_000 &&
      typeof record.parent_deadline === "boolean" &&
      typeof record.parent_cancelled === "boolean"
    ) {
      results.push({
        component: "account_lookup",
        phase: record.phase,
        elapsed_ms: record.elapsed_ms,
        parent_deadline: record.parent_deadline,
        parent_cancelled: record.parent_cancelled,
      });
    } else if (
      record.msg === "request" &&
      record.method === "PUT" &&
      record.path === "/v1/orgs/:id/staff-mfa-recoveries/:commandId" &&
      Number.isInteger(record.status) &&
      record.status >= 100 &&
      record.status <= 599
    ) {
      results.push({ component: "owner_http", method: "PUT", status: record.status });
    }
    if (results.length === 100) break;
  }
  return results;
}

async function main(path) {
  let file;
  try {
    if (!path) throw new Error();
    file = await open(path, "r");
    if (!(await file.stat()).isFile()) throw new Error();
    const buffer = Buffer.alloc(maxBytes + 1);
    let bytes = 0;
    while (bytes < buffer.length) {
      const result = await file.read(buffer, bytes, buffer.length - bytes, null);
      if (result.bytesRead === 0) break;
      bytes += result.bytesRead;
    }
    if (bytes > maxBytes) throw new Error();
    for (const record of recoveryDiagnostics(buffer.subarray(0, bytes).toString("utf8"))) {
      process.stdout.write(`${JSON.stringify(record)}\n`);
    }
  } catch {
    process.stdout.write("Recovery owner diagnostics unavailable\n");
  } finally {
    await file?.close();
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main(process.argv[2]);
}
