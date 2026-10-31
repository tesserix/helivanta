import { readFile, writeFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const registryFile = "docs/api/reference/inventory.json";
const reviewFile = "docs/qa/staff-operational-access-reviews.json";
const outputFile = "docs/qa/staff-operational-access-inventory.json";
const sha256 = (bytes) => createHash("sha256").update(bytes).digest("hex");
const read = (file) => readFile(path.join(root, file), "utf8");

function fields(value, allowed, label) {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error(`Invalid ${label}`);
  for (const field of Object.keys(value))
    if (!allowed.includes(field)) throw new Error(`Unexpected ${label} field: ${field}`);
}

async function main() {
  const args = new Set(process.argv.slice(2));
  if ([...args].some((arg) => !["--check", "--require-classified"].includes(arg)))
    throw new Error(
      "Usage: node scripts/staff-operational-access-inventory.mjs [--check] [--require-classified]",
    );
  const registryBytes = await read(registryFile);
  const registry = JSON.parse(registryBytes);
  const reviews = JSON.parse(await read(reviewFile));
  fields(
    registry,
    ["format_version", "modules", "unauthenticated_routes", "platform_migrations"],
    "registry",
  );
  fields(reviews, ["format_version", "reviews"], "review document");
  if (
    registry.format_version !== 1 ||
    !Array.isArray(registry.modules) ||
    !registry.unauthenticated_routes
  )
    throw new Error("Unsupported registry format");
  if (reviews.format_version !== 1 || !Array.isArray(reviews.reviews))
    throw new Error("Unsupported review format");
  const entries = [];
  const ids = new Set();
  const httpRoutes = new Set();
  const add = (kind, moduleName, registration, suffix) => {
    const id = `${kind} ${moduleName} ${suffix}`;
    if (ids.has(id)) throw new Error(`Duplicate registry entry: ${id}`);
    ids.add(id);
    if (kind === "http" || kind === "unauthenticated_http") {
      const route =
        kind === "http" ? `${registration.method} ${registration.path}` : registration.route;
      if (httpRoutes.has(route)) throw new Error(`Duplicate HTTP registration: ${route}`);
      httpRoutes.add(route);
    }
    entries.push({ id, kind, module: moduleName, registration, review: null });
  };
  for (const ownerModule of registry.modules) {
    fields(
      ownerModule,
      [
        "name",
        "source",
        "routes",
        "grants",
        "publishes",
        "directed_subjects",
        "directed_write_tables",
        "consumers",
        "broadcasts",
        "migrations",
      ],
      "module",
    );
    if (
      !ownerModule.name ||
      !ownerModule.source ||
      !Array.isArray(ownerModule.routes) ||
      !Array.isArray(ownerModule.consumers) ||
      !Array.isArray(ownerModule.broadcasts)
    )
      throw new Error("Incomplete module registration");
    for (const route of ownerModule.routes) {
      fields(
        route,
        [
          "method",
          "path",
          "permission",
          "authentication_required",
          "membership_required",
          "paginated",
          "counted",
          "scope_kind",
          "scope_parameter",
        ],
        "route",
      );
      if (!route.method || !route.path) throw new Error("Incomplete HTTP registration");
      add(
        "http",
        ownerModule.name,
        { source: ownerModule.source, ...route },
        `${route.method} ${route.path}`,
      );
    }
    for (const consumer of ownerModule.consumers) {
      fields(consumer, ["name", "subject", "after_commit"], "consumer");
      if (!consumer.name || !consumer.subject || typeof consumer.after_commit !== "boolean")
        throw new Error("Incomplete consumer registration");
      add(
        "consumer",
        ownerModule.name,
        { source: ownerModule.source, ...consumer },
        `${consumer.name} ${consumer.subject}`,
      );
    }
    for (const subject of ownerModule.broadcasts) {
      if (typeof subject !== "string" || !subject)
        throw new Error("Incomplete broadcast registration");
      add("broadcast", ownerModule.name, { source: ownerModule.source, subject }, subject);
    }
  }
  for (const [route, rationale] of Object.entries(registry.unauthenticated_routes))
    add("unauthenticated_http", "platform", { route, rationale }, route);
  entries.sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
  const entriesByID = new Map(entries.map((entry) => [entry.id, entry]));
  const reviewed = new Set();
  const authorityKinds = new Set([
    "hospital_operational",
    "governance_recovery",
    "patient_self",
    "system_continuation",
    "public_entry",
    "mixed",
  ]);
  for (const review of reviews.reviews) {
    fields(
      review,
      [
        "id",
        "registration_sha256",
        "authority_kind",
        "resource_scope",
        "rationale",
        "authority_sources",
        "remaining",
      ],
      "review",
    );
    const entry = entriesByID.get(review.id);
    if (!entry) throw new Error(`Review has no registered surface: ${review.id}`);
    if (reviewed.has(review.id)) throw new Error(`Duplicate review: ${review.id}`);
    reviewed.add(review.id);
    if (review.registration_sha256 !== sha256(JSON.stringify(entry.registration)))
      throw new Error(`Registration changed; review again: ${review.id}`);
    if (
      !authorityKinds.has(review.authority_kind) ||
      typeof review.resource_scope !== "string" ||
      !review.resource_scope.trim() ||
      typeof review.rationale !== "string" ||
      !review.rationale.trim()
    )
      throw new Error(`Explicit authority, scope and rationale required: ${review.id}`);
    if (
      !Array.isArray(review.remaining) ||
      !review.remaining.length ||
      review.remaining.some((item) => typeof item !== "string" || !item.trim())
    )
      throw new Error(`Remaining runtime acceptance obligations required: ${review.id}`);
    if (!Array.isArray(review.authority_sources) || !review.authority_sources.length)
      throw new Error(`Pinned authority source required: ${review.id}`);
    for (const source of review.authority_sources) {
      fields(source, ["path", "sha256", "symbol"], "source");
      if (
        typeof source.path !== "string" ||
        !source.path.startsWith("backend/") ||
        path.posix.normalize(source.path) !== source.path ||
        source.path.includes("\\") ||
        typeof source.symbol !== "string" ||
        !source.symbol.trim()
      )
        throw new Error(`Invalid authority source: ${review.id}`);
      const bytes = await read(source.path);
      if (source.sha256 !== sha256(bytes))
        throw new Error(`Authority source changed; review again: ${review.id}: ${source.path}`);
      if (!bytes.includes(source.symbol))
        throw new Error(`Authority source symbol missing: ${review.id}: ${source.symbol}`);
    }
    entry.review = review;
  }
  const ledger = {
    format_version: 1,
    registry_sha256: sha256(registryBytes),
    scope:
      "Registered HTTP routes, unauthenticated allowlist entries, async consumers and broadcasts only. This is a source-review ledger, not proof of runtime cutoff or a complete inventory of timers, external integrations and unregistered background work.",
    summary: {
      total: entries.length,
      unclassified: entries.filter((entry) => entry.review === null).length,
    },
    entries,
  };
  const rendered = JSON.stringify(ledger, null, 2) + "\n";
  if (args.has("--check")) {
    if ((await read(outputFile)) !== rendered)
      throw new Error(
        "Staff operational access inventory is stale; regenerate and review the registry/source changes",
      );
  } else {
    await writeFile(path.join(root, outputFile), rendered);
  }
  if (args.has("--require-classified") && ledger.summary.unclassified)
    throw new Error(
      `${ledger.summary.unclassified} unclassified registered surfaces; employment enforcement activation is not established`,
    );
  process.stdout.write(
    `${entries.length} registered surfaces; ${ledger.summary.unclassified} unclassified. No runtime acceptance is asserted.\n`,
  );
}
main().catch((error) => {
  process.stderr.write(`${error.message}\n`);
  process.exitCode = 1;
});
