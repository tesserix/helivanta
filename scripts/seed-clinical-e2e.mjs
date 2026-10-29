// Synthetic development fixture for #1389. All catalogue writes go through
// drugmaster's owner CLI and its distinct stager/approver database identities.
import { execFileSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

if (process.env.HELIVANTA_ENV !== "dev")
  throw new Error("Clinical e2e fixtures require HELIVANTA_ENV=dev; never run against production.");
const database = new URL(
  process.env.ADMIN_DATABASE_URL ??
    "postgres://helivanta:helivanta@localhost:5432/helivanta?sslmode=disable",
);
if (
  !["postgres:", "postgresql:"].includes(database.protocol) ||
  !["localhost", "127.0.0.1", "[::1]"].includes(database.hostname) ||
  database.pathname !== "/helivanta"
)
  throw new Error("Clinical e2e fixtures require the local helivanta development database.");
// pgx accepts routing/identity overrides in URL query parameters. Reject them
// rather than allowing a localhost-looking URL to change host, database or role.
if ([...database.searchParams.keys()].some((key) => !["sslmode", "connect_timeout"].includes(key)))
  throw new Error("Clinical e2e fixtures refuse database routing or identity overrides.");
const root = fileURLToPath(new URL("../", import.meta.url));
const fixture = JSON.parse(
  readFileSync(new URL("../e2e/fixtures/opd-drugs.json", import.meta.url), "utf8"),
);
fixture.source.version = `e2e-${randomUUID()}`;
const directory = mkdtempSync(path.join(tmpdir(), "helivanta-clinical-e2e-"));
try {
  const file = path.join(directory, "release.json");
  writeFileSync(file, JSON.stringify(fixture));
  const run = (role, args) => {
    const dsn = new URL(database);
    dsn.username = role;
    dsn.password = role; // Existing dev/init-db.sql accounts; no production credentials.
    return execFileSync("go", ["run", "./cmd/drugmaster-release", ...args], {
      cwd: path.join(root, "backend"),
      env: { ...process.env, DRUGMASTER_DATABASE_URL: dsn.href },
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
    }).trim();
  };
  const release = run("helivanta_drug_stager", ["stage", "-file", file, "-by", "TEST e2e stager"]);
  if (!/^[0-9a-f-]{36}$/.test(release))
    throw new Error("Drugmaster did not return a release identifier.");
  run("helivanta_drug_approver", [
    "decide",
    "-release",
    release,
    "-file",
    file,
    "-by",
    "TEST e2e approver",
    "-publish",
    "-reason",
    "Synthetic #1389 browser fixture; not clinical policy",
  ]);
  // An unchanged molecule needs re-announcement when a consumer is new or was
  // previously offline. Replay remains with the owner, never a projection write.
  run("helivanta_drug_approver", ["replay-molecules"]);
  console.log("Published synthetic OPD e2e drug fixture through the owning release commands.");
} finally {
  rmSync(directory, { recursive: true, force: true });
}
