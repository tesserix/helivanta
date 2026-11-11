const localIssuer = "http://auth.tesserix.localhost:20080";
const tenants = ["11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"];

export async function ensureTenantIdentityFixtures({ issuer, pat, fetcher = fetch }) {
  if (issuer !== localIssuer) throw new Error("identity fixtures require the local native issuer");
  async function request(method, path, body, scope, allowMissing = false) {
    const response = await fetcher(`${issuer}${path}`, {
      method,
      redirect: "error",
      signal: AbortSignal.timeout(10000),
      headers: {
        Authorization: `Bearer ${pat}`,
        "Content-Type": "application/json",
        ...(scope ? { "x-zitadel-orgid": scope } : {}),
      },
      ...(body ? { body: JSON.stringify(body) } : {}),
    });
    if (allowMissing && response.status === 404) return null;
    if (!response.ok)
      throw new Error(
        `local native fixture request refused: ${method} ${path} (${response.status})`,
      );
    const text = await response.text();
    if (text.length > 65536) throw new Error("oversized local native fixture response");
    return JSON.parse(text);
  }
  const platform = await request("GET", "/management/v1/orgs/me");
  const projects = await request("POST", "/management/v1/projects/_search", {
    query: { limit: "100" },
    queries: [{ nameQuery: { name: "Helivanta", method: "TEXT_QUERY_METHOD_EQUALS" } }],
  });
  const matching = projects.result?.filter((p) => p.name === "Helivanta");
  if (!platform.org?.id || matching?.length !== 1 || !matching[0].id)
    throw new Error("local native project not bootstrapped");
  const projectId = matching[0].id;
  const adminOrgId = platform.org.id;
  for (const tenant of tenants) {
    const org = `helivanta-${tenant}`;
    const existing = await request("GET", `/admin/v1/orgs/${org}`, null, null, true);
    if (!existing) {
      const created = await request("POST", "/v2/organizations", {
        organizationId: org,
        name: `[${tenant}] Helivanta local fixture`,
      });
      if (created.organizationId !== org) throw new Error("local native organization mismatch");
    } else if (existing.org?.id !== org || existing.org?.state !== "ORG_STATE_ACTIVE") {
      throw new Error("local native organization unavailable");
    }
    await request(
      "POST",
      "/management/v1/metadata/tenant_id",
      { value: Buffer.from(tenant).toString("base64") },
      org,
    );
    const grants = await request(
      "POST",
      "/management/v1/projectgrants/_search",
      {
        query: { limit: "2" },
        queries: [{ projectIdQuery: { projectId } }, { grantedOrgIdQuery: { grantedOrgId: org } }],
      },
      adminOrgId,
    );
    if (grants.details?.totalResult === "0" && (grants.result ?? []).length === 0) {
      await request(
        "POST",
        `/management/v1/projects/${projectId}/grants`,
        { grantedOrgId: org, roleKeys: [] },
        adminOrgId,
      );
    } else if (
      grants.details?.totalResult !== "1" ||
      grants.result?.length !== 1 ||
      grants.result[0].projectId !== projectId ||
      grants.result[0].grantedOrgId !== org ||
      grants.result[0].state !== "PROJECT_GRANT_STATE_ACTIVE"
    ) {
      throw new Error("local native project grant mismatch");
    }
  }
  return { projectId, adminOrgId };
}
