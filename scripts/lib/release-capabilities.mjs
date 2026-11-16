export function validateReleaseManifest(value, declaredRoutes) {
  const fail = (message) => {
    throw new Error(message);
  };
  if (value?.version !== 1 || !/^[a-zA-Z0-9_.-]{1,128}$/.test(value.policy_version)) {
    fail("invalid release manifest version");
  }
  const capabilities = new Map();
  for (const capability of value.capabilities ?? []) {
    if (capability.key === "baseline") fail("reserved capability baseline");
    if (
      !/^[a-z][a-z0-9_]{0,63}$/.test(capability.key) ||
      typeof capability.eligible !== "boolean" ||
      !capability.owner ||
      !Array.isArray(capability.dependencies) ||
      capabilities.has(capability.key)
    ) {
      fail("invalid or duplicate capability");
    }
    capabilities.set(capability.key, capability);
  }
  const visited = new Set();
  const visit = (key, pending = new Set()) => {
    if (pending.has(key)) fail(`dependency cycle at ${key}`);
    if (visited.has(key)) return;
    const capability = capabilities.get(key);
    const next = new Set([...pending, key]);
    for (const dependency of capability.dependencies) {
      if (!capabilities.has(dependency)) fail(`unknown dependency ${dependency}`);
      visit(dependency, next);
      if (capability.eligible && !capabilities.get(dependency).eligible) {
        fail(`ineligible dependency ${dependency}`);
      }
    }
    visited.add(key);
  };
  for (const key of capabilities.keys()) visit(key);
  const known = (key) => {
    if (key !== "baseline" && !capabilities.has(key)) fail(`unknown capability ${key}`);
  };
  const routeKeys = new Set();
  for (const route of value.routes ?? []) {
    for (const field of Object.keys(route)) {
      if (!["method", "path", "capability"].includes(field)) fail(`unknown route field ${field}`);
    }
    if (
      !/^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)$/.test(route.method) ||
      !/^\/[a-zA-Z0-9_:/.*-]+$/.test(route.path) ||
      route.path.includes("//")
    ) {
      fail("invalid route");
    }
    const key = `${route.method} ${route.path}`;
    if (routeKeys.has(key)) fail(`duplicate route ${key}`);
    routeKeys.add(key);
    known(route.capability);
  }
  for (const route of declaredRoutes) {
    const key = `${route.method} ${route.path}`;
    if (!routeKeys.has(key)) fail(`unclassified API route ${key}`);
  }
  for (const [kind, entries] of [
    ["page", value.pages],
    ["panel", value.panels],
  ]) {
    const seen = new Set();
    for (const entry of entries ?? []) {
      const key = kind === "page" ? entry.path : entry.key;
      if (typeof key !== "string" || !key || seen.has(key)) fail(`invalid or duplicate ${kind}`);
      seen.add(key);
      known(entry.capability);
    }
  }
  return value;
}
