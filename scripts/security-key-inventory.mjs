#!/usr/bin/env node
// #1898 slice 3: count the security keys that must move before the hosted
// continuation (ADR-0069) is deleted. Read-only: it sends only GET and
// _search requests to Zitadel and reads two files the operator exported.
//
// A key is legacy when it is a READY u2f factor whose id is not in
// iam_security_keys, the same rule sign-in applies (loginclient/sufficiency.go).
//
// Export the two files with a role that can read past forced RLS, in a
// read-only transaction:
//
//   export PGOPTIONS='-c default_transaction_read_only=on'
//   psql "$HELIVANTA_DB_ADMIN_URL" -At -c "SELECT DISTINCT subject FROM iam_members ORDER BY 1" > members.txt
//   psql "$HELIVANTA_DB_ADMIN_URL" -At -F, -c "SELECT subject, key_id FROM iam_security_keys ORDER BY 1, 2" > registered.csv
//
// Then, with a Zitadel service user that can read every Helivanta user:
//
//   ZITADEL_ISSUER=https://auth.tesserix.app ZITADEL_PAT=... \
//     node scripts/security-key-inventory.mjs members.txt registered.csv
//
// Exit 0: zero legacy keys. Exit 2: legacy keys found (listed). Exit 1: the
// count could not be completed, which is never reported as zero.
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

export function parseMembers(text) {
  return [
    ...new Set(
      text
        .split("\n")
        .map((line) => line.trim())
        .filter(Boolean),
    ),
  ];
}

export function parseRegistered(text) {
  const keys = new Set();
  for (const line of text.split("\n")) {
    if (!line.trim()) continue;
    const [subject, keyID, ...rest] = line.trim().split(",");
    if (!subject || !keyID || rest.length)
      throw new Error(`registered.csv: bad line ${JSON.stringify(line)}`);
    keys.add(`${subject},${keyID}`);
  }
  return keys;
}

// get(path) and search(path, orgID) return parsed JSON, or null for a 404.
// Any other failure throws, so an unreadable user stops the count.
export async function inventory({ members, registered, get, search }) {
  const report = {
    members: members.length,
    missing: 0,
    withKeys: 0,
    registeredKeys: 0,
    legacy: [],
  };
  for (const subject of members) {
    const user = await get(`/v2/users/${encodeURIComponent(subject)}`);
    if (user === null) {
      // A deleted identity holds no key.
      report.missing++;
      continue;
    }
    const org = user?.user?.details?.resourceOwner;
    if (user?.user?.userId !== subject || !org)
      throw new Error(`user ${subject}: response has no id or organisation`);
    const factors = await search(
      `/management/v1/users/${encodeURIComponent(subject)}/auth_factors/_search`,
      org,
    );
    if (factors === null) throw new Error(`user ${subject}: auth factors not found`);
    const keys = (factors.result ?? []).filter(
      (f) => f.state === "AUTH_FACTOR_STATE_READY" && f.u2f?.id,
    );
    if (keys.length) report.withKeys++;
    for (const key of keys) {
      if (registered.has(`${subject},${key.u2f.id}`)) report.registeredKeys++;
      else report.legacy.push({ subject, org, keyID: key.u2f.id });
    }
  }
  return report;
}

function zitadel(issuer, pat) {
  const call = async (method, path, orgID) => {
    const res = await fetch(`${issuer}${path}`, {
      method,
      headers: {
        Authorization: `Bearer ${pat}`,
        "Content-Type": "application/json",
        ...(orgID ? { "x-zitadel-orgid": orgID } : {}),
      },
      ...(method === "POST" ? { body: "{}" } : {}),
      redirect: "error",
    });
    if (res.status === 404) return null;
    if (!res.ok) throw new Error(`${method} ${path}: HTTP ${res.status}`);
    return res.json();
  };
  return { get: (path) => call("GET", path), search: (path, orgID) => call("POST", path, orgID) };
}

async function main([membersFile, registeredFile]) {
  const { ZITADEL_ISSUER: issuer, ZITADEL_PAT: pat } = process.env;
  if (!membersFile || !registeredFile)
    throw new Error("usage: security-key-inventory.mjs members.txt registered.csv");
  if (!issuer?.startsWith("https://") || !pat)
    throw new Error("ZITADEL_ISSUER (https) and ZITADEL_PAT are required");
  const members = parseMembers(readFileSync(membersFile, "utf8"));
  if (!members.length) throw new Error(`${membersFile} lists no members; refusing to report zero`);
  const report = await inventory({
    members,
    registered: parseRegistered(readFileSync(registeredFile, "utf8")),
    ...zitadel(issuer.replace(/\/$/, ""), pat),
  });
  console.log(
    `members ${report.members}, not in Zitadel ${report.missing}, with keys ${report.withKeys}, ` +
      `registered keys ${report.registeredKeys}, legacy keys ${report.legacy.length}`,
  );
  for (const key of report.legacy)
    console.log(`legacy\tsubject=${key.subject}\torg=${key.org}\tkey=${key.keyID}`);
  return report.legacy.length ? 2 : 0;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).then(
    (code) => process.exit(code),
    (err) => {
      console.error(`security-key-inventory: ${err.message}`);
      process.exit(1);
    },
  );
}
